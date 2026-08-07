// Package limites é o contador do tecto de pedidos por IP (ARQUITETURA.md §4,
// tabela `limites`; §7.5).
//
// ⚠️ **Vive num pacote próprio desde 2026-08-07, e a razão é o que vem a
// seguir.** Estava dentro do `infra/catalogo`, que existe para a série do
// varrimento e **morre com ela** (Fase 6, passo 5). O contador não morre: é
// caminho do cliente, e é ele que separa um serviço de uma ferramenta de carga
// contra cinco bancos. Deixá-lo lá dentro obrigava a escolher entre apagar o
// catálogo e ficar sem tecto.
//
// ⚠️ Vive na BASE e não no processo (§7.7), pela mesma razão que o tecto por
// banco: várias PaaS definem a concorrência sozinhas, e dois processos com
// contadores em memória não se travam um ao outro.
package limites

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/bd"
)

// Postgres é o contador sobre um pool já aberto. Quem abre o pool fecha-o.
type Postgres struct {
	pool *pgxpool.Pool
}

func NovoPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

// ContarPedido regista um pedido de um cliente e devolve quantos vai nesta
// janela.
//
// ⚠️ Numa instrução só. Um SELECT seguido de UPDATE deixa duas ligações a lerem
// a mesma contagem e a escreverem a mesma soma.
//
// ⚠️ **E o custo está medido, não estimado** (2026-08-07,
// `TestSobConcorrenciaNenhumPedidoSePerde`): com a contagem lida e depois
// escrita, **30 pedidos em paralelo deram contagem 2** — perderam-se 28. O tecto
// não «valia o dobro»: valia 15×, e só sob a carga que ele existe para travar.
func (p *Postgres) ContarPedido(
	ctx context.Context, chave string, janela time.Duration, agora time.Time,
) (int, error) {
	linha, err := bd.New(p.pool).ContarPedido(ctx, bd.ContarPedidoParams{
		Chave:  chave,
		Agora:  pgtype.Timestamptz{Time: agora, Valid: true},
		Janela: intervaloDe(janela),
	})
	if err != nil {
		return 0, fmt.Errorf("contar o pedido de %q: %w", chave, err)
	}
	return int(linha.Contagem), nil
}

// ⚠️ O `LimparLimitesAntigos` do `db/queries/limites.sql` **continua sem
// chamador**, e não passa a ter um aqui: isto é uma mudança de sítio, não uma
// funcionalidade nova. Quem lhe pegar dá-lhe um dono e um teste.

// intervaloDe converte a janela para o intervalo do Postgres.
func intervaloDe(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}
