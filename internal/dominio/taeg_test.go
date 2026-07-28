package dominio_test

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Sem encargos, a TAEG é a anualização efectiva da TAN — e o solver tem de a
// reencontrar.
//
// ⚠️ É o teste âncora, e é forte por construção: os fluxos SÃO a anuidade
// francesa à taxa i = 6/1200 = 0,005, portanto a taxa que os iguala ao capital é
// exactamente i, e a TAEG é (1,005)¹² − 1 = 0,0616778… = 6,168 %. O número
// confere-se à mão sem este código. Se a bissecção, o desconto ou a conversão
// mensal→anual estiverem errados, isto falha — e é o único teste do ficheiro que
// não depende de nada nosso.
func TestSemEncargosATAEGEAAnualizacaoEfectivaDaTAN(t *testing.T) {
	t.Parallel()

	capital := dominio.DinheiroDeInteiro(100_000)
	plano, err := dominio.PlanoFrances(capital, []dominio.Trecho{{Meses: 360, Anual: taxa(t, "6")}})
	if err != nil {
		t.Fatalf("PlanoFrances: %v", err)
	}

	taeg, err := dominio.TAEGDeFluxos(capital, plano.Fluxos)
	if err != nil {
		t.Fatalf("TAEGDeFluxos: %v", err)
	}

	// (1,005)¹² − 1 = 6,16778…% → 6,168 % a três casas.
	esperada := taxa(t, "6.168")
	if desvio := taeg.Decimal().Sub(esperada.Decimal()).Abs(); desvio.GreaterThan(decimal.New(1, -3)) {
		t.Errorf("a TAEG é %s %% e a anualização de 6 %% nominal dá %s %% (desvio de %s p.p.)",
			taeg, esperada, desvio)
	}
	t.Logf("TAN 6 %% nominal → TAEG %s %% efectiva", taeg)
}

func TestATAEGEMaiorDoQueATANEMenorDoQueOAbsurdo(t *testing.T) {
	t.Parallel()

	// A TAEG efectiva excede sempre a TAN nominal, por capitalização, mesmo sem
	// um cêntimo de encargos. É a relação que impede o erro mais fácil deste
	// ficheiro: devolver a taxa mensal × 12 e chamar-lhe TAEG.
	for _, tan := range []string{"0.5", "3.25", "6", "12"} {
		capital := dominio.DinheiroDeInteiro(200_000)
		plano, err := dominio.PlanoFrances(capital, []dominio.Trecho{{Meses: 300, Anual: taxa(t, tan)}})
		if err != nil {
			t.Fatalf("TAN %s: PlanoFrances: %v", tan, err)
		}
		taeg, err := dominio.TAEGDeFluxos(capital, plano.Fluxos)
		if err != nil {
			t.Fatalf("TAN %s: TAEGDeFluxos: %v", tan, err)
		}
		if taeg.Cmp(taxa(t, tan)) <= 0 {
			t.Errorf("TAN %s %%: a TAEG deu %s %%, e devia ser maior", tan, taeg)
		}
		t.Logf("TAN %s %% → TAEG %s %%", tan, taeg)
	}
}

func TestOsEncargosSubemATAEGEATANFicaQuieta(t *testing.T) {
	t.Parallel()

	// ⚠️ É a propriedade de que todo o modelo de encargos depende: a distância
	// entre a TAEG e a TAN é o que os encargos custam. Se um encargo maior não
	// desse uma TAEG maior, o ajuste do encargos.go não teria por onde pegar.
	capital := dominio.DinheiroDeInteiro(320_000)
	plano, err := dominio.PlanoFrances(capital, []dominio.Trecho{{Meses: 360, Anual: taxa(t, "3.25")}})
	if err != nil {
		t.Fatalf("PlanoFrances: %v", err)
	}

	semNada, err := dominio.TAEGDeFluxos(capital, plano.Fluxos)
	if err != nil {
		t.Fatalf("TAEGDeFluxos: %v", err)
	}

	// Um encargo antecipado desconta-se do capital recebido (forma (b) do Anexo I).
	comAntecipado, err := dominio.TAEGDeFluxos(dinheiro(t, "317000"), plano.Fluxos)
	if err != nil {
		t.Fatalf("TAEGDeFluxos com antecipado: %v", err)
	}
	if comAntecipado.Cmp(semNada) <= 0 {
		t.Errorf("com 3 000 € de encargo antecipado a TAEG deu %s %% e sem nada deu %s %%",
			comAntecipado, semNada)
	}

	// Um encargo recorrente soma-se a cada prestação.
	comRecorrente := make([]dominio.Dinheiro, len(plano.Fluxos))
	for i, f := range plano.Fluxos {
		comRecorrente[i] = dominio.DinheiroDeDecimal(f.Decimal().Add(decimal.NewFromInt(25)))
	}
	comMensal, err := dominio.TAEGDeFluxos(capital, comRecorrente)
	if err != nil {
		t.Fatalf("TAEGDeFluxos com recorrente: %v", err)
	}
	if comMensal.Cmp(semNada) <= 0 {
		t.Errorf("com 25 €/mês a TAEG deu %s %% e sem nada deu %s %%", comMensal, semNada)
	}

	t.Logf("sem encargos %s %% · +3 000 € à cabeça %s %% · +25 €/mês %s %%",
		semNada, comAntecipado, comMensal)
}

