// Package catalogos guarda o que um banco PUBLICA e que é preciso saber antes
// de lhe montar o pedido (ARQUITETURA.md §4, KAN-36).
//
// ⚠️ **Não é a cache de respostas, e a diferença está na §4.** Uma resposta é o
// preço de alguém e leva as guardas de privacidade do §7.6 — chave por resumo
// criptográfico, pedido em claro fora do disco. Um catálogo é o que o banco
// mostra a quem visite o site: catorze inteiros, no caso da CGD. Por isso a chave
// aqui é **legível**, `(banco_id, nome)`, e olhar para a tabela diz o que lá está.
//
// ⚠️ **O que a obrigou a existir, medido a 2026-08-14:** a CGD publica os
// períodos de taxa fixa dentro do HTML da página inicial, e o pacote dela lia-os
// dentro do `Simular`. Por pedido de cliente com fase fixa: 3 pedidos à CGD e
// 82 333 bytes, dos quais 75 291 (91,4 %) eram a página.
//
// ⚠️ Vive em Postgres e não no processo (§7.5). Com mais de um worker, um
// catálogo em memória seria um por worker — e o que se está a limitar é carga
// contra um terceiro.
package catalogos

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/bd"
)

// ValidadeOmissao é quanto tempo um catálogo serve depois de se ler do banco.
//
// ⚠️ **24 horas, e é um valor de partida e não uma medição** — a mesma
// honestidade com que o §7.6 declara os 5 min da cache de respostas. Ninguém
// mediu de quanto em quanto tempo a CGD mexe nos períodos que pratica, e o que o
// mede é uma vigia: perguntar o mesmo de hora a hora e ver quando muda, sendo a
// PRIMEIRA mudança o que decide e não a média.
//
// Porquê 24 h e não 5 min: um catálogo de produtos não é um preço. O que muda de
// manhã é a Euribor, não a lista de períodos que o banco vende — e é por isso que
// as duas validades não têm de ser a mesma. ⚠️ Mas isto é raciocínio, não
// medição, e fica escrito como tal.
const ValidadeOmissao = 24 * time.Hour

// Postgres guarda catálogos sobre um pool já aberto. Quem abre o pool fecha-o.
type Postgres struct {
	pool     *pgxpool.Pool
	validade time.Duration
	agora    func() time.Time
	diario   *slog.Logger
}

// NovoPostgres constrói a loja.
//
// ⚠️ **O relógio e o diário entram aqui porque a interface não os pode
// transportar.** A `bancos.Catalogos` não devolve erro — um catálogo que não se
// lê é uma ida à fonte, não uma falha do pedido —, e por isso o sítio onde uma
// avaria da base se torna visível é este. Sem diário, uma base em baixo fazia
// todos os pedidos irem à página da CGD e ninguém saberia porquê.
//
// Validade nula ou negativa vale ValidadeOmissao: uma validade a zero é não ter
// catálogo nenhum, e ninguém quer dizer isso por engano.
func NovoPostgres(
	pool *pgxpool.Pool, validade time.Duration, agora func() time.Time, diario *slog.Logger,
) *Postgres {
	if validade <= 0 {
		validade = ValidadeOmissao
	}
	if agora == nil {
		agora = time.Now
	}
	return &Postgres{pool: pool, validade: validade, agora: agora, diario: diario}
}

// Validade é a validade em vigor, para quem a queira dizer no arranque.
func (p *Postgres) Validade() time.Duration { return p.validade }

// Ler devolve o catálogo guardado, se ainda for válido.
//
// ⚠️ **Não devolve erro, e é a decisão inteira.** Uma base que não responde
// significa uma ida à fonte — que é o que acontecia sempre antes desta tabela
// existir. Falhar fechado transformava uma avaria nossa na impossibilidade de
// servir aquele banco, e isso é trocar um custo por uma indisponibilidade.
//
// ⚠️ Uma entrada expirada não volta: a validade filtra-se na consulta, e não
// depende de a limpeza ter corrido.
func (p *Postgres) Ler(ctx context.Context, bancoID, nome string) ([]byte, bool) {
	linha, err := bd.New(p.pool).LerCatalogo(ctx, bd.LerCatalogoParams{
		BancoID: bancoID,
		Nome:    nome,
		Agora:   pgtype.Timestamptz{Time: p.agora(), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false
	}
	if err != nil {
		p.registar(ctx, "não se conseguiu ler o catálogo", bancoID, nome, err)
		return nil, false
	}
	return linha.Valor, true
}

// Guardar grava o catálogo lido da fonte, com a validade em vigor.
//
// ⚠️ Também não devolve erro: não conseguir guardar significa que o próximo
// pedido vai outra vez à fonte — mais caro, e correcto na mesma.
func (p *Postgres) Guardar(ctx context.Context, bancoID, nome string, valor []byte) {
	agora := p.agora()
	err := bd.New(p.pool).GravarCatalogo(ctx, bd.GravarCatalogoParams{
		BancoID:  bancoID,
		Nome:     nome,
		Valor:    valor,
		LidoEm:   pgtype.Timestamptz{Time: agora, Valid: true},
		ExpiraEm: pgtype.Timestamptz{Time: agora.Add(p.validade), Valid: true},
	})
	if err != nil {
		p.registar(ctx, "não se conseguiu guardar o catálogo", bancoID, nome, err)
	}
}

// Limpar apaga os catálogos expirados. Corre por manutenção, e não no caminho de
// um cliente — a leitura já ignora o que expirou.
func (p *Postgres) Limpar(ctx context.Context) (int64, error) {
	return bd.New(p.pool).LimparCatalogosExpirados(ctx,
		pgtype.Timestamptz{Time: p.agora(), Valid: true})
}

// registar é onde uma avaria da base deixa de ser silenciosa.
//
// ⚠️ O que vai na linha é o banco, o catálogo e o erro — nada disto é de ninguém.
// É a diferença para a `respostas_em_cache`, onde a mesma linha não podia levar a
// chave sem levar o pedido atrás.
func (p *Postgres) registar(ctx context.Context, msg, bancoID, nome string, err error) {
	if p.diario == nil {
		return
	}
	p.diario.LogAttrs(ctx, slog.LevelWarn, msg,
		slog.String("banco", bancoID),
		slog.String("catalogo", nome),
		slog.String("erro", err.Error()),
	)
}
