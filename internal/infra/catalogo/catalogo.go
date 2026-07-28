// Package catalogo grava e lê o catalogo_taxas — a série de que sai toda a
// resposta ao cliente (ARQUITETURA.md §1 e §4).
//
// É a implementação da porta varrimento.Catalogo. A tradução de uma
// dominio.Oferta para uma linha vive aqui, e não no domínio nem no caso de uso:
// é a fronteira que conhece pgtype, jsonb e os CHECK da tabela.
package catalogo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/bd"
)

// Postgres é o catálogo sobre um pool já aberto. Quem abre o pool fecha-o.
type Postgres struct {
	pool *pgxpool.Pool
}

func NovoPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

var _ varrimento.Catalogo = (*Postgres)(nil)

// UltimoVarrimentoEm serve a guarda de idempotência. O bool falso é «nunca se
// varreu», que é diferente de «varreu-se há muito tempo».
func (p *Postgres) UltimoVarrimentoEm(ctx context.Context) (time.Time, bool, error) {
	quando, err := bd.New(p.pool).UltimoVarrimentoEm(ctx)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("ler o último varrimento: %w", err)
	}
	if !quando.Valid {
		return time.Time{}, false, nil
	}
	return quando.Time, true, nil
}

// GravarLote grava as observações de uma corrida sob um varrimento_id novo.
//
// ⚠️ Tudo numa transacção. Um lote meio gravado deixaria na série uma corrida
// com bancos a faltar e nada a dizer que faltavam — e quem a lesse concluía que
// esses bancos não tinham respondido.
func (p *Postgres) GravarLote(ctx context.Context, obs []varrimento.Observacao) (string, error) {
	if len(obs) == 0 {
		return "", fmt.Errorf("lote sem observações")
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("abrir transacção do lote: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := bd.New(p.pool).WithTx(tx)

	id, err := q.NovoVarrimentoID(ctx)
	if err != nil {
		return "", fmt.Errorf("cunhar o varrimento_id: %w", err)
	}

	for i, o := range obs {
		params, err := linha(id, o)
		if err != nil {
			return "", fmt.Errorf("observação %d (%s, %s): %w", i+1, o.Oferta.BancoID, o.Ponto.Cenario, err)
		}
		if _, err := q.InserirTaxa(ctx, params); err != nil {
			return "", fmt.Errorf("gravar a observação %d (%s, %s): %w", i+1, o.Oferta.BancoID, o.Ponto.Cenario, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("fechar a transacção do lote: %w", err)
	}
	return uuidTexto(id), nil
}

// nucleares são os campos que uma linha de sucesso tem de trazer, e o CHECK
// catalogo_taxas_resposta_completa_quando_sucesso impõe-nos.
//
// ⚠️ A §4 é explícita sobre o que fazer quando falta um: «um nulo aqui não é
// meio-dado, é resposta mal lida, e isso é uma falha (sucesso = false)». Por
// isso a linha desce a falha em vez de a inserção rebentar e levar o lote
// inteiro atrás — mas o erro NOMEIA o campo em falta, para não parecer que o
// banco esteve em baixo quando o que houve foi uma resposta incompleta.
func nucleares(o dominio.Oferta) []string {
	var falta []string
	if o.TAN == nil {
		falta = append(falta, "TAN")
	}
	if o.TAEG == nil {
		falta = append(falta, "TAEG")
	}
	if o.Prestacao == nil {
		falta = append(falta, "prestação")
	}
	if o.MTIC == nil {
		falta = append(falta, "MTIC")
	}
	return falta
}

// indexados são os campos que só existem onde há Euribor — variável e mista.
// Numa taxa fixa pura não há indexante nem spread sobre ele.
func indexados(p dominio.Pedido, o dominio.Oferta) []string {
	if p.TipoTaxa != dominio.TaxaVariavel && p.TipoTaxa != dominio.TaxaMista {
		return nil
	}
	var falta []string
	if o.Spread == nil {
		falta = append(falta, "spread")
	}
	if o.Indexante == "" {
		falta = append(falta, "indexante")
	}
	if o.EuriborValor == nil {
		falta = append(falta, "valor da Euribor")
	}
	return falta
}

// linha traduz uma observação para os parâmetros da inserção.
//
// ⚠️ ltv_min, ltv_max e spread_minimo só se preenchem quando a observação é um
// degrau da escala. Numa observação de ponto ficam nulos: ela não afirma
// intervalo de LTV nenhum, e o LTV dela continua derivável de
// montante/valor_imovel, que vão na linha (§4).
func linha(id pgtype.UUID, o varrimento.Observacao) (bd.InserirTaxaParams, error) {
	p, oferta := o.Ponto.Pedido, o.Oferta

	// ⚠️ Sem instante de captura não se grava. A coluna é timestamptz NOT NULL,
	// e quem carimba a hora é o varrimento — uma observação sem ela não veio de
	// uma corrida, veio de código que a construiu à mão.
	if oferta.CapturadoEm.IsZero() {
		return bd.InserirTaxaParams{}, fmt.Errorf("observação sem instante de captura")
	}

	sucesso := oferta.Sucesso()
	erro := ""
	if !sucesso {
		erro = oferta.Erro.Mensagem
	}

	// A resposta incompleta desce a falha, com o campo nomeado. Ver `nucleares`.
	if sucesso {
		if falta := append(nucleares(oferta), indexados(p, oferta)...); len(falta) > 0 {
			sucesso = false
			erro = fmt.Sprintf(
				"resposta incompleta do %s: faltou %s. Uma linha de sucesso traz a resposta nuclear completa (§4); "+
					"um nulo aqui não é meio-dado, é resposta mal lida.",
				oferta.BancoNome, junta(falta))
		}
	}

	// ⚠️ O spread da linha e o do degrau têm de ser o MESMO número.
	//
	// A §4 manda que a observação representativa de um degrau seja a do spread
	// servido, e até aqui isso era promessa de quem constrói. Uma linha em que
	// discordem é o defeito que esta issue existe para não deixar acontecer: o
	// `spread` de um preço e a `tan`/`taeg`/`prestacao_mensal`/`mtic` de outro.
	// Nenhum CHECK da tabela o apanha — cada coluna, sozinha, é válida.
	//
	// Sobe como erro e derruba o lote, e não desce a falha: uma resposta
	// incompleta é do banco, isto é nosso. É a mesma escolha que a linha sem
	// instante de captura já faz acima.
	if sucesso && o.Degrau != nil && oferta.Spread != nil && !o.Degrau.Spread.Equal(*oferta.Spread) {
		return bd.InserirTaxaParams{}, fmt.Errorf(
			"degrau (%s; %s] serve o spread %s e a observação que o preenche mediu %s: "+
				"a linha ficaria com o spread de um preço e o TAEG de outro (§4)",
			o.Degrau.De, o.Degrau.Ate, o.Degrau.Spread, *oferta.Spread)
	}

	// ⚠️ Um degrau cuja observação desceu a falha perde o intervalo, e não o
	// contrário. Os CHECK do intervalo exigem `spread` não nulo do lado caro, e
	// uma linha de falha não tem spread — mas o que interessa não é o CHECK: um
	// intervalo agarrado a uma resposta que não se leu afirmaria que o preço vale
	// de ltv_min a ltv_max sem haver preço nenhum medido.
	ltvMin, ltvMax, spreadMinimo := intervalo(o.Degrau, sucesso)

	// O resíduo da §7.4, medido aqui e não transportado na observação.
	//
	// ⚠️ É uma função pura sobre a observação inteira, e é por isso que os
	// degraus da escala também o trazem: eles não passam pelo `simular` — o
	// amostrador chama o `b.Simular` directamente —, e um campo a preencher em
	// dois caminhos esquece-se num deles. Foi assim que o `capturado_em` quase
	// ficou a zero em todas as linhas de degrau.
	//
	// ⚠️ Mede-se sobre o `sucesso` já corrigido acima: uma resposta incompleta
	// desceu a falha, e a linha dela não afirma resíduo nenhum.
	residuo := pgtype.Numeric{}
	if sucesso {
		if r, ok := varrimento.Residuo(o); ok {
			residuo = numero(r.Decimal())
		}
	}

	produtos, err := paraJSON(oferta.ProdutosAplicados, "produtos")
	if err != nil {
		return bd.InserirTaxaParams{}, err
	}
	aplicado, err := paraJSON(oferta.Aplicado(), "aplicado")
	if err != nil {
		return bd.InserirTaxaParams{}, err
	}
	notas, err := paraJSON(oferta.Notas(), "notas")
	if err != nil {
		return bd.InserirTaxaParams{}, err
	}

	return bd.InserirTaxaParams{
		VarrimentoID:     id,
		CapturadoEm:      pgtype.Timestamptz{Time: oferta.CapturadoEm, Valid: true},
		Cenario:          o.Ponto.Cenario,
		BancoID:          oferta.BancoID,
		BancoNome:        oferta.BancoNome,
		RateType:         string(p.TipoTaxa),
		ValorImovel:      numero(p.ValorImovel.Decimal()),
		Montante:         numero(p.Montante.Decimal()),
		PrazoAnos:        int32(p.PrazoAnos), //nolint:gosec // o Validar do Pedido tem-no entre 1 e 50
		FixedPeriodYears: inteiro(p.PeriodoFixoAnos),
		EuriborIndexante: textoOuNulo(string(oferta.Indexante)),
		Tan:              taxa(oferta.TAN),
		Taeg:             taxa(oferta.TAEG),
		Spread:           taxa(oferta.Spread),
		EuriborValor:     taxa(oferta.EuriborValor),
		PrestacaoMensal:  dinheiro(oferta.Prestacao),
		Mtic:             dinheiro(oferta.MTIC),
		LtvMin:           ltvMin,
		LtvMax:           ltvMax,
		SpreadMinimo:     spreadMinimo,
		ResiduoPrestacao: residuo,
		Produtos:         produtos,
		Aplicado:         aplicado,
		Notas:            notas,
		Sucesso:          sucesso,
		Erro:             textoOuNulo(erro),
	}, nil
}

// paraJSON serializa para jsonb. ⚠️ Uma lista nula sai como `[]` e não como
// `null`: a coluna é NOT NULL DEFAULT '[]' e o /api/rate-catalog exige
// `products` sempre lista (API.md §2). Vazio é informação — «nada aplicado» —
// e nulo seria «não sei».
func paraJSON(v any, campo string) ([]byte, error) {
	switch t := v.(type) {
	case []string:
		if t == nil {
			v = []string{}
		}
	case map[string]any:
		if t == nil {
			v = map[string]any{}
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("serializar %s: %w", campo, err)
	}
	return b, nil
}

// intervalo devolve as três colunas do degrau, ou três nulos.
//
// O spread_minimo é nulo num degrau resolvido, e o `resolvido` deriva-se dele —
// não se guarda a mesma verdade duas vezes (§4).
func intervalo(d *dominio.DegrauLTV, sucesso bool) (min, max, minimo pgtype.Numeric) {
	if d == nil || !sucesso {
		return pgtype.Numeric{}, pgtype.Numeric{}, pgtype.Numeric{}
	}
	min = numero(d.De.Decimal())
	max = numero(d.Ate.Decimal())
	if d.SpreadMinimo != nil {
		minimo = numero(d.SpreadMinimo.Decimal())
	}
	return min, max, minimo
}

func numero(d decimal.Decimal) pgtype.Numeric {
	return pgtype.Numeric{Int: d.Coefficient(), Exp: d.Exponent(), Valid: true}
}

func taxa(t *dominio.Taxa) pgtype.Numeric {
	if t == nil {
		return pgtype.Numeric{}
	}
	return numero(t.Decimal())
}

func dinheiro(d *dominio.Dinheiro) pgtype.Numeric {
	if d == nil {
		return pgtype.Numeric{}
	}
	return numero(d.Decimal())
}

func inteiro(n *int) pgtype.Int4 {
	if n == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*n), Valid: true} //nolint:gosec // períodos fixos são unidades de anos
}

func textoOuNulo(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// uuidTexto escreve o UUID na forma canónica com hífenes.
func uuidTexto(id pgtype.UUID) string {
	b := id.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func junta(s []string) string {
	if len(s) == 1 {
		return s[0]
	}
	saida := ""
	for i, v := range s {
		switch {
		case i == 0:
			saida = v
		case i == len(s)-1:
			saida += " e " + v
		default:
			saida += ", " + v
		}
	}
	return saida
}