func TestOMesmoEncargoPesaMaisNumPrazoCurto(t *testing.T) {
	t.Parallel()

	// ⚠️ É ESTA a assimetria que a 7.ª família da grelha existe para medir, e é o
	// que torna os dois tipos de encargo separáveis. Um encargo ANTECIPADO
	// dilui-se por mais anos: os mesmos 3 000 € afastam muito a TAEG da TAN em 10
	// anos e pouco em 40. Um encargo RECORRENTE não se dilui — renova-se todos os
	// meses. Se estas duas linhas dessem a mesma coisa, nenhum ajuste distinguiria
	// os dois, e a repartição seria assunção nossa.
	const capital = 320_000
	antecipado := dinheiro(t, "317000") // 3 000 € de encargo à cabeça

	gapAntecipado := map[int]decimal.Decimal{}
	gapRecorrente := map[int]decimal.Decimal{}

	for _, anos := range []int{10, 40} {
		plano, err := dominio.PlanoFrances(
			dominio.DinheiroDeInteiro(capital), []dominio.Trecho{{Meses: anos * 12, Anual: taxa(t, "3.25")}})
		if err != nil {
			t.Fatalf("%d anos: PlanoFrances: %v", anos, err)
		}
		base, err := dominio.TAEGDeFluxos(dominio.DinheiroDeInteiro(capital), plano.Fluxos)
		if err != nil {
			t.Fatalf("%d anos: TAEGDeFluxos: %v", anos, err)
		}

		comA, err := dominio.TAEGDeFluxos(antecipado, plano.Fluxos)
		if err != nil {
			t.Fatalf("%d anos: TAEGDeFluxos antecipado: %v", anos, err)
		}
		gapAntecipado[anos] = comA.Decimal().Sub(base.Decimal())

		comR := make([]dominio.Dinheiro, len(plano.Fluxos))
		for i, f := range plano.Fluxos {
			comR[i] = dominio.DinheiroDeDecimal(f.Decimal().Add(decimal.NewFromInt(25)))
		}
		comRec, err := dominio.TAEGDeFluxos(dominio.DinheiroDeInteiro(capital), comR)
		if err != nil {
			t.Fatalf("%d anos: TAEGDeFluxos recorrente: %v", anos, err)
		}
		gapRecorrente[anos] = comRec.Decimal().Sub(base.Decimal())
	}

	// O antecipado dilui-se: pesa MENOS a 40 anos do que a 10.
	if gapAntecipado[40].GreaterThanOrEqual(gapAntecipado[10]) {
		t.Errorf("o encargo antecipado devia diluir-se com o prazo: 10 anos deu +%s p.p. e 40 anos +%s p.p.",
			gapAntecipado[10], gapAntecipado[40])
	}

	// O recorrente não se dilui da mesma maneira — e é a razão de os dois serem
	// identificáveis a partir de dois prazos.
	razaoA := gapAntecipado[10].Div(gapAntecipado[40])
	razaoR := gapRecorrente[10].Div(gapRecorrente[40])
	if razaoA.Cmp(razaoR) <= 0 {
		t.Errorf("as duas naturezas não se distinguem pelo prazo: razão antecipado %s, recorrente %s",
			razaoA.Round(3), razaoR.Round(3))
	}

	t.Logf("3 000 € à cabeça: +%s p.p. a 10 anos, +%s p.p. a 40 (razão %s)",
		gapAntecipado[10].Round(3), gapAntecipado[40].Round(3), razaoA.Round(2))
	t.Logf("25 €/mês:        +%s p.p. a 10 anos, +%s p.p. a 40 (razão %s)",
		gapRecorrente[10].Round(3), gapRecorrente[40].Round(3), razaoR.Round(2))
}

func TestOMTICSomaTudoOQueSePaga(t *testing.T) {
	t.Parallel()

	// ⚠️ O MTIC é uma soma e não uma equação, e o encargo antecipado SOMA-SE em
	// vez de se descontar. Dar-lhe o líquido em vez do antecipado dava um MTIC
	// menor do que a verdade — engano que passa despercebido porque o número
	// continua grande.
	fluxos := []dominio.Dinheiro{dinheiro(t, "100"), dinheiro(t, "200"), dinheiro(t, "300")}
	got := dominio.MTICDeFluxos(dinheiro(t, "50"), fluxos)
	if esperado := dinheiro(t, "650"); !got.Equal(esperado) {
		t.Errorf("MTIC deu %s e devia dar %s", got, esperado)
	}
}

func TestOSolverRecusaOQueNaoTemRaiz(t *testing.T) {
	t.Parallel()

	// ⚠️ Devolve erro em vez do extremo do intervalo. Uma TAEG que é o limite da
	// procura é um número errado com ar de certo — §7.4 outra vez.
	for _, caso := range []struct {
		nome     string
		liquido  string
		fluxos   []string
		sentinel error
	}{
		{"sem fluxos", "1000", nil, dominio.ErrSemFluxos},
		{"liquido zero", "0", []string{"100"}, dominio.ErrLiquidoNaoPositivo},
		{
			// Paga-se menos do que se recebeu: não há taxa não-negativa.
			"pagamentos abaixo do capital", "1000",
			[]string{"100", "100"},
			dominio.ErrTAEGForaDoIntervalo,
		},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			fluxos := make([]dominio.Dinheiro, 0, len(caso.fluxos))
			for _, f := range caso.fluxos {
				fluxos = append(fluxos, dinheiro(t, f))
			}
			_, err := dominio.TAEGDeFluxos(dinheiro(t, caso.liquido), fluxos)
			if !errors.Is(err, caso.sentinel) {
				t.Errorf("erro %v, esperava %v", err, caso.sentinel)
			}
		})
	}
}
