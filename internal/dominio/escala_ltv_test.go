package dominio_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// escalaDaCGD é a escala medida a 2026-07-26: imóvel fixo em 400 000 € e
// montante a variar de 1 000 €, para cada passo ser exactamente 0,25 p.p. de
// LTV. Variável, 30 anos, habitação própria.
//
//	33,50 – 66,50 → 2,000
//	66,75 – 67,75 → 2,050
//	68,00 – 92,00 → 1,350
//
// ⚠️ As fronteiras estão em (66,50 ; 66,75] e (67,75 ; 68,00], e é a primeira
// que não contém LTV inteiro nenhum. É por isso que nem uma grelha de 1 p.p.
// representa este banco.
//
// A escala começa nos 33,50 % porque é aí que a medição fica resolvida: sabe-se
// que há outra fronteira em (33,00 ; 33,50] — 1,950 aos 33,00 % — e não se
// refinou entre os dois. Abaixo dos 32 % ficou fora de âmbito da KAN-35 e não
// se varreu. ⚠️ Uma escala afirma o que se mediu, e não mais do que isso.
func escalaDaCGD(t *testing.T) dominio.EscalaDeLTV {
	t.Helper()
	e, err := dominio.NovaEscalaDeLTV([]dominio.DegrauLTV{
		{De: racio(t, "0.335"), Ate: racio(t, "0.6650"), Spread: taxa(t, "2.000")},
		{De: racio(t, "0.6650"), Ate: racio(t, "0.6775"), Spread: taxa(t, "2.050")},
		{De: racio(t, "0.6775"), Ate: racio(t, "0.92"), Spread: taxa(t, "1.350")},
	})
	if err != nil {
		t.Fatalf("a escala medida da CGD não construiu: %v", err)
	}
	return e
}

// TestEscalaDeLTVOsTresPontosDaCGD é o critério de pronto da KAN-35: três LTV
// que o modelo antigo punha todos na banda 70 — e que na CGD têm três spreads
// diferentes — não podem receber o mesmo preço.
//
// ⚠️ Este é o teste que a reversão tem de fazer falhar. Com o BandaLTV de 5 %
// por trás, os três caem na banda 70 e recebem um só spread; a diferença entre
// os extremos é de 0,70 p.p., cerca de 79 €/mês em 200 000 € a 30 anos.
func TestEscalaDeLTVOsTresPontosDaCGD(t *testing.T) {
	e := escalaDaCGD(t)

	casos := []struct {
		ltv    string
		quero  string
		porque string
	}{
		{"0.6650", "2.000", "66,50 % ainda é o degrau de baixo — a fronteira é fechada em cima"},
		{"0.6675", "2.050", "66,75 % já é o patamar isolado, e a fronteira não está em LTV inteiro"},
		{"0.6800", "1.350", "68,00 % é o degrau de cima, 0,70 p.p. abaixo do primeiro"},
	}

	// ⚠️ Recolhe-se o spread SERVIDO, não o esperado: contar os esperados seria
	// uma tautologia — dariam três porque os escrevemos diferentes. O que tem
	// de dar três é o que a escala devolve.
	servidos := make(map[string]string, len(casos))
	for _, c := range casos {
		t.Run("ltv "+c.ltv, func(t *testing.T) {
			s, nota, err := e.SpreadEm(racio(t, c.ltv))
			if err != nil {
				t.Fatalf("SpreadEm(%s) deu erro: %v", c.ltv, err)
			}
			if !s.Equal(taxa(t, c.quero)) {
				t.Errorf("spread em %s = %s, queria %s (%s)", c.ltv, s, c.quero, c.porque)
			}
			if nota != "" {
				t.Errorf("degrau medido e resolvido não leva nota, e veio %q", nota)
			}
		})

		s, _, err := e.SpreadEm(racio(t, c.ltv))
		if err != nil {
			t.Fatalf("SpreadEm(%s) deu erro: %v", c.ltv, err)
		}
		servidos[s.String()] = c.ltv
	}

	// O remate, e é o critério de pronto da issue: não basta cada um estar
	// certo, os três LTV têm de receber três preços distintos. Um modelo de
	// banda fixa de 5 % põe-nos todos na banda 70 e falha aqui — com 0,70 p.p.
	// entre o primeiro e o terceiro.
	if len(servidos) != 3 {
		// ⚠️ A falha tem de nomear o custo, e não só a contagem. «Deu 1 em vez
		// de 3» não diz a quanto sai; por isso procura-se cada par que devia
		// ter preços diferentes e recebeu o mesmo, e reporta-se os p.p. que
		// quem cai do lado errado paga a mais ou a menos.
		for i := range casos {
			for j := i + 1; j < len(casos); j++ {
				si, _, err := e.SpreadEm(racio(t, casos[i].ltv))
				if err != nil {
					t.Fatalf("SpreadEm(%s) deu erro: %v", casos[i].ltv, err)
				}
				sj, _, err := e.SpreadEm(racio(t, casos[j].ltv))
				if err != nil {
					t.Fatalf("SpreadEm(%s) deu erro: %v", casos[j].ltv, err)
				}
				if !si.Equal(sj) {
					continue
				}
				diferenca := taxa(t, casos[i].quero).Sub(taxa(t, casos[j].quero)).Decimal().Abs()
				t.Errorf(
					"LTV %s e %s receberam o mesmo spread (%s), e a CGD pratica %s e %s: "+
						"quem cair do lado errado erra %s p.p.",
					casos[i].ltv, casos[j].ltv, si, casos[i].quero, casos[j].quero, diferenca)
			}
		}
		t.Fatalf(
			"os três LTV da CGD receberam %d spreads distintos (%v), e o banco pratica 3",
			len(servidos), servidos)
	}
}

