// Package bpi é o simulador de crédito à habitação do Banco BPI (grupo
// CaixaBank).
//
// O simulador é uma app OutSystems Traditional Web (postbacks com __OSVSTATE),
// sem API JSON limpa — por isso conduzimos o formulário com Playwright e lemos
// os resultados do DOM.
//
// O simulador é público e indicativo (não vinculativo), sem lead nem reCAPTCHA.
//
// Limitações conhecidas:
//   - Prazo máximo 36 anos (perfis acima são limitados a 36).
//   - Não pede valor do imóvel (só montante + prazo) → LTV não aplicável.
//   - Duas variantes: "base" (sem vendas associadas) e "contratado" (com seguros).
package bpi

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A identidade do banco, e os produtos que ele expõe pelos ids que os
// Requisitos publicam.
const (
	IDBanco   = "bpi"
	NomeBanco = "Banco BPI"
)

const (
	// ProdutoVendasAssociadas é o pacote de vendas associadas (seguros).
	ProdutoVendasAssociadas = "bpi:vendas_associadas"
)

// Os limites que o banco pratica, medidos a 2026-08-16.
const (
	PrazoMinAnos = 11
	PrazoMaxAnos = 36

	// IdadeMaximaFim é o tecto do contrato: o prazo tem de terminar até aos
	// 70 anos (medido no v1).
	IdadeMaximaFim = 70
)

// Períodos fixos válidos para a taxa mista.
var PeriodosFixosMista = []int{3, 5, 10}

// regexp para extrair valores do HTML do BPI.
var (
	// Prestação do período fixo (mista): "Período Taxa Fixa (Prestação) /mês 1.161,63 EUR"
	regexPrestacaoFixa = regexp.MustCompile(`Período Taxa Fixa \(Prestação\)\s*/mês\s*([\d\.\s]+,\d+)\s*EUR`)

	// Prestação do período variável (mista): "Período Taxa Variável (Prestação) /mês 1.300,29 EUR"
	regexPrestacaoVariavel = regexp.MustCompile(`Período Taxa Variável \(Prestação\)\s*/mês\s*([\d\.\s]+,\d+)\s*EUR`)

	// Prestação única (fixa/variável): "Prestação /mês 1.300,29 EUR"
	regexPrestacaoUnica = regexp.MustCompile(`Prestação\s*/mês\s*([\d\.\s]+,\d+)\s*EUR`)

	// TAEG: "TAEG 4,4 %"
	regexTAEG = regexp.MustCompile(`TAEG\s*([\d,]+)\s*%`)

	// TAN: "TAN Contratada 3,497 %" ou "TAN Base 4,247 %"
	regexTAN = regexp.MustCompile(`TAN\s+(Contratad[ao]|Base)\s*([\d\.\s]*\d,\d+)\s*%`)

	// Spread: "Spread Contratado 0,850 %" ou "Spread Base 1,600 %"
	regexSpread = regexp.MustCompile(`Spread\s+(Contratad[ao]|Base)\s*([\d\.\s]*\d,\d+)\s*%`)

	// MTIC: "MTIC Contratado 384.541,08 €" ou "MTIC Base 415.234,56 €"
	regexMTIC = regexp.MustCompile(`MTIC\s+(Contratad[ao]|Base)\s*([\d\.\s]*\d,\d+)\s*€`)

	// Euribor: "Euribor 6 meses ( 2,647 %)"
	regexEuribor = regexp.MustCompile(`Euribor\s+(\d+)\s+mes\w*\s*\(\s*([\d\.\s]*\d,\d+)\s*%\s*\)`)
)

// resultadoExtraido é o resultado da extração do HTML.
type resultadoExtraido struct {
	prestacaoFixa     *float64
	prestacaoVariavel *float64
	taeg              *float64
	tanContratada     *float64
	tanBase           *float64
	spreadContratado  *float64
	spreadBase        *float64
	mticContratado    *float64
	mticBase          *float64
	euriborMeses      int
	euriborValor      *float64
}

