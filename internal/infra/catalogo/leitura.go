package catalogo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/bd"
)

// A leitura do catálogo: de linhas de `catalogo_taxas` para observações com que
// o `aplicacao/comparar` responde.
//
// ⚠️ É o caminho inverso do `linha()`, e a simetria não é completa **de
// propósito**: o que se grava é uma observação, e o que se lê é o que a resposta
// precisa. Três coisas não voltam, e cada uma tem razão:
//
//   - o **plano de fases** não se grava (a §4 não lhe deu coluna). O que dele se
//     precisa é o prazo, e esse deriva-se — ver `varrimento.PrazoAplicado`;
//   - os **titulares** não existem na tabela, e é o ponto inteiro da §4: esta
//     série não tem dados pessoais. A resposta não precisa deles;
//   - as **notas** do varrimento não voltam. Elas descrevem o que aconteceu ao
//     pedido NEUTRO da grelha, e repeti-las a um cliente seria contar-lhe a
//     história de outra simulação.

// ErrSemVarrimento: a base ainda não tem nenhuma corrida gravada.
var ErrSemVarrimento = errors.New("não há varrimento nenhum gravado")

// UltimoVarrimento lê as observações da corrida mais recente.
//
// ⚠️ De **uma** corrida, e não das últimas N horas. Misturar varrimentos era
// servir preços de momentos diferentes na mesma comparação — e o resíduo da
// §7.4, que se mede por corrida, deixava de dizer alguma coisa sobre o conjunto
// que se serviu.
func (p *Postgres) UltimoVarrimento(ctx context.Context) ([]varrimento.Observacao, error) {
	q := bd.New(p.pool)

	id, err := q.UltimoVarrimentoID(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSemVarrimento
	}
	if err != nil {
		return nil, fmt.Errorf("ler o último varrimento: %w", err)
	}

	linhas, err := q.ObservacoesDoVarrimento(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("ler as observações do varrimento %s: %w", uuidTexto(id), err)
	}

	obs := make([]varrimento.Observacao, 0, len(linhas))
	for i, l := range linhas {
		o, err := observacaoDe(l)
		if err != nil {
			return nil, fmt.Errorf("observação %d do varrimento %s: %w", i+1, uuidTexto(id), err)
		}
		obs = append(obs, o)
	}
	return obs, nil
}

// observacaoDe reconstrói uma observação a partir de uma linha.
func observacaoDe(l bd.ObservacoesDoVarrimentoRow) (varrimento.Observacao, error) {
	cenario, err := grelha.LerCenario(l.Cenario)
	if err != nil {
		// ⚠️ Falha alto e não devolve um ponto plausível. Uma chave que não se
		// reconhece é uma mudança de formato, e servir o que dela se conseguiu
		// ler era o modo de falha da §7.4 — um número certo no cenário errado.
		return varrimento.Observacao{}, fmt.Errorf("cenário ilegível %q: %w", l.Cenario, err)
	}

	pedido := dominio.Pedido{
		ValorImovel: dinheiroDe(l.ValorImovel),
		Montante:    dinheiroDe(l.Montante),
		PrazoAnos:   int(l.PrazoAnos),
		TipoTaxa:    cenario.TipoTaxa,
		Finalidade:  cenario.Finalidade,
		Localizacao: dominio.LocalizacaoContinente,
	}
	if l.FixedPeriodYears.Valid {
		anos := int(l.FixedPeriodYears.Int32)
		pedido.PeriodoFixoAnos = &anos
	}

	produtos, err := listaDeJSON(l.Produtos)
	if err != nil {
		return varrimento.Observacao{}, fmt.Errorf("produtos: %w", err)
	}
	pedido.Produtos = produtos

	oferta := dominio.Oferta{
		BancoID:           l.BancoID,
		BancoNome:         l.BancoNome,
		TAN:               taxaDe(l.Tan),
		TAEG:              taxaDe(l.Taeg),
		Spread:            taxaDe(l.Spread),
		EuriborValor:      taxaDe(l.EuriborValor),
		Prestacao:         montanteDe(l.PrestacaoMensal),
		MTIC:              montanteDe(l.Mtic),
		ProdutosAplicados: produtos,
		CapturadoEm:       l.CapturadoEm.Time,
	}
	if l.EuriborIndexante.Valid {
		oferta.Indexante = dominio.Indexante(l.EuriborIndexante.String)
	}

	// ⚠️ A base da taxa fixa volta como TAN da observação, e não como um campo
	// novo. É o que o `varrimento.BaseDaTaxaFixa` reencontra ao subtrair-lhe o
	// spread do degrau — e é assim que uma observação lida da base e uma
	// observada em memória se comportam da mesma maneira no `comparar`.
	//
	// Sem base gravada, a linha de taxa fixa fica com a TAN medida e o comparar
	// recusa-a: é o que a §4 manda («grava-se, e não se serve»).
	if l.BaseFixa.Valid && pedido.TipoTaxa == dominio.TaxaFixa {
		base := taxaDe(l.BaseFixa)
		spread := taxaDe(l.Spread)
		if base != nil && spread == nil {
			// A linha de fixa não tem spread — é o normal —, por isso a TAN que
			// se reconstrói é a base mais o spread do degrau em que foi medida,
			// e isso reconstitui-se no comparar. Guarda-se a base como TAN e o
			// degrau responde pelo resto.
			oferta.TAN = base
		}
	}

	obs := varrimento.Observacao{
		Ponto:  varrimento.Ponto{Cenario: l.Cenario, Pedido: pedido},
		Oferta: oferta,
	}

	degrau, err := degrauDe(l)
	if err != nil {
		return varrimento.Observacao{}, err
	}
	obs.Degrau = degrau
	return obs, nil
}