func TestEscalaDeLTVDegrauPorResolver(t *testing.T) {
	// A fronteira que a medição de 2026-07-26 deixou mesmo por resolver: 1,950
	// aos 33,00 % e 2,000 aos 33,50 %, sem refinamento entre os dois.
	minimo := taxa(t, "1.950")
	e, err := dominio.NovaEscalaDeLTV([]dominio.DegrauLTV{
		{De: racio(t, "0.32"), Ate: racio(t, "0.33"), Spread: taxa(t, "1.950")},
		{
			De: racio(t, "0.33"), Ate: racio(t, "0.335"),
			Spread: taxa(t, "2.000"), SpreadMinimo: &minimo,
		},
		{De: racio(t, "0.335"), Ate: racio(t, "0.6650"), Spread: taxa(t, "2.000")},
	})
	if err != nil {
		t.Fatalf("escala com degrau por resolver não construiu: %v", err)
	}

	t.Run("serve o lado mais caro, e não recusa", func(t *testing.T) {
		// ⚠️ Anexo I, Parte II, alínea (d) da MCD: havendo vários valores
		// possíveis, presume-se o mais alto. Recusar seria menos útil e não
		// mais honesto.
		s, _, err := e.SpreadEm(racio(t, "0.332"))
		if err != nil {
			t.Fatalf("um degrau por resolver responde com nota, não com erro: %v", err)
		}
		if !s.Equal(taxa(t, "2.000")) {
			t.Errorf("spread = %s, queria 2.000 — o mais alto do intervalo", s)
		}
	})

	t.Run("a nota é obrigatória e nomeia os dois números", func(t *testing.T) {
		// Sem esta frase isto é o defeito da §7.4: um número errado com ar de
		// certo. E uma nota que dissesse só «é aproximado» não seria
		// verificável — por isso exige-se que nomeie os dois valores e os
		// limites do intervalo, e não só que exista.
		_, nota, err := e.SpreadEm(racio(t, "0.332"))
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if nota == "" {
			t.Fatal("degrau por resolver serviu um spread sem nota nenhuma")
		}
		for _, pedaco := range []string{"33 %", "33,5 %", "(2 %)", "(1,95 %)", "limite superior"} {
			if !strings.Contains(nota, pedaco) {
				t.Errorf("a nota não fala de %q: %s", pedaco, nota)
			}
		}
	})

	t.Run("os degraus resolvidos à volta continuam mudos", func(t *testing.T) {
		for _, ltv := range []string{"0.325", "0.50"} {
			if _, nota, err := e.SpreadEm(racio(t, ltv)); err != nil || nota != "" {
				t.Errorf("degrau resolvido em %s deu nota %q e erro %v", ltv, nota, err)
			}
		}
	})
}

