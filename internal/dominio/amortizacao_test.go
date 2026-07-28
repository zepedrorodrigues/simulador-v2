package dominio_test

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A prestação de 100 000 € a 6 % em 360 meses é 599,55 €.
//
// ⚠️ É o valor de manual, e está aqui de propósito em vez de um valor que este
// código tenha produzido. Um teste que compare a função consigo mesma passa com
// a fórmula errada; este falha. Confere-se à mão: i = 0,005, (1,005)^360 ≈
// 6,022575, p = 100000 × 0,005 / (1 − 1/6,022575) = 500 / 0,833958 = 599,55.
func TestAPrestacaoBateComOValorDeManual(t *testing.T) {
	t.Parallel()

	p, err := dominio.PrestacaoFrancesa(dominio.DinheiroDeInteiro(100_000), taxa(t, "6"), 360)
	if err != nil {
		t.Fatalf("PrestacaoFrancesa: %v", err)
	}
	if esperado := dinheiro(t, "599.55"); !p.Equal(esperado) {
		t.Errorf("a prestação é %s e o manual diz %s", p, esperado)
	}
}

func TestATaxaZeroRepartOCapitalPelosMeses(t *testing.T) {
	t.Parallel()

	// ⚠️ Não é um caso exótico a mais: é o caso em que a fórmula geral divide
	// por zero, porque 1−(1+0)^−n é exactamente 0. Sem o desvio explícito, isto
	// era um pânico do decimal a meio de um pedido de um cliente.
	p, err := dominio.PrestacaoFrancesa(dominio.DinheiroDeInteiro(120_000), taxa(t, "0"), 240)
	if err != nil {
		t.Fatalf("PrestacaoFrancesa: %v", err)
	}
	if esperado := dinheiro(t, "500"); !p.Equal(esperado) {
		t.Errorf("a prestação a 0 %% é %s e devia ser %s", p, esperado)
	}
}

func TestUmPlanoFechaSempreExactamenteAZero(t *testing.T) {
	t.Parallel()

	// ⚠️ É a propriedade que o arredondamento ao cêntimo põe em risco: ao longo
	// de 480 meses, meio cêntimo por mês são dois euros e meio de saldo que
	// ninguém explicaria. O PlanoFrances fecha com «o que resta mais o juro do
	// mês» exactamente para isto, e aqui afirma-se sobre muitas combinações em
	// vez de uma.
	for _, capital := range []int64{50_000, 320_000, 1_000_000} {
		for _, anos := range []int{5, 15, 30, 40} {
			for _, tx := range []string{"0", "0.5", "3.25", "7.125", "12"} {
				plano, err := dominio.PlanoFrances(
					dominio.DinheiroDeInteiro(capital),
					[]dominio.Trecho{{Meses: anos * 12, Anual: taxa(t, tx)}},
				)
				if err != nil {
					t.Fatalf("%d € / %d anos / %s %%: %v", capital, anos, tx, err)
				}
				if got := plano.Meses(); got != anos*12 {
					t.Errorf("%d € / %d anos / %s %%: %d fluxos, esperava %d",
						capital, anos, tx, got, anos*12)
				}
				if err := dominio.ValidarFases(plano.Fases, anos); err != nil {
					t.Errorf("%d € / %d anos / %s %%: ValidarFases: %v", capital, anos, tx, err)
				}
			}
		}
	}
}

func TestNumaMistaAPrestacaoDaFaseFixaSaiDoPrazoTodoENaoDaFase(t *testing.T) {
	t.Parallel()

	// ⚠️ É a armadilha que o E2E da CGD nomeia: «numa mista, a prestação da fase
	// fixa calcula-se sobre o prazo TODO e não sobre a duração da fase — é o que
	// faz dela uma fase de um plano e não um empréstimo de cinco anos».
	// Calculá-la sobre os 60 meses do troço dava uma prestação várias vezes
	// maior, e plausível à vista.
	const capital = 320_000
	const anos = 30

	plano, err := dominio.PlanoFrances(dominio.DinheiroDeInteiro(capital), []dominio.Trecho{
		{Meses: 5 * 12, Anual: taxa(t, "2.9")},
		{Meses: 25 * 12, Anual: taxa(t, "3.9")},
	})
	if err != nil {
		t.Fatalf("PlanoFrances: %v", err)
	}
	if len(plano.Fases) != 2 {
		t.Fatalf("%d fases, esperava 2", len(plano.Fases))
	}

	// A fase fixa amortizaria o capital todo em 30 anos àquela taxa.
	sobreOPrazoTodo, err := dominio.PrestacaoFrancesa(dominio.DinheiroDeInteiro(capital), taxa(t, "2.9"), anos*12)
	if err != nil {
		t.Fatalf("PrestacaoFrancesa: %v", err)
	}
	if !plano.Fases[0].Prestacao.Equal(sobreOPrazoTodo) {
		t.Errorf("a fase fixa dá %s e sobre o prazo todo dá %s",
			plano.Fases[0].Prestacao, sobreOPrazoTodo)
	}

	// E sobre os 60 meses do troço daria muito mais — é o erro que isto exclui.
	sobreOTroco, err := dominio.PrestacaoFrancesa(dominio.DinheiroDeInteiro(capital), taxa(t, "2.9"), 5*12)
	if err != nil {
		t.Fatalf("PrestacaoFrancesa: %v", err)
	}
	if plano.Fases[0].Prestacao.Cmp(sobreOTroco) >= 0 {
		t.Errorf("a fase fixa (%s) não devia ser tão alta como a do troço isolado (%s)",
			plano.Fases[0].Prestacao, sobreOTroco)
	}
	t.Logf("fase fixa %s; sobre o troço isolado seria %s", plano.Fases[0].Prestacao, sobreOTroco)
}

