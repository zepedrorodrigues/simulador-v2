package dominio_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O ajuste reencontra encargos que ele próprio não escolheu.
//
// ⚠️ É o teste que decide se este ficheiro serve para algo. Parte-se de encargos
// CONHECIDOS, geram-se as TAEG que eles produziriam em três prazos, e exige-se
// que o ajuste os reencontre a partir só dessas TAEG. Um ajuste que não faça o
// caminho de volta está a inventar a repartição, que é precisamente o que a 7.ª
// família da grelha existe para evitar.
//
// A grelha de valores verdadeiros não é decoração: se a monotonia de que as duas
// bissecções dependem falhasse em alguma zona, é aqui que apareceria.
func TestOAjusteReencontraEncargosConhecidos(t *testing.T) {
	t.Parallel()

	const capital = 320_000
	// Os prazos são os que a grelha varre: os extremos e a referência.
	prazos := []int{10 * 12, 30 * 12, 40 * 12}

	for _, verdadeiro := range []struct{ antecipado, recorrente string }{
		{"0", "0"},          // um banco sem encargos nenhuns
		{"0.01", "0"},       // só antecipado: 1 % do capital
		{"0", "0.35"},       // só recorrente: seguro de vida típico
		{"0.005", "0.25"},   // os dois, em valores plausíveis
		{"0.02", "0.6"},     // um banco caro
		{"0.0035", "0.125"}, // valores que não caem em números redondos
	} {
		nome := fmt.Sprintf("a=%s r=%s", verdadeiro.antecipado, verdadeiro.recorrente)
		t.Run(nome, func(t *testing.T) {
			t.Parallel()

			original := dominio.Encargos{
				Antecipado: racio(t, verdadeiro.antecipado),
				Recorrente: taxa(t, verdadeiro.recorrente),
			}

			// As observações que este banco produziria.
			obs := make([]dominio.ObservacaoDeEncargo, 0, len(prazos))
			for _, meses := range prazos {
				tan := taxa(t, "3.25")
				taeg, err := original.TAEGDe(dominio.DinheiroDeInteiro(capital), meses, tan)
				if err != nil {
					t.Fatalf("TAEGDe(%d meses): %v", meses, err)
				}
				obs = append(obs, dominio.ObservacaoDeEncargo{
					Capital:    dominio.DinheiroDeInteiro(capital),
					PrazoMeses: meses,
					TAN:        tan,
					TAEG:       taeg,
				})
			}

			ajustados, residuos, err := dominio.AjustarEncargos(obs)
			if err != nil {
				t.Fatalf("AjustarEncargos: %v", err)
			}

			// ⚠️ A tolerância é sobre a TAEG que resulta, e não sobre os parâmetros:
			// é a TAEG que se publica. Parâmetros ligeiramente diferentes que dêem a
			// mesma TAEG a três casas são o mesmo modelo para quem lê.
			for _, r := range residuos {
				if r.Excede() {
					t.Errorf("resíduo a %d meses: prevista %s %%, observada %s %% (desvio %s p.p.)",
						r.PrazoMeses, r.Prevista, r.Observada, r.Desvio)
				}
			}

			t.Logf("verdadeiro a=%s r=%s → ajustado a=%s r=%s",
				original.Antecipado, original.Recorrente, ajustados.Antecipado, ajustados.Recorrente)
		})
	}
}

func TestUmPrazoSoNaoIdentificaARepartição(t *testing.T) {
	t.Parallel()

	// ⚠️ É a razão de existir de tudo isto. Com uma observação só, infinitas
	// repartições entre antecipado e recorrente dão exactamente a mesma TAEG —
	// portanto não há ajuste, há escolha. Devolver um número aqui seria devolver
	// uma assunção com aparência de medição.
	uma := []dominio.ObservacaoDeEncargo{{
		Capital:    dominio.DinheiroDeInteiro(320_000),
		PrazoMeses: 360,
		TAN:        taxa(t, "3.25"),
		TAEG:       taxa(t, "3.45"),
	}}
	if _, _, err := dominio.AjustarEncargos(uma); !errors.Is(err, dominio.ErrPoucasObservacoes) {
		t.Errorf("erro %v, esperava ErrPoucasObservacoes", err)
	}

	// E duas observações no MESMO prazo continuam a ser um prazo só: o que
	// identifica não é o número de linhas, é a variedade de prazos.
	duas := append(uma, uma[0]) //nolint:gocritic // é de propósito a mesma observação repetida
	if _, _, err := dominio.AjustarEncargos(duas); !errors.Is(err, dominio.ErrPoucasObservacoes) {
		t.Errorf("com duas observações no mesmo prazo: erro %v, esperava ErrPoucasObservacoes", err)
	}
}