func TestNovaEscalaDeLTVRecusaOQueNaoRepresentaUmPreco(t *testing.T) {
	maisAlto := taxa(t, "2.500")

	casos := []struct {
		nome    string
		degraus []dominio.DegrauLTV
		naFrase string
	}{
		{
			nome:    "sem degraus",
			degraus: nil,
			naFrase: "sem degraus",
		},
		{
			nome: "com um buraco",
			degraus: []dominio.DegrauLTV{
				{De: racio(t, "0.30"), Ate: racio(t, "0.50"), Spread: taxa(t, "2.000")},
				{De: racio(t, "0.60"), Ate: racio(t, "0.90"), Spread: taxa(t, "1.350")},
			},
			naFrase: "buraco",
		},
		{
			nome: "com uma sobreposição",
			degraus: []dominio.DegrauLTV{
				{De: racio(t, "0.30"), Ate: racio(t, "0.60"), Spread: taxa(t, "2.000")},
				{De: racio(t, "0.50"), Ate: racio(t, "0.90"), Spread: taxa(t, "1.350")},
			},
			naFrase: "sobreposição",
		},
		{
			nome: "fora de ordem",
			degraus: []dominio.DegrauLTV{
				{De: racio(t, "0.60"), Ate: racio(t, "0.90"), Spread: taxa(t, "1.350")},
				{De: racio(t, "0.30"), Ate: racio(t, "0.60"), Spread: taxa(t, "2.000")},
			},
			naFrase: "buraco",
		},
		{
			nome: "degrau que não é intervalo",
			degraus: []dominio.DegrauLTV{
				{De: racio(t, "0.60"), Ate: racio(t, "0.60"), Spread: taxa(t, "2.000")},
			},
			naFrase: "não é um intervalo",
		},
		{
			nome: "por resolver a servir o lado barato",
			degraus: []dominio.DegrauLTV{
				// ⚠️ O caso que esta issue existe para impedir: um intervalo
				// por resolver cujo spread servido é o mais baixo dos dois.
				{
					De: racio(t, "0.30"), Ate: racio(t, "0.60"),
					Spread: taxa(t, "2.000"), SpreadMinimo: &maisAlto,
				},
			},
			naFrase: "não é o mais alto",
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := dominio.NovaEscalaDeLTV(c.degraus)
			if err == nil {
				t.Fatal("construiu uma escala que não representa um preço")
			}
			if !strings.Contains(err.Error(), c.naFrase) {
				t.Errorf("o erro não nomeia %q: %v", c.naFrase, err)
			}
		})
	}
}

func TestEscalaDeLTVForaDoQueOBancoPreca(t *testing.T) {
	e := escalaDaCGD(t)

	// A escala medida vai de 33,50 % a 92 %. Fora disso o banco não preça, e o
	// degrau mais próximo seria o preço de outro cliente — é o
	// ErroProdutoIndisponivel que a §4 nomeia, e não um spread.
	for _, ltv := range []string{"0.20", "0.33", "0.95", "1.00"} {
		t.Run("ltv "+ltv, func(t *testing.T) {
			if _, _, err := e.SpreadEm(racio(t, ltv)); !errors.Is(err, dominio.ErrLTVForaDaEscala) {
				t.Fatalf("erro = %v, queria ErrLTVForaDaEscala", err)
			}
		})
	}

	t.Run("os extremos ainda estão dentro", func(t *testing.T) {
		for _, ltv := range []string{"0.335", "0.92"} {
			if _, _, err := e.SpreadEm(racio(t, ltv)); err != nil {
				t.Errorf("%s é extremo da escala e deu erro: %v", ltv, err)
			}
		}
	})
}

// TestEscalaDeLTVNovoBancoFechaEmCima afirma a convenção da fronteira contra o
// banco onde ela está medida: no Novo Banco, 50,00 % leva 0,75 e 50,25 % leva
// 0,80 — «LTV até 50 %» inclui os 50 %.
//
// ⚠️ É o banco que concorda com o modelo antigo de bandas de 5 %, e está aqui
// de propósito: a escala nova tem de continuar a representá-lo bem. O que a
// KAN-35 rejeita é a banda fixa como constante do domínio, não esta forma de
// preçar — que é a de um dos dois bancos medidos.
func TestEscalaDeLTVNovoBancoFechaEmCima(t *testing.T) {
	e, err := dominio.NovaEscalaDeLTV([]dominio.DegrauLTV{
		{De: racio(t, "0.30"), Ate: racio(t, "0.50"), Spread: taxa(t, "0.75")},
		{De: racio(t, "0.50"), Ate: racio(t, "0.80"), Spread: taxa(t, "0.80")},
		{De: racio(t, "0.80"), Ate: racio(t, "0.90"), Spread: taxa(t, "0.95")},
	})
	if err != nil {
		t.Fatalf("a escala medida do Novo Banco não construiu: %v", err)
	}

	casos := []struct{ ltv, quero string }{
		{"0.50", "0.75"},   // a fronteira pertence ao degrau de baixo
		{"0.5025", "0.80"}, // e um quarto de ponto acima já é o seguinte
		{"0.80", "0.80"},
		{"0.8025", "0.95"},
	}
	for _, c := range casos {
		t.Run("ltv "+c.ltv, func(t *testing.T) {
			s, _, err := e.SpreadEm(racio(t, c.ltv))
			if err != nil {
				t.Fatalf("SpreadEm(%s) deu erro: %v", c.ltv, err)
			}
			if !s.Equal(taxa(t, c.quero)) {
				t.Errorf("spread em %s = %s, queria %s", c.ltv, s, c.quero)
			}
		})
	}
}
