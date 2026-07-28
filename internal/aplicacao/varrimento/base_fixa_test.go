package varrimento_test

import (
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A base da taxa fixa, contra a forma medida da CGD a 2026-07-28.
//
// ⚠️ Os números não são inventados: são os do produto cartesiano, e é por isso
// que este ficheiro os repete em vez de usar valores redondos. Um teste com
// 1,00 e 2,00 passaria na mesma e não diria nada sobre o banco.

// escalaMedidaDaCGD é a escala que a descoberta devolveu na corrida do
// cartesiano: quatro degraus, todos resolvidos.
func escalaMedidaDaCGD(t *testing.T) dominio.EscalaDeLTV {
	t.Helper()
	e, err := dominio.NovaEscalaDeLTV([]dominio.DegrauLTV{
		{De: racioDeTeste(t, "0.30"), Ate: racioDeTeste(t, "0.333125"), Spread: taxaDeTeste(t, "1.950")},
		{De: racioDeTeste(t, "0.333125"), Ate: racioDeTeste(t, "0.6665625"), Spread: taxaDeTeste(t, "2.000")},
		{De: racioDeTeste(t, "0.6665625"), Ate: racioDeTeste(t, "0.6775"), Spread: taxaDeTeste(t, "2.050")},
		{De: racioDeTeste(t, "0.6775"), Ate: racioDeTeste(t, "0.90"), Spread: taxaDeTeste(t, "1.350")},
	})
	if err != nil {
		t.Fatalf("a escala medida da CGD não construiu: %v", err)
	}
	return e
}

// TestABaseEAMesmaEmTodosOsDegraus é a afirmação central: a mesma taxa fixa,
// medida em degraus diferentes, dá TAN diferentes e **base igual**.
//
// Os pares TAN/LTV são os do cartesiano, para a fixa a 10 anos.
func TestABaseEAMesmaEmTodosOsDegraus(t *testing.T) {
	escala := escalaMedidaDaCGD(t)

	// LTV, TAN medida nesse LTV. O imóvel é 400 000 € e o montante varia.
	casos := []struct {
		montante string
		tan      string
	}{
		{"120000", "5.450"}, // LTV 0,30   → spread 1,950
		{"134000", "5.500"}, // LTV 0,335  → spread 2,000
		{"268000", "5.550"}, // LTV 0,67   → spread 2,050
		{"272000", "4.850"}, // LTV 0,68   → spread 1,350
	}

	for _, c := range casos {
		o := observacaoDeTaxaFixa(t, c.montante, c.tan)

		base, ok := varrimento.BaseDaTaxaFixa(o, escala)
		if !ok {
			t.Fatalf("montante %s: não se calculou base nenhuma", c.montante)
		}
		if got := base.Decimal().StringFixed(3); got != "3.500" {
			t.Errorf("montante %s (TAN %s): base %s, e a fixa a 10 anos da CGD tem base 3.500 em todos os degraus",
				c.montante, c.tan, got)
		}
	}
}

func TestNaoHaBaseOndeNaoFazSentidoCalcularUma(t *testing.T) {
	escala := escalaMedidaDaCGD(t)

	t.Run("taxa variável", func(t *testing.T) {
		// ⚠️ Na variável o spread é publicado à parte e a identidade fecha com a
		// Euribor. Subtrair o spread à TAN aqui seria subtrair duas vezes o
		// mesmo número, e o resultado não descrevia preço nenhum.
		o := observacaoDeTaxaFixa(t, "120000", "5.450")
		o.Ponto.Pedido.TipoTaxa = dominio.TaxaVariavel
		if _, ok := varrimento.BaseDaTaxaFixa(o, escala); ok {
			t.Error("calculou-se base para uma taxa variável")
		}
	})

	t.Run("observação de falha", func(t *testing.T) {
		o := observacaoDeTaxaFixa(t, "120000", "5.450")
		o.Oferta = dominio.Falhar("cgd", "CGD", &dominio.ErroOferta{
			Codigo: dominio.ErroBancoIndisponivel, Mensagem: "em baixo",
		})
		if _, ok := varrimento.BaseDaTaxaFixa(o, escala); ok {
			t.Error("calculou-se base a partir de uma falha")
		}
	})

	t.Run("LTV fora da escala medida", func(t *testing.T) {
		// A escala da CGD acaba nos 90 %: é onde o banco deixa de financiar na
		// habitação própria. Inventar o degrau mais próximo era dar a este
		// cliente o preço de outro.
		o := observacaoDeTaxaFixa(t, "380000", "5.450") // LTV 0,95
		if _, ok := varrimento.BaseDaTaxaFixa(o, escala); ok {
			t.Error("calculou-se base para um LTV fora da escala")
		}
	})

	t.Run("sem escala nenhuma", func(t *testing.T) {
		// O banco cuja escala não se mediu neste varrimento. A linha grava-se
		// como dado bruto; o que ela não pode é responder noutro LTV.
		o := observacaoDeTaxaFixa(t, "120000", "5.450")
		if _, ok := varrimento.BaseDaTaxaFixa(o, dominio.EscalaDeLTV{}); ok {
			t.Error("calculou-se base sem escala nenhuma medida")
		}
	})
}

// TestAsEscalasSaemDoProprioLote afirma o que torna a base gravável: os degraus
// que vão no lote reconstroem a escala de cada banco.
func TestAsEscalasSaemDoProprioLote(t *testing.T) {
	var lote []varrimento.Observacao

	// Os quatro degraus da CGD, como o varrimento os grava.
	for _, d := range escalaMedidaDaCGD(t).Degraus() {
		intervalo := d
		o := observacaoDeTaxaFixa(t, "120000", "5.450")
		o.Oferta.BancoID = "cgd"
		o.Degrau = &intervalo
		lote = append(lote, o)
	}
	// E um ponto de outro banco, sem degraus nenhuns.
	outro := observacaoDeTaxaFixa(t, "120000", "4.100")
	outro.Oferta.BancoID = "novobanco"
	lote = append(lote, outro)

	escalas := varrimento.EscalasPorBanco(lote)

	if _, temCGD := escalas["cgd"]; !temCGD {
		t.Fatal("a escala da CGD não se reconstruiu a partir das linhas de degrau do lote")
	}
	// ⚠️ Um banco sem degraus não aparece, e não aparece com uma escala vazia:
	// «deste banco não se sabe» é diferente de «este banco não tem preço».
	if _, temNovoBanco := escalas["novobanco"]; temNovoBanco {
		t.Error("um banco sem linhas de degrau ganhou escala")
	}

	base, ok := varrimento.BaseDaTaxaFixa(observacaoDeTaxaFixa(t, "268000", "5.550"), escalas["cgd"])
	if !ok || base.Decimal().StringFixed(3) != "3.500" {
		t.Errorf("a escala reconstruída do lote não dá a mesma base: %v (ok=%t)", base, ok)
	}
}

// TestUmaEscalaQueNaoValidaNaoViraMeiaEscala: degraus com um buraco entre eles
// não descrevem o preço de nada, e servir o que sobra seria pior do que não
// servir.
func TestUmaEscalaQueNaoValidaNaoViraMeiaEscala(t *testing.T) {
	buraco := []dominio.DegrauLTV{
		{De: racioDeTeste(t, "0.30"), Ate: racioDeTeste(t, "0.50"), Spread: taxaDeTeste(t, "1.950")},
		// Salta de 0,50 para 0,60: não é contíguo.
		{De: racioDeTeste(t, "0.60"), Ate: racioDeTeste(t, "0.90"), Spread: taxaDeTeste(t, "1.350")},
	}

	var lote []varrimento.Observacao
	for _, d := range buraco {
		intervalo := d
		o := observacaoDeTaxaFixa(t, "120000", "5.450")
		o.Degrau = &intervalo
		lote = append(lote, o)
	}

	if escalas := varrimento.EscalasPorBanco(lote); len(escalas) != 0 {
		t.Errorf("uma escala com um buraco entre degraus foi aceite: %+v", escalas)
	}
}

// observacaoDeTaxaFixa monta uma observação de taxa fixa sobre um imóvel de
// 400 000 € — o mesmo da grelha de referência, onde 1 000 € de montante são
// 0,25 p.p. de LTV.
func observacaoDeTaxaFixa(t *testing.T, montante, tan string) varrimento.Observacao {
	t.Helper()

	taxa := taxaDeTeste(t, tan)
	prestacao := dinheiroDeTeste(t, "1000")
	return varrimento.Observacao{
		Ponto: varrimento.Ponto{
			Cenario: "fixa/10/propria",
			Pedido: dominio.Pedido{
				ValorImovel: dominio.DinheiroDeInteiro(400_000),
				Montante:    dinheiroDeTeste(t, montante),
				PrazoAnos:   10,
				TipoTaxa:    dominio.TaxaFixa,
				Finalidade:  dominio.FinalidadePropria,
				Localizacao: dominio.LocalizacaoContinente,
			},
		},
		Oferta: dominio.Oferta{
			BancoID: "cgd", BancoNome: "Caixa Geral de Depósitos",
			TAN:         &taxa,
			Prestacao:   &prestacao,
			CapturadoEm: time.Now(),
		},
	}
}

func racioDeTeste(t *testing.T, s string) dominio.Racio {
	t.Helper()
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		t.Fatalf("rácio de teste inválido %q: %v", s, err)
	}
	return r
}