// extrairResultado lê o HTML da página e devolve os valores extraídos.
//
// É pura: dados para dados, sem rede e sem relógio.
func extrairResultado(html string) (*resultadoExtraido, error) {
	r := &resultadoExtraido{}

	// Prestação
	if m := regexPrestacaoFixa.FindStringSubmatch(html); m != nil {
		v, err := parsePT(m[1])
		if err != nil {
			return nil, fmt.Errorf("prestação fixa: %w", err)
		}
		r.prestacaoFixa = &v
	}
	if m := regexPrestacaoVariavel.FindStringSubmatch(html); m != nil {
		v, err := parsePT(m[1])
		if err != nil {
			return nil, fmt.Errorf("prestação variável: %w", err)
		}
		r.prestacaoVariavel = &v
	}
	if m := regexPrestacaoUnica.FindStringSubmatch(html); m != nil {
		v, err := parsePT(m[1])
		if err != nil {
			return nil, fmt.Errorf("prestação única: %w", err)
		}
		if r.prestacaoFixa == nil {
			r.prestacaoFixa = &v
		}
	}

	// TAEG
	if m := regexTAEG.FindStringSubmatch(html); m != nil {
		v, err := parsePT(m[1])
		if err != nil {
			return nil, fmt.Errorf("TAEG: %w", err)
		}
		r.taeg = &v
	}

	// TAN (procurar pela Contratada primeiro)
	for _, m := range regexTAN.FindAllStringSubmatch(html, -1) {
		variante := strings.ToLower(m[1])
		v, err := parsePT(m[2])
		if err != nil {
			return nil, fmt.Errorf("TAN %s: %w", variante, err)
		}
		if strings.Contains(variante, "contratad") {
			r.tanContratada = &v
		} else {
			r.tanBase = &v
		}
	}

	// Spread (procurar pelo Contratado primeiro)
	for _, m := range regexSpread.FindAllStringSubmatch(html, -1) {
		variante := strings.ToLower(m[1])
		v, err := parsePT(m[2])
		if err != nil {
			return nil, fmt.Errorf("spread %s: %w", variante, err)
		}
		if strings.Contains(variante, "contratad") {
			r.spreadContratado = &v
		} else {
			r.spreadBase = &v
		}
	}

	// MTIC (procurar pelo Contratado primeiro)
	for _, m := range regexMTIC.FindAllStringSubmatch(html, -1) {
		variante := strings.ToLower(m[1])
		v, err := parsePT(m[2])
		if err != nil {
			return nil, fmt.Errorf("MTIC %s: %w", variante, err)
		}
		if strings.Contains(variante, "contratad") {
			r.mticContratado = &v
		} else {
			r.mticBase = &v
		}
	}

	// Euribor
	if m := regexEuribor.FindStringSubmatch(html); m != nil {
		v, err := parsePT(m[2])
		if err != nil {
			return nil, fmt.Errorf("Euribor: %w", err)
		}
		r.euriborMeses = atoi(m[1])
		r.euriborValor = &v
	}

	return r, nil
}

// lerResposta traduz o HTML extraído para uma oferta.
//
// É pura: dados para dados, sem rede e sem relógio.
func lerResposta(html string, p dominio.Pedido) (dominio.Oferta, error) {
	r, err := extrairResultado(html)
	if err != nil {
		return dominio.Oferta{}, ilegivel("o HTML", err)
	}

	if r.taeg == nil && r.prestacaoFixa == nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: "Não foi possível ler os resultados BPI (layout mudou?)",
		}
	}

	produtos := p.ProdutosDoBanco(IDBanco)
	comVendas := len(produtos) > 0

	var o dominio.Oferta

	// TAEG
	if r.taeg != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.taeg))
		o.TAEG = &taxa
	}

	// TAN — usar a Contratada se tiver vendas, senão a Base
	if comVendas && r.tanContratada != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.tanContratada))
		o.TAN = &taxa
	} else if r.tanBase != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.tanBase))
		o.TAN = &taxa
	} else if r.tanContratada != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.tanContratada))
		o.TAN = &taxa
	}

	// Spread — usar o Contratado se tiver vendas, senão o Base
	if comVendas && r.spreadContratado != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.spreadContratado))
		o.Spread = &taxa
	} else if r.spreadBase != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.spreadBase))
		o.Spread = &taxa
	} else if r.spreadContratado != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.spreadContratado))
		o.Spread = &taxa
	}

	// MTIC — usar o Contratado se tiver vendas, senão o Base
	if comVendas && r.mticContratado != nil {
		dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*r.mticContratado))
		o.MTIC = &dinheiro
	} else if r.mticBase != nil {
		dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*r.mticBase))
		o.MTIC = &dinheiro
	} else if r.mticContratado != nil {
		dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*r.mticContratado))
		o.MTIC = &dinheiro
	}

	// Prestação — usar a do período fixo se tiver, senão a única
	if r.prestacaoFixa != nil {
		dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*r.prestacaoFixa))
		o.Prestacao = &dinheiro
	} else if r.prestacaoVariavel != nil {
		dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*r.prestacaoVariavel))
		o.Prestacao = &dinheiro
	}

	// Euribor
	if r.euriborValor != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.euriborValor))
		o.EuriborValor = &taxa
		switch r.euriborMeses {
		case 3:
			o.Indexante = dominio.Euribor3M
		case 6:
			o.Indexante = dominio.Euribor6M
		case 12:
			o.Indexante = dominio.Euribor12M
		}
	}

	// Fases
	o.Fases = lerFases(r, p)

	// Anotações
	if comVendas {
		o.Anotar("Este preço pressupõe as vendas associadas do Banco BPI — seguros contratados com o banco.")
	} else {
		o.Anotar("Este preço não inclui as vendas associadas do Banco BPI. " +
			"Escolhê-las desconta a prestação e a TAEG.")
	}

	o.ProdutosAplicados = produtos
	return o, nil
}