// degrauDe reconstrói o intervalo de LTV quando a linha é um degrau da escala.
func degrauDe(l bd.ObservacoesDoVarrimentoRow) (*dominio.DegrauLTV, error) {
	if !l.LtvMin.Valid || !l.LtvMax.Valid {
		return nil, nil
	}
	spread := taxaDe(l.Spread)
	if spread == nil {
		return nil, fmt.Errorf("degrau com intervalo e sem spread — a linha diz sobre que LTV vale um preço que não traz")
	}

	d := &dominio.DegrauLTV{
		De:     racioDe(l.LtvMin),
		Ate:    racioDe(l.LtvMax),
		Spread: *spread,
	}
	if l.SpreadMinimo.Valid {
		minimo := taxaDe(l.SpreadMinimo)
		d.SpreadMinimo = minimo
	}
	return d, nil
}

// --- as conversões da fronteira -------------------------------------------------

func decimalDe(n pgtype.Numeric) decimal.Decimal {
	if !n.Valid || n.Int == nil {
		return decimal.Zero
	}
	return decimal.NewFromBigInt(n.Int, n.Exp)
}

func taxaDe(n pgtype.Numeric) *dominio.Taxa {
	if !n.Valid {
		return nil
	}
	t := dominio.TaxaDeDecimal(decimalDe(n))
	return &t
}

func montanteDe(n pgtype.Numeric) *dominio.Dinheiro {
	if !n.Valid {
		return nil
	}
	d := dominio.DinheiroDeDecimal(decimalDe(n))
	return &d
}

func dinheiroDe(n pgtype.Numeric) dominio.Dinheiro {
	return dominio.DinheiroDeDecimal(decimalDe(n))
}

func racioDe(n pgtype.Numeric) dominio.Racio {
	return dominio.RacioDeDecimal(decimalDe(n))
}

// listaDeJSON lê uma coluna jsonb de strings. ⚠️ Nulo e `[]` dão os dois uma
// lista vazia: a coluna é NOT NULL DEFAULT '[]', e vazio quer dizer «nenhum
// produto», que é informação e não ausência dela.
func listaDeJSON(b []byte) ([]string, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var lista []string
	if err := json.Unmarshal(b, &lista); err != nil {
		return nil, fmt.Errorf("ler a lista %q: %w", string(b), err)
	}
	if len(lista) == 0 {
		return nil, nil
	}
	return lista, nil
}
