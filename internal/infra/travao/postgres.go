// Package travao implementa em Postgres a regra da §7.2: nunca dois
// varrimentos do mesmo banco ao mesmo tempo.
//
// ⚠️ Vive na base e não no processo, e isso é a decisão inteira. O v1 tinha o
// gate por banco dentro do processo e avisava no arranque que, com mais do que
// um worker, o mesmo banco levava N scrapes em paralelo — precisamente o que o
// gate existia para evitar. Várias PaaS decidem a concorrência sozinhas, e um
// travão que só trava dentro de um processo não trava nada.
//
// Usa-se um advisory lock e não uma linha de tabela: o travão não é dado, não
// sobrevive à morte da sessão, e a §4 tem duas tabelas e não abre uma terceira
// para isto.
package travao

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
)

// classeVarrimento é o primeiro argumento do advisory lock, e serve de espaço
// de nomes: os locks do varrimento não colidem com os de mais ninguém que venha
// a usar o mesmo mecanismo nesta base. O número é a data da decisão da §7.2.
const classeVarrimento int32 = 20260725

// PrazoParaLargar é o tecto de tempo do UNLOCK. É curto de propósito: largar é
// uma ida à base e nada mais, e se ela não responder neste tempo a saída certa
// é matar a sessão — o que também larga o travão.
const PrazoParaLargar = 5 * time.Second

// Postgres é o travão do varrimento.
type Postgres struct {
	pool *pgxpool.Pool
}

// NovoPostgres constrói o travão sobre um pool já aberto. Quem abre o pool
// fecha-o.
func NovoPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

var _ varrimento.Travao = (*Postgres)(nil)

// Tomar tenta tomar o travão do banco, sem esperar por ele.
//
// ⚠️ `try` e não a versão que bloqueia: dois arranques concorrentes não valem
// duas cargas em cima do banco, e o segundo não deve ficar em fila para dar a
// mesma carga daqui a pouco. Devolve ErrBancoTravado, e quem o recebe salta o
// banco nesta corrida.
//
// O lock é de sessão, portanto a ligação fica presa a este travão até se largar
// — é o que garante que é a mesma sessão a tomar e a devolver.
func (p *Postgres) Tomar(ctx context.Context, bancoID string) (varrimento.Largar, error) {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("travão de %q: obter ligação: %w", bancoID, err)
	}

	chave := chave(bancoID)
	var tomou bool
	if err := conn.QueryRow(
		ctx, "SELECT pg_try_advisory_lock($1, $2)", classeVarrimento, chave,
	).Scan(&tomou); err != nil {
		conn.Release()
		return nil, fmt.Errorf("travão de %q: pedir o lock: %w", bancoID, err)
	}
	if !tomou {
		conn.Release()
		return nil, fmt.Errorf("%w: %s", varrimento.ErrBancoTravado, bancoID)
	}

	return func() { largar(ctx, conn, bancoID, chave) }, nil
}

// largar devolve o lock e a ligação ao pool.
func largar(ctx context.Context, conn *pgxpool.Conn, bancoID string, chave int32) {
	// ⚠️ context.WithoutCancel. Medido a 2026-07-25: um ctx derivado de um pai
	// que termina fica `context canceled` no mesmo instante — e largar chama-se
	// num defer, exactamente quando o varrimento acabou ou foi interrompido. Com
	// o ctx do varrimento, o UNLOCK de um varrimento cancelado nunca chegava à
	// base.
	ctx, cancelar := context.WithTimeout(context.WithoutCancel(ctx), PrazoParaLargar)
	defer cancelar()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1, $2)", classeVarrimento, chave); err != nil {
		// ⚠️ Devolver ao pool uma ligação que ainda segura o lock deixava o banco
		// travado para sempre: o lock é da sessão, e a sessão sobrevive no pool.
		// Mata-se a ligação, e o Postgres larga o lock com ela. É a única saída
		// que não depende de a base voltar a responder.
		_ = conn.Hijack().Close(ctx)
		return
	}
	conn.Release()
}

// chave transforma o id do banco no segundo argumento do advisory lock.
//
// A conversão de uint32 para int32 dá a volta e é intencional: o que importa é
// ser determinística e caber no int4 que o Postgres pede.
func chave(bancoID string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(bancoID))
	return int32(h.Sum32())
}
