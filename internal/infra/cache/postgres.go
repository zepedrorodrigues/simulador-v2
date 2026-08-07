// Package cache é a cache das respostas do caminho ao vivo (ARQUITETURA.md §4
// e §7.6).
//
// ⚠️ **Não é optimização de latência — é a defesa contra sermos um
// amplificador.** Um pedido de cliente vira ~10 pedidos aos bancos, com origem
// aparente nossa. Esta é a única defesa que corta essa carga sem cortar
// funcionalidade.
//
// ⚠️ **Guarda o servido e não o objecto de domínio** — a `api.Oferta` já
// traduzida. Assim um acerto é igual a uma resposta fresca por construção, e não
// por cuidado de quem escreveu o código. A alternativa, e porque se recusou,
// está na §4.
//
// ⚠️ Vive em Postgres e não no processo (§7.7). O v1 tinha estado em-processo e
// avisava no arranque que com mais do que um worker ele deixava de valer.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/bd"
)

// ValidadeOmissao é quanto tempo uma resposta serve depois de o banco a dar.
//
// ⚠️ **Cinco minutos, e é um valor de partida e não uma medição.** A §7.6 manda
// que fique curto até haver vigia, e a vigia não é uma corrida: é perguntar o
// mesmo ao mesmo banco de hora a hora e ver quando muda — o que decide é a
// PRIMEIRA mudança, não a média. Cinco minutos cobrem o padrão que existe hoje,
// a mesma pessoa a comparar e a voltar, e são curtos ao ponto de uma Euribor que
// fixa de manhã nunca ficar cinco minutos errada em cima de quem pergunta.
const ValidadeOmissao = 5 * time.Minute

// Postgres é a cache sobre um pool já aberto. Quem abre o pool fecha-o.
type Postgres struct {
	pool     *pgxpool.Pool
	validade time.Duration
}

// NovoPostgres constrói a cache. Validade nula ou negativa vale ValidadeOmissao
// — uma validade a zero é não ter cache, e ninguém quer dizer isso por engano.
func NovoPostgres(pool *pgxpool.Pool, validade time.Duration) *Postgres {
	if validade <= 0 {
		validade = ValidadeOmissao
	}
	return &Postgres{pool: pool, validade: validade}
}

// Validade é a validade em vigor, para quem a queira dizer no arranque.
func (p *Postgres) Validade() time.Duration { return p.validade }

// Ler devolve a resposta guardada para este banco e este pedido, e se havia.
//
// ⚠️ **O `capturado_em` que volta é o do banco, e não o de agora** — é o instante
// em que se falou com ele. Servir um acerto com a data de agora apresentava um
// preço de há cinco minutos como se tivesse acabado de ser cotado, que é
// exactamente o que o campo existe para impedir.
//
// ⚠️ Uma entrada expirada não volta: a validade filtra-se na consulta, e não
// depende de a limpeza ter corrido.
func (p *Postgres) Ler(
	ctx context.Context, chave string, agora time.Time,
) (api.Oferta, bool, error) {
	linha, err := bd.New(p.pool).LerRespostaEmCache(ctx, bd.LerRespostaEmCacheParams{
		Chave: chave,
		Agora: pgtype.Timestamptz{Time: agora, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.Oferta{}, false, nil
	}
	if err != nil {
		return api.Oferta{}, false, fmt.Errorf("ler a cache: %w", err)
	}

	var oferta api.Oferta
	if err := json.Unmarshal(linha.Resposta, &oferta); err != nil {
		// ⚠️ Uma linha ilegível trata-se como ausência e não como avaria: a cache
		// é uma defesa, e uma defesa que derruba o pedido quando ela própria está
		// estragada é pior do que não a ter. Vai-se ao banco.
		return api.Oferta{}, false, nil
	}

	capturado := linha.CapturadoEm.Time.UTC()
	oferta.CapturadoEm = &capturado
	emCache := true
	oferta.EmCache = &emCache
	return oferta, true, nil
}

// Gravar guarda a resposta servida, com a validade em vigor.
//
// ⚠️ **Só respostas de SUCESSO.** Guardar um `banco_indisponivel` transformava um
// soluço de segundos numa indisponibilidade de toda a validade, e multiplicava-a
// por todos os clientes com o mesmo pedido. Uma falha não guardada custa um
// pedido a mais ao banco; uma falha guardada custa uma oferta a quem a podia ter
// tido. Uma oferta em falha aqui é ignorada em silêncio, e não é erro de quem
// chama.
func (p *Postgres) Gravar(
	ctx context.Context, chave, bancoID string, oferta api.Oferta, agora time.Time,
) error {
	if !oferta.Sucesso || oferta.CapturadoEm == nil {
		return nil
	}

	// ⚠️ O `em_cache` NÃO se grava: é uma propriedade de como ESTA resposta foi
	// servida, e não da resposta. Gravado a `true` saía `true` na primeira vez
	// que se servisse do banco a seguir; gravado a `false` obrigava o `Ler` a
	// corrigi-lo à mão. Fica de fora, e o `Ler` põe-no.
	oferta.EmCache = nil

	bruto, err := json.Marshal(oferta)
	if err != nil {
		return fmt.Errorf("serializar a resposta do %q: %w", bancoID, err)
	}

	if err := bd.New(p.pool).GravarRespostaEmCache(ctx, bd.GravarRespostaEmCacheParams{
		Chave:       chave,
		BancoID:     bancoID,
		Resposta:    bruto,
		CapturadoEm: pgtype.Timestamptz{Time: *oferta.CapturadoEm, Valid: true},
		ExpiraEm:    pgtype.Timestamptz{Time: agora.Add(p.validade), Valid: true},
	}); err != nil {
		return fmt.Errorf("gravar a resposta do %q: %w", bancoID, err)
	}
	return nil
}

// LimparExpiradas apaga o que já não se pode servir, e devolve quantas linhas
// caíram. Corre por manutenção e não no caminho de um cliente.
func (p *Postgres) LimparExpiradas(ctx context.Context, agora time.Time) (int64, error) {
	caidas, err := bd.New(p.pool).LimparRespostasExpiradas(
		ctx, pgtype.Timestamptz{Time: agora, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("limpar a cache: %w", err)
	}
	return caidas, nil
}
