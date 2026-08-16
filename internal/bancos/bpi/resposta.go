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
	// Prestação única (fixa/variável): "Prestação /mês 897 ,75 EUR"
	// Nota: o browser parte o número em linhas diferentes, depois normalizamos
	// mas pode ficar "897 ,75" com espaço entre inteiro e decimal.
	regexPrestacaoUnica = regexp.MustCompile(`Prestação\s*/mês\s*([\d\.\s]+,\d+)\s*EUR`)

	// TAEG: "TAEG 4,4%"
	regexTAEG = regexp.MustCompile(`TAEG\s*([\d,]+)%`)

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
// O BPI mostra duas variantes: "com vendas" (contratado) e "sem vendas" (base).
type resultadoExtraido struct {
	// Variante com vendas associadas (contratado)
	prestacaoContratado *float64
	taegContratado      *float64

	// Variante sem vendas associadas (base)
	prestacaoBase *float64
	taegBase      *float64

	// TAN e Spread (podem ter variantes Contratada/Base)
	tanContratada    *float64
	tanBase          *float64
	spreadContratado *float64
	spreadBase       *float64

	// MTIC (pode ter variantes Contratado/Base)
	mticContratado *float64
	mticBase       *float64

	// Euribor
	euriborMeses int
	euriborValor *float64
}

// extrairResultado lê o HTML da página e devolve os valores extraídos.
//
// É pura: dados para dados, sem rede e sem relógio.
func extrairResultado(html string) (*resultadoExtraido, error) {
	// Normalizar o texto: remover tags HTML, juntar linhas partidas e espaços múltiplos.
	// O innerText do browser parte os valores em linhas separadas
	// (ex: "Prestação\n/mês\n897\n,75\nEUR"), mas os regex esperam tudo junto.
	html = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(html, " ")
	html = strings.ReplaceAll(html, "\n", " ")
	html = strings.ReplaceAll(html, "\r", "")
	html = regexp.MustCompile(`\s{2,}`).ReplaceAllString(html, " ")

	r := &resultadoExtraido{}

	// O BPI mostra duas secções: "com vendas" (contratado) e "sem vendas" (base).
	// Precisamos de extrair cada uma separadamente.
	secContratado := extrairSecao(html, "com vendas")
	secBase := extrairSecao(html, "sem vendas")

	// Prestação + TAEG da secção "com vendas" (contratado)
	if secContratado != "" {
		if m := regexPrestacaoUnica.FindStringSubmatch(secContratado); m != nil {
			v, err := parsePT(m[1])
			if err != nil {
				return nil, fmt.Errorf("prestação contratado: %w", err)
			}
			r.prestacaoContratado = &v
		}
		if m := regexTAEG.FindStringSubmatch(secContratado); m != nil {
			v, err := parsePT(m[1])
			if err != nil {
				return nil, fmt.Errorf("TAEG contratado: %w", err)
			}
			r.taegContratado = &v
		}
	}

	// Prestação + TAEG da secção "sem vendas" (base)
	if secBase != "" {
		if m := regexPrestacaoUnica.FindStringSubmatch(secBase); m != nil {
			v, err := parsePT(m[1])
			if err != nil {
				return nil, fmt.Errorf("prestação base: %w", err)
			}
			r.prestacaoBase = &v
		}
		if m := regexTAEG.FindStringSubmatch(secBase); m != nil {
			v, err := parsePT(m[1])
			if err != nil {
				return nil, fmt.Errorf("TAEG base: %w", err)
			}
			r.taegBase = &v
		}
	}

	// Fallback: se não encontrámos as secções, procurar no HTML inteiro
	if r.prestacaoContratado == nil && r.prestacaoBase == nil {
		if m := regexPrestacaoUnica.FindStringSubmatch(html); m != nil {
			v, err := parsePT(m[1])
			if err != nil {
				return nil, fmt.Errorf("prestação: %w", err)
			}
			r.prestacaoBase = &v
		}
	}
	if r.taegContratado == nil && r.taegBase == nil {
		if m := regexTAEG.FindStringSubmatch(html); m != nil {
			v, err := parsePT(m[1])
			if err != nil {
				return nil, fmt.Errorf("TAEG: %w", err)
			}
			r.taegBase = &v
		}
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
			return nil, fmt.Errorf("euribor: %w", err)
		}
		r.euriborMeses = atoi(m[1])
		r.euriborValor = &v
	}

	return r, nil
}

