package bpi

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

func replaceAll(s, old, new string) string {
	return strings.ReplaceAll(s, old, new)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestExtrairResultadoVariavel30a testa a extração contra a captura real
// do BPI com Taxa Variável, 200.000€, 30 anos.
func TestExtrairResultadoVariavel30a(t *testing.T) {
	html, err := os.ReadFile("capturas/variavel_30a.html")
	if err != nil {
		t.Fatalf("ler captura: %v", err)
	}

	// Debug: verificar se as secções são encontradas
	normalizado := string(html)
	normalizado = replaceAll(normalizado, "\n", " ")
	normalizado = replaceAll(normalizado, "\r", "")
	normalizado = regexp.MustCompile(`\s{2,}`).ReplaceAllString(normalizado, " ")

	secContratado := extrairSecao(normalizado, "com vendas")
	secBase := extrairSecao(normalizado, "sem vendas")
	t.Logf("secContratado len=%d, secBase len=%d", len(secContratado), len(secBase))
	if len(secContratado) > 0 {
		t.Logf("secContratado[:200]=%q", secContratado[:min(200, len(secContratado))])
	}
	if len(secBase) > 0 {
		t.Logf("secBase[:200]=%q", secBase[:min(200, len(secBase))])
	}

	r, err := extrairResultado(string(html))
	if err != nil {
		t.Fatalf("extrairResultado: %v", err)
	}

	// Variante com vendas associadas (contratado)
	if r.prestacaoContratado == nil {
		t.Error("prestacaoContratado: nil, queria 897.75")
	} else {
		esperado := 897.75
		if diff := sub(*r.prestacaoContratado, esperado); diff.Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
			t.Errorf("prestacaoContratado = %f, queria %f (diff %s)", *r.prestacaoContratado, esperado, diff)
		}
	}
	if r.taegContratado == nil {
		t.Error("taegContratado: nil, queria 4.4")
	} else {
		esperado := 4.4
		if diff := sub(*r.taegContratado, esperado); diff.Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
			t.Errorf("taegContratado = %f, queria %f (diff %s)", *r.taegContratado, esperado, diff)
		}
	}

	// Variante sem vendas (base)
	if r.prestacaoBase == nil {
		t.Error("prestacaoBase: nil, queria 983.53")
	} else {
		esperado := 983.53
		if diff := sub(*r.prestacaoBase, esperado); diff.Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
			t.Errorf("prestacaoBase = %f, queria %f (diff %s)", *r.prestacaoBase, esperado, diff)
		}
	}
	if r.taegBase == nil {
		t.Error("taegBase: nil, queria 5.1")
	} else {
		esperado := 5.1
		if diff := sub(*r.taegBase, esperado); diff.Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
			t.Errorf("taegBase = %f, queria %f (diff %s)", *r.taegBase, esperado, diff)
		}
	}
}

// TestLerRespostaComVendas testa que lerResposta escolhe a variante "contratado"
// quando o pedido inclui vendas associadas.
func TestLerRespostaComVendas(t *testing.T) {
	html, err := os.ReadFile("capturas/variavel_30a.html")
	if err != nil {
		t.Fatalf("ler captura: %v", err)
	}

	p := dominio.Pedido{
		Montante:  dominio.DinheiroDeInteiro(200000),
		PrazoAnos: 30,
		TipoTaxa:  dominio.TaxaVariavel,
		Produtos:  []string{ProdutoVendasAssociadas},
	}

	o, err := lerResposta(string(html), p)
	if err != nil {
		t.Fatalf("lerResposta: %v", err)
	}

	// Deve usar a variante com vendas
	if o.TAEG == nil {
		t.Fatal("TAEG: nil")
	}
	taeg := o.TAEG.Decimal()
	esperado := 4.4
	if diff := sub(taeg.InexactFloat64(), esperado); diff.Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
		t.Errorf("TAEG = %s, queria %f", taeg, esperado)
	}

	if o.Prestacao == nil {
		t.Fatal("Prestacao: nil")
	}
	prestacao := o.Prestacao.Decimal()
	esperadoPrestacao := 897.75
	if diff := sub(prestacao.InexactFloat64(), esperadoPrestacao); diff.Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
		t.Errorf("Prestacao = %s, queria %f", prestacao, esperadoPrestacao)
	}

	// Deve ter anotação sobre vendas
	notas := o.Notas()
	if len(notas) == 0 {
		t.Error("Notas: vazia, queria anotação sobre vendas")
	}
}

// TestLerRespostaSemVendas testa que lerResposta escolhe a variante "base"
// quando o pedido não inclui vendas associadas.
func TestLerRespostaSemVendas(t *testing.T) {
	html, err := os.ReadFile("capturas/variavel_30a.html")
	if err != nil {
		t.Fatalf("ler captura: %v", err)
	}

	p := dominio.Pedido{
		Montante:  dominio.DinheiroDeInteiro(200000),
		PrazoAnos: 30,
		TipoTaxa:  dominio.TaxaVariavel,
		Produtos:  []string{}, // sem vendas
	}

	o, err := lerResposta(string(html), p)
	if err != nil {
		t.Fatalf("lerResposta: %v", err)
	}

	// Deve usar a variante base
	if o.TAEG == nil {
		t.Fatal("TAEG: nil")
	}
	taeg := o.TAEG.Decimal()
	esperado := 5.1
	if diff := sub(taeg.InexactFloat64(), esperado); diff.Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
		t.Errorf("TAEG = %s, queria %f", taeg, esperado)
	}

	if o.Prestacao == nil {
		t.Fatal("Prestacao: nil")
	}
	prestacao := o.Prestacao.Decimal()
	esperadoPrestacao := 983.53
	if diff := sub(prestacao.InexactFloat64(), esperadoPrestacao); diff.Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
		t.Errorf("Prestacao = %s, queria %f", prestacao, esperadoPrestacao)
	}

	// Deve ter anotação sem vendas
	notas := o.Notas()
	if len(notas) == 0 {
		t.Error("Notas: vazia, queria anotação sem vendas")
	}
}

// TestParsePT testa o parser de números em formato português.
func TestParsePT(t *testing.T) {
	casos := []struct {
		entrada  string
		esperado float64
	}{
		{"897,75", 897.75},
		{"4,4", 4.4},
		{"200.000", 200000},
		{"1.161,63", 1161.63},
		{"4,4%", 4.4},
		{"5,1%", 5.1},
	}

	for _, c := range casos {
		t.Run(c.entrada, func(t *testing.T) {
			v, err := parsePT(c.entrada)
			if err != nil {
				t.Fatalf("parsePT(%q): %v", c.entrada, err)
			}
			esperado := decimal.NewFromFloat(c.esperado)
			resultado := decimal.NewFromFloat(v)
			if !esperado.Equal(resultado) {
				t.Errorf("parsePT(%q) = %s, queria %s", c.entrada, resultado, esperado)
			}
		})
	}
}

func sub(a, b float64) decimal.Decimal {
	return decimal.NewFromFloat(a).Sub(decimal.NewFromFloat(b))
}
