package dominio_test

import (
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Os testes da amortização francesa.
//
// ⚠️ Os valores esperados não saem desta implementação: saem da fórmula
// fechada, calculada à parte com 40 dígitos de precisão. Um esperado copiado do
// que o código devolve afirma que o código não mudou, e não que está certo — e
// é esta conta que decide se um banco mudou de forma de calcular (§7.4).

// arredonda compara ao cêntimo, que é a unidade em que um banco publica uma
// prestação.
func verEuros(t *testing.T, o string, obtido dominio.Dinheiro) {
	t.Helper()
	if got := obtido.Decimal().StringFixed(2); got != o {
		t.Errorf("esperava %s €, deu %s €", o, got)
	}
}

func TestPrestacaoFrancesaBateComAFormulaFechada(t *testing.T) {
	// 200 000 € a 3,00 % em 30 anos. O número clássico, e o mesmo que sai da
	// fórmula com 40 dígitos: 843,2080674…
	p, err := dominio.PrestacaoFrancesa(dinheiro(t, "200000"), taxa(t, "3.00"), 360)
	if err != nil {
		t.Fatalf("não calculou: %v", err)
	}
	verEuros(t, "843.21", p)
}

// TestAPrestacaoDaFaseFixaSaiDoPrazoTodoENaoDaDuracaoDaFase é a armadilha da
// mista, e vale um teste próprio porque o número errado é perfeitamente
// plausível: 3 593,74 € é uma prestação que existe — a de um empréstimo de
// cinco anos — e nada nela diz que está errada.
func TestAPrestacaoDaFaseFixaSaiDoPrazoTodoENaoDaDuracaoDaFase(t *testing.T) {
	capital, tan := dinheiro(t, "200000"), taxa(t, "3.00")

	planoInteiro, err := dominio.PrestacaoFrancesa(capital, tan, 360)
	if err != nil {
		t.Fatalf("não calculou o plano inteiro: %v", err)
	}
	soAFase, err := dominio.PrestacaoFrancesa(capital, tan, 60)
	if err != nil {
		t.Fatalf("não calculou a fase: %v", err)
	}

	verEuros(t, "843.21", planoInteiro)
	verEuros(t, "3593.74", soAFase)
}

func TestPrestacaoSemJuroRepartOCapitalPelosMeses(t *testing.T) {
	// Sem este caminho, a fórmula dividia por zero.
	p, err := dominio.PrestacaoFrancesa(dinheiro(t, "120000"), taxa(t, "0"), 240)
	if err != nil {
		t.Fatalf("não calculou: %v", err)
	}
	verEuros(t, "500.00", p)
}

func TestCapitalEmDividaDepoisDaFaseFixa(t *testing.T) {
	// Cinco anos pagos de um plano de trinta, a 3,00 %: é o capital que a fase
	// indexada de uma mista vai amortizar.
	c, err := dominio.CapitalEmDivida(dinheiro(t, "200000"), taxa(t, "3.00"), 360, 60)
	if err != nil {
		t.Fatalf("não calculou: %v", err)
	}
	verEuros(t, "177812.73", c)

	// E a fase seguinte, a 4,5 % sobre o que sobrou, nos 300 meses que faltam.
	p, err := dominio.PrestacaoFrancesa(c, taxa(t, "4.5"), 300)
	if err != nil {
		t.Fatalf("não calculou a fase indexada: %v", err)
	}
	verEuros(t, "988.34", p)
}

func TestCapitalEmDividaNosExtremosDoPlano(t *testing.T) {
	capital, tan := dinheiro(t, "200000"), taxa(t, "3.00")

	inicio, err := dominio.CapitalEmDivida(capital, tan, 360, 0)
	if err != nil {
		t.Fatalf("não calculou o início: %v", err)
	}
	if !inicio.Equal(capital) {
		t.Errorf("sem um mês pago, a dívida é %s e o capital era %s", inicio, capital)
	}

	fim, err := dominio.CapitalEmDivida(capital, tan, 360, 360)
	if err != nil {
		t.Fatalf("não calculou o fim: %v", err)
	}
	if fim.Decimal().StringFixed(2) != "0.00" {
		t.Errorf("com o plano todo pago, ainda sobram %s €", fim.Decimal().StringFixed(2))
	}
}

// TestSemJuroADividaDesceEmLinhaReta afirma o caminho da taxa zero, que a
// fórmula geral não consegue percorrer.
func TestSemJuroADividaDesceEmLinhaReta(t *testing.T) {
	c, err := dominio.CapitalEmDivida(dinheiro(t, "120000"), taxa(t, "0"), 240, 60)
	if err != nil {
		t.Fatalf("não calculou: %v", err)
	}
	verEuros(t, "90000.00", c)
}

// TestAmortizacaoRecusaOQueNaoEUmPlano: cada um destes devolveria um número —
// zero, infinito ou negativo — que se leria como um resíduo enorme, ou seja
// como «o banco mudou». Falham alto, e o erro nomeia o que estava errado.
func TestAmortizacaoRecusaOQueNaoEUmPlano(t *testing.T) {
	casos := []struct {
		nome    string
		capital dominio.Dinheiro
		taxa    dominio.Taxa
		meses   int
		nomeia  string
	}{
		{"plano de zero meses", dinheiro(t, "200000"), taxa(t, "3.00"), 0, "0 meses"},
		{"plano de meses negativos", dinheiro(t, "200000"), taxa(t, "3.00"), -12, "-12 meses"},
		{"capital a zero", dinheiro(t, "0"), taxa(t, "3.00"), 360, "capital"},
		{"capital negativo", dinheiro(t, "-1"), taxa(t, "3.00"), 360, "capital"},
		{"taxa negativa", dinheiro(t, "200000"), taxa(t, "-0.5"), 360, "taxa anual"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := dominio.PrestacaoFrancesa(c.capital, c.taxa, c.meses)
			if err == nil {
				t.Fatal("a prestação foi calculada e não devia")
			}
			if !strings.Contains(err.Error(), c.nomeia) {
				t.Errorf("o erro é %q e não nomeia %q", err, c.nomeia)
			}

			if _, err := dominio.CapitalEmDivida(c.capital, c.taxa, c.meses, 0); err == nil {
				t.Error("o capital em dívida foi calculado e não devia")
			}
		})
	}
}

func TestCapitalEmDividaRecusaMesesPagosForaDoPlano(t *testing.T) {
	capital, tan := dinheiro(t, "200000"), taxa(t, "3.00")

	for _, pagos := range []int{-1, 361} {
		_, err := dominio.CapitalEmDivida(capital, tan, 360, pagos)
		if err == nil {
			t.Fatalf("com %d meses pagos de 360, calculou na mesma", pagos)
		}
		if !strings.Contains(err.Error(), "plano de 360") {
			t.Errorf("o erro é %q e não diz de que plano se fala", err)
		}
	}
}