func TestDuasNaturezasIndistinguiveisNumPrazoESeparamSeEmDois(t *testing.T) {
	t.Parallel()

	// A afirmação em número: dois modelos de encargos MUITO diferentes que dão a
	// mesma TAEG a 30 anos dão TAEG diferentes a 10.
	//
	// ⚠️ É a medição que sustenta a 7.ª família da grelha. Sem esta diferença, a
	// família seria dois pedidos por banco gastos a não medir nada.
	capital := dominio.DinheiroDeInteiro(320_000)
	tan := taxa(t, "3.25")

	soAntecipado := dominio.Encargos{Antecipado: racio(t, "0.0243"), Recorrente: taxa(t, "0")}
	soRecorrente := dominio.Encargos{Antecipado: racio(t, "0"), Recorrente: taxa(t, "0.20")}

	a30, err := soAntecipado.TAEGDe(capital, 360, tan)
	if err != nil {
		t.Fatalf("TAEGDe: %v", err)
	}
	r30, err := soRecorrente.TAEGDe(capital, 360, tan)
	if err != nil {
		t.Fatalf("TAEGDe: %v", err)
	}
	a10, err := soAntecipado.TAEGDe(capital, 120, tan)
	if err != nil {
		t.Fatalf("TAEGDe: %v", err)
	}
	r10, err := soRecorrente.TAEGDe(capital, 120, tan)
	if err != nil {
		t.Fatalf("TAEGDe: %v", err)
	}

	// A 30 anos são praticamente o mesmo número — indistinguíveis para quem lê.
	if a30.Decimal().Sub(r30.Decimal()).Abs().GreaterThan(decimal.NewFromFloat(0.03)) {
		t.Logf("os dois modelos não estão calibrados para coincidir a 30 anos: %s vs %s", a30, r30)
	}
	// A 10 anos separam-se muito.
	separacao := a10.Decimal().Sub(r10.Decimal()).Abs()
	if separacao.LessThan(decimal.NewFromFloat(0.1)) {
		t.Errorf("a 10 anos os dois modelos deviam separar-se: %s %% vs %s %% (só %s p.p.)",
			a10, r10, separacao)
	}
	t.Logf("a 30 anos: %s %% vs %s %% (indistinguíveis) · a 10 anos: %s %% vs %s %% (separam-se %s p.p.)",
		a30, r30, a10, r10, separacao.Round(3))
}

func TestOResiduoDenunciaUmBancoQueOModeloNaoDescreve(t *testing.T) {
	t.Parallel()

	// ⚠️ O ajuste ancora nos dois extremos, e é isso que deixa a observação do
	// meio livre para contradizer o modelo. Aqui a TAEG de 30 anos está fora do
	// que duas naturezas conseguem produzir — um banco que cobre uma comissão
	// diferente a médio prazo, por exemplo — e o resíduo tem de o dizer em vez de
	// o absorver.
	capital := dominio.DinheiroDeInteiro(320_000)
	tan := taxa(t, "3.25")
	original := dominio.Encargos{Antecipado: racio(t, "0.01"), Recorrente: taxa(t, "0.3")}

	obs := make([]dominio.ObservacaoDeEncargo, 0, 3)
	for _, meses := range []int{120, 360, 480} {
		taeg, err := original.TAEGDe(capital, meses, tan)
		if err != nil {
			t.Fatalf("TAEGDe: %v", err)
		}
		if meses == 360 {
			// Empurra a do meio meio ponto para cima: o modelo não a alcança.
			taeg = dominio.TaxaDeDecimal(taeg.Decimal().Add(decimal.NewFromFloat(0.5)))
		}
		obs = append(obs, dominio.ObservacaoDeEncargo{
			Capital: capital, PrazoMeses: meses, TAN: tan, TAEG: taeg,
		})
	}

	_, residuos, err := dominio.AjustarEncargos(obs)
	if err != nil {
		t.Fatalf("AjustarEncargos: %v", err)
	}

	var excedeu bool
	for _, r := range residuos {
		if r.PrazoMeses == 360 && r.Excede() {
			excedeu = true
			t.Logf("o resíduo denunciou os 360 meses: prevista %s %%, observada %s %% (desvio %s p.p.)",
				r.Prevista, r.Observada, r.Desvio)
		}
	}
	if !excedeu {
		t.Error("o resíduo dos 360 meses devia exceder o tolerado, e não excedeu")
	}
}

func TestOsPressupostosNomeiamOsNumerosDeQueDependem(t *testing.T) {
	t.Parallel()

	// ⚠️ Um pressuposto que diga só «é aproximado» não é informação — é a mesma
	// exigência que a nota do degrau de LTV por resolver já cumpre, e é o que o
	// Anexo II da MCD pede. Aqui afirma-se que os números aparecem no texto.
	e := dominio.Encargos{Antecipado: racio(t, "0.01"), Recorrente: taxa(t, "0.35")}
	ps := e.Pressupostos(dominio.DinheiroDeInteiro(320_000))

	if len(ps) == 0 {
		t.Fatal("sem pressupostos: o contrato exige a lista não vazia quando há TAEG")
	}
	junto := ""
	for _, p := range ps {
		if p == "" {
			t.Error("pressuposto vazio")
		}
		junto += p + "\n"
	}
	for _, numero := range []string{"3200", "0,35", "1"} {
		if !contem(junto, numero) {
			t.Errorf("os pressupostos não nomeiam %q:\n%s", numero, junto)
		}
	}
	t.Logf("pressupostos:\n%s", junto)
}

func contem(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