// extrairSecao extrai uma secção do HTML entre dois marcadores.
// Procura por "marcadorInicio" e devolve o texto até ao próximo "Valor" ou fim.
func extrairSecao(html, tipoSecao string) string {
	// Procurar pela secção (ex: "com vendas" ou "sem vendas")
	idx := strings.Index(html, tipoSecao)
	if idx == -1 {
		return ""
	}
	// Encontrar o início dos dados (após o marcador)
_inicio:
	for i := idx; i < len(html) && i < idx+500; i++ {
		if html[i] == 'P' && strings.HasPrefix(html[i:], "Prestação") {
			idx = i
			break _inicio
		}
	}
	// Procurar o fim da secção (próximo "Valor" ou "Modalidade")
	fim := len(html)
	for _, marcador := range []string{"Valor sem", "Valor com", "Modalidade de Taxa"} {
		if i := strings.Index(html[idx+10:], marcador); i > 0 {
			pos := idx + 10 + i
			if pos < fim {
				fim = pos
			}
		}
	}
	return html[idx:fim]
}

// lerResposta traduz o HTML extraído para uma oferta.
//
// É pura: dados para dados, sem rede e sem relógio.
func lerResposta(html string, p dominio.Pedido) (dominio.Oferta, error) {
	r, err := extrairResultado(html)
	if err != nil {
		return dominio.Oferta{}, ilegivel("o HTML", err)
	}

	// Verificar se temos dados mínimos
	temContratado := r.prestacaoContratado != nil || r.taegContratado != nil
	temBase := r.prestacaoBase != nil || r.taegBase != nil
	if !temContratado && !temBase {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: "Não foi possível ler os resultados BPI (layout mudou?)",
		}
	}

	produtos := p.ProdutosDoBanco(IDBanco)
	comVendas := len(produtos) > 0

	var o dominio.Oferta

	// Escolher a variante certa (contratado vs base)
	if comVendas && temContratado {
		// Usar variante com vendas associadas
		if r.taegContratado != nil {
			taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.taegContratado))
			o.TAEG = &taxa
		}
		if r.prestacaoContratado != nil {
			dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*r.prestacaoContratado))
			o.Prestacao = &dinheiro
		}
	} else {
		// Usar variante base (sem vendas)
		if r.taegBase != nil {
			taxa := dominio.TaxaDeDecimal(decimal.NewFromFloat(*r.taegBase))
			o.TAEG = &taxa
		}
		if r.prestacaoBase != nil {
			dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*r.prestacaoBase))
			o.Prestacao = &dinheiro
		}
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
	// Escolher a prestação certa (contratado vs base)
	var prestacao *float64
	if p.TemProduto(ProdutoVendasAssociadas) && r.prestacaoContratado != nil {
		prestacao = r.prestacaoContratado
	} else if r.prestacaoBase != nil {
		prestacao = r.prestacaoBase
	} else if r.prestacaoContratado != nil {
		prestacao = r.prestacaoContratado
	}

	if prestacao == nil {
		return nil
	}

	// Uma só fase (o BPI não dá duração das fases na mista)
	taxa := dominio.TaxaDeDecimal(decimal.NewFromInt(0))
	dinheiro := dominio.DinheiroDeDecimal(decimal.NewFromFloat(*prestacao))
	return []dominio.Fase{
		{AteMes: p.PrazoAnos * 12, Taxa: taxa, Prestacao: dinheiro},
	}
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
	} else if strings.Contains(limpo, ".") {
		// Sem vírgula: o ponto é separador de milhares (formato PT).
		limpo = strings.ReplaceAll(limpo, ".", "")
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
	_, _ = fmt.Sscanf(s, "%d", &v)
	return v
}

func ilegivel(oQue string, err error) *dominio.ErroOferta {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroRespostaIlegivel,
		Mensagem: fmt.Sprintf("Não se conseguiu ler %s da resposta do Banco BPI: %v", oQue, err),
	}
}