func TestOJuroTotalEASomaDosFluxosMenosOCapital(t *testing.T) {
	t.Parallel()

	// O MTIC é construído sobre esta soma, por isso vale afirmá-la aqui: se os
	// fluxos não somarem capital + juro, tudo o que se derive deles está errado.
	const capital = 200_000
	plano, err := dominio.PlanoFrances(dominio.DinheiroDeInteiro(capital), []dominio.Trecho{
		{Meses: 360, Anual: taxa(t, "4")},
	})
	if err != nil {
		t.Fatalf("PlanoFrances: %v", err)
	}

	soma := decimal.Zero
	for _, f := range plano.Fluxos {
		soma = soma.Add(f.Decimal())
	}
	if soma.Cmp(decimal.NewFromInt(capital)) <= 0 {
		t.Errorf("a soma dos fluxos (%s) não excede o capital (%d)", soma.StringFixed(2), capital)
	}
	t.Logf("200 000 € a 4 %% em 30 anos: paga-se %s €, dos quais %s € de juro",
		soma.StringFixed(2), soma.Sub(decimal.NewFromInt(capital)).StringFixed(2))
}

func TestOSaldoVaiDoCapitalAZero(t *testing.T) {
	t.Parallel()

	capital := dominio.DinheiroDeInteiro(150_000)
	const meses = 240

	inicio, err := dominio.SaldoApos(capital, taxa(t, "3.5"), meses, 0)
	if err != nil {
		t.Fatalf("SaldoApos(0): %v", err)
	}
	if !inicio.Equal(capital) {
		t.Errorf("sem prestações pagas o saldo é %s e devia ser o capital %s", inicio, capital)
	}

	fim, err := dominio.SaldoApos(capital, taxa(t, "3.5"), meses, meses)
	if err != nil {
		t.Fatalf("SaldoApos(%d): %v", meses, err)
	}
	// ⚠️ Tolerância de um euro, e não zero: esta é a fórmula fechada com a
	// prestação já arredondada aos cêntimos, e 240 meses desse arredondamento
	// movem o saldo. Quem quer o fecho exacto usa o PlanoFrances, que caminha
	// mês a mês — é ele o autoritativo, e é a mesma distinção que o E2E da CGD
	// já registou ao dar tolerância maior à segunda fase.
	if fim.Decimal().Abs().Cmp(decimal.NewFromInt(1)) > 0 {
		t.Errorf("ao fim do prazo o saldo é %s e devia ser perto de zero", fim)
	}
	t.Logf("saldo final pela fórmula fechada: %s €", fim)
}

func TestOMotorRecusaOQueNaoEAmortizavel(t *testing.T) {
	t.Parallel()

	for _, caso := range []struct {
		nome     string
		capital  int64
		taxa     string
		meses    int
		sentinel error
	}{
		{"capital zero", 0, "3", 360, dominio.ErrCapitalNaoAmortizavel},
		{"prazo zero", 100_000, "3", 0, dominio.ErrPrazoNaoAmortizavel},
		{"prazo negativo", 100_000, "3", -12, dominio.ErrPrazoNaoAmortizavel},
		{"taxa negativa", 100_000, "-1", 360, dominio.ErrTaxaNegativa},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()
			_, err := dominio.PrestacaoFrancesa(
				dominio.DinheiroDeInteiro(caso.capital), taxa(t, caso.taxa), caso.meses)
			if !errors.Is(err, caso.sentinel) {
				t.Errorf("erro %v, esperava %v", err, caso.sentinel)
			}
		})
	}
}

// ⚠️ Os utilitários `taxa` e `dinheiro` vivem no dinheiro_test.go, que é o
// ficheiro de quem os tipos são. Não se repetem aqui.
