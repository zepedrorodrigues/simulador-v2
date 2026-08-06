// Package lotacao implementa em Postgres o tecto de concorrência por banco da
// §7.2: N pedidos nossos em voo contra o mesmo banco, e o N+1 desiste.
//
// ⚠️ **Não é o `infra/travao`, e não é a mesma coisa com outro nome.** O travão
// é um fecho — um varrimento de cada vez — e morre com o varrimento (passo 6 da
// Fase 6). Este é um tecto, e vive no caminho do cliente, onde dois clientes a
// perguntar pelo mesmo banco ao mesmo tempo é o normal.
//
// ⚠️ Vive na base e não no processo (§7.7). O v1 tinha-o no processo e avisava
// no arranque que com mais do que um worker o mesmo banco levava N pedidos em
// paralelo — que é precisamente o que o tecto existe para evitar.
//
// ⚠️ **Custa uma ligação ao Postgres por pedido em voo**, porque o lock é de
// sessão e a sessão tem de ser a mesma a tomar e a devolver. Com cinco bancos e
// duas vagas são dez ligações no pior caso, e é por isso que o `web/servir`
// dimensiona o pool a partir daqui em vez de aceitar o omissão do pgxpool (o
// maior de 4 e o número de CPUs): um pool pequeno de mais fazia o tecto esfomear
// as consultas que ele existe para proteger.
package lotacao

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/aovivo"
)

// classeAoVivo é o primeiro argumento do advisory lock, e serve de espaço de
// nomes: as vagas do caminho ao vivo não colidem com os locks do varrimento
// (classe 20260725) nem com os de mais ninguém nesta base. O número é a data da
// decisão que reverteu a §1.
const classeAoVivo int32 = 20260806

// VagasOmissao é quantos pedidos nossos podem estar em voo contra o mesmo banco.
//
// ⚠️ **Dois, e é escolha e não medição de tolerância.** A §7.2 diz que fica
// apertado até estar medido, e o apertado já está medido: 1, 2 e 4 pedidos em
// paralelo contra a CGD (2026-07-26, 8 pontos) deram 1,086 s por ponto, 603 ms e
// 367 ms, **zero falhas nos três**. Escolheu-se 2 e não 4 pelo que o `PLAN.md`
// escreve na Fase 6: comprar 40 % de velocidade ao preço de quadruplicar a carga
// que se põe num sistema alheio não é uma troca que se faça por se poder fazer.
//
// ⚠️ E não se sobe procurando onde o banco parte — isso é um teste de carga
// contra o simulador público de um terceiro, e o número que produz é exactamente
// o que não se quer usar.
const VagasOmissao = 2

// PrazoParaSair é o tecto de tempo do UNLOCK, pela mesma razão do
// `travao.PrazoParaLargar`: devolver a vaga é uma ida à base e nada mais, e se
// ela não responder neste tempo a saída certa é matar a sessão — o que também
// devolve a vaga.
const PrazoParaSair = 5 * time.Second

// Postgres é o tecto por banco.
type Postgres struct {
	pool  *pgxpool.Pool
	vagas int
}

// NovoPostgres constrói o tecto sobre um pool já aberto. Quem abre o pool
// fecha-o. Vagas abaixo de 1 valem VagasOmissao — um tecto a zero seria não
// servir banco nenhum, e ninguém quer dizer isso por engano.
func NovoPostgres(pool *pgxpool.Pool, vagas int) *Postgres {
	if vagas < 1 {
		vagas = VagasOmissao
	}
	return &Postgres{pool: pool, vagas: vagas}
}

var _ aovivo.Lotacao = (*Postgres)(nil)

// Vagas é o tecto em vigor, para quem o queira dizer no arranque.
func (p *Postgres) Vagas() int { return p.vagas }

// Entrar toma uma das N vagas do banco, sem esperar por nenhuma.
//
// ⚠️ **Desiste, não faz fila**, e a §7.2 deixa as duas saídas em aberto. Quem
// fica em fila continua a segurar uma ligação à base e a gastar o prazo do
// cliente para no fim ouvir «o banco não respondeu» — e o cliente esperou por
// nós, não pelo banco. Um 503 imediato com o banco nomeado diz a verdade e deixa
// a app decidir se volta a pedir aquele banco.
//
// As N vagas são N advisory locks distintos sobre o mesmo banco: toma-se a
// primeira que estiver livre. O lock é de sessão, portanto a ligação fica presa
// a esta vaga até se sair — é o que garante que é a mesma sessão a tomar e a
// devolver, e é o que faz a vaga cair sozinha se o processo morrer.
func (p *Postgres) Entrar(ctx context.Context, bancoID string) (aovivo.Sair, error) {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("lotação de %q: obter ligação: %w", bancoID, err)
	}

	for vaga := range p.vagas {
		chave := chave(bancoID, vaga)
		var tomou bool
		if err := conn.QueryRow(
			ctx, "SELECT pg_try_advisory_lock($1, $2)", classeAoVivo, chave,
		).Scan(&tomou); err != nil {
			conn.Release()
			return nil, fmt.Errorf("lotação de %q: pedir a vaga %d: %w", bancoID, vaga, err)
		}
		if tomou {
			return func() { sair(ctx, conn, bancoID, chave) }, nil
		}
	}

	conn.Release()
	return nil, fmt.Errorf("%w: %s (%d vagas)", aovivo.ErrSemVaga, bancoID, p.vagas)
}

// sair devolve a vaga e a ligação ao pool.
func sair(ctx context.Context, conn *pgxpool.Conn, bancoID string, chave int32) {
	// ⚠️ context.WithoutCancel, e aqui é ainda mais necessário do que no travão:
	// sair chama-se num defer, e o caso normal do caminho ao vivo é o ctx do
	// pedido já ter acabado — o cliente desligou-se, ou o prazo do banco expirou.
	// Com o ctx do pedido, o UNLOCK de um pedido cancelado nunca chegava à base e
	// a vaga só voltava com a ligação morta.
	ctx, cancelar := context.WithTimeout(context.WithoutCancel(ctx), PrazoParaSair)
	defer cancelar()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1, $2)", classeAoVivo, chave); err != nil {
		// ⚠️ Devolver ao pool uma ligação que ainda segura a vaga tirava-a de
		// circulação para sempre: o lock é da sessão, e a sessão sobrevive no pool.
		// Mata-se a ligação, e o Postgres larga o lock com ela.
		_ = conn.Hijack().Close(ctx)
		return
	}
	conn.Release()
}

// chave transforma o banco e o número da vaga no segundo argumento do advisory
// lock.
//
// ⚠️ O número da vaga entra na chave, e é o que faz disto um tecto: N chaves
// distintas por banco são N vagas: uma só era o fecho do varrimento outra vez.
//
// A conversão de uint32 para int32 dá a volta e é intencional — o que importa é
// ser determinística e caber no int4 que o Postgres pede.
func chave(bancoID string, vaga int) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(bancoID + "#" + strconv.Itoa(vaga)))
	return int32(h.Sum32())
}