// lerFases monta o plano de prestações a partir do HTML extraído.
func lerFases(r *resultadoExtraido, p dominio.Pedido) []dominio.Fase {
	// Na mista, temos duas fases
	if p.TipoTaxa == dominio.TaxaMista && r.prestacaoFixa != nil && r.prestacaoVariavel != nil {
		// ⚠️ O BPI não diz a duração de cada fase no HTML — só a prestação.
		// Assumir que o período fixo é o que o pedido pediu, e o variável é o
		// restante. Isto é uma aproximação; o ideal seria capturar o HTML e
		// extrair as durações.
		var fixo, variavel float64
		if r.prestacaoFixa != nil {
			fixo = *r.prestacaoFixa
		}
		if r.prestacaoVariavel != nil {
			variavel = *r.prestacaoVariavel
		}
		_ = fixo
		_ = variavel

		// ⚠️ Sem as durações exactas, não podemos montar fases precisas.
		// Devolver uma só fase com a prestação do período fixo.
		taxa := dominio.TaxaDeDecimal(decimal.NewFromInt(0))
		dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(fixo))
		return []dominio.Fase{
			{AteMes: p.PrazoAnos * 12, Taxa: taxa, Prestacao: dinheiro},
		}
	}

	// Variável ou fixa: uma só fase
	if r.prestacaoFixa != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromInt(0))
		dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*r.prestacaoFixa))
		return []dominio.Fase{
			{AteMes: p.PrazoAnos * 12, Taxa: taxa, Prestacao: dinheiro},
		}
	}
	if r.prestacaoVariavel != nil {
		taxa := dominio.TaxaDeDecimal(decimal.NewFromInt(0))
		dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*r.prestacaoVariavel))
		return []dominio.Fase{
			{AteMes: p.PrazoAnos * 12, Taxa: taxa, Prestacao: dinheiro},
		}
	}

	return nil
}

// parsePT lê um número em formato português (vírgula decimal, ponto de milhares).
func parsePT(s string) (float64, error) {
	limpo := strings.NewReplacer(" ", "", "\u00a0", "", "%", "", "€", "").Replace(s)
	if limpo == "" {
		return 0, fmt.Errorf("número vazio")
	}
	if strings.Contains(limpo, ",") {
		limpo = strings.ReplaceAll(limpo, ".", "")
		limpo = strings.ReplaceAll(limpo, ",", ".")
	}
	var v float64
	_, err := fmt.Sscanf(limpo, "%f", &v)
	if err != nil {
		return 0, fmt.Errorf("número inválido %q: %w", s, err)
	}
	return v, nil
}

func atoi(s string) int {
	var v int
	fmt.Sscanf(s, "%d", &v)
	return v
}

func ilegivel(oQue string, err error) *dominio.ErroOferta {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroRespostaIlegivel,
		Mensagem: fmt.Sprintf("Não se conseguiu ler %s da resposta do Banco BPI: %v", oQue, err),
	}
}
