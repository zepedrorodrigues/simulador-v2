package dominio_test

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
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
	// ⚠️ Os números aparecem escritos para uma pessoa desde a KAN-51 — «3 200,00»
	// e não «3200». O que este teste afirma continua a ser o mesmo: que os
	// pressupostos NOMEIAM os números de que dependem, em vez de os esconderem.
	for _, numero := range []string{"3 200,00", "0,35", "1"} {
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

// O pressuposto dos encargos lê-se como uma frase, e não como um despejo de
// números (KAN-51).
//
// ⚠️ Visto no ecrã do detalhe a 2026-08-06, contra dados varridos: saía
// «27911.11 € de encargos iniciais (8,72222222222223 % do montante)». O sítio é
// o que agrava — é o `pressupostos`, que a MCD manda declarar A QUEM LÊ.
//
// A reversão é repor o `String()` e o `percentagem()` sem arredondar: o teste
// falha a mostrar a FRASE inteira, e não «esperava X veio Y» sobre um número
// solto, porque o que está errado é o que a pessoa lê.
func TestOPressupostoDosEncargosLeSeComoUmaFrase(t *testing.T) {
	antecipado, err := decimal.NewFromString("0.0872222222222223")
	if err != nil {
		t.Fatalf("montar o antecipado: %v", err)
	}
	encargos := dominio.Encargos{
		Antecipado: dominio.RacioDeDecimal(antecipado),
		Recorrente: dominio.TaxaDeDecimal(decimal.Zero),
	}
	capital, err := dominio.DinheiroDeTexto("320000")
	if err != nil {
		t.Fatalf("DinheiroDeTexto: %v", err)
	}

	var frase string
	for _, p := range encargos.Pressupostos(capital) {
		if strings.Contains(p, "encargos iniciais") {
			frase = p
		}
	}
	if frase == "" {
		t.Fatal("não há pressuposto nenhum sobre os encargos iniciais")
	}

	for _, feio := range []string{"27911.11", "8,72222222222223"} {
		if strings.Contains(frase, feio) {
			t.Errorf("o pressuposto tem %q lá dentro, e é para uma pessoa ler:\n  %s", feio, frase)
		}
	}
	for _, bonito := range []string{"27 911,11 €", "8,72 %"} {
		if !strings.Contains(frase, bonito) {
			t.Errorf("o pressuposto devia trazer %q e não traz:\n  %s", bonito, frase)
		}
	}
}

// Um ajuste que não fecha sai como ERRO, e não como medição (KAN-52).
//
// ⚠️ O caso é o do recorrente negativo: uma TAEG que decai com o prazo mais
// depressa do que um encargo antecipado sozinho explica. O sistema pede um
// recorrente abaixo de zero, o `limitar` prende-o em 0, e as iterações esgotam-se
// sem nunca lá chegar. Até aqui isso saía com `err == nil` e uma repartição que
// não reproduz observação nenhuma.
func TestUmAjusteQueNaoFechaSaiComoErroENaoComoMedicao(t *testing.T) {
	t.Parallel()

	capital, err := dominio.DinheiroDeTexto("320000")
	if err != nil {
		t.Fatalf("DinheiroDeTexto: %v", err)
	}
	tan := taxa(t, "3.200")

	// A distância entre a TAN e a TAEG estreita-se depressa demais para caber
	// num antecipado com recorrente não-negativo.
	var obs []dominio.ObservacaoDeEncargo
	for _, o := range []struct {
		meses int
		taeg  string
	}{
		{120, "4.900"}, {360, "3.700"}, {480, "3.560"},
	} {
		obs = append(obs, dominio.ObservacaoDeEncargo{
			Capital: capital, PrazoMeses: o.meses, TAN: tan, TAEG: taxa(t, o.taeg),
		})
	}

	ajustados, _, err := dominio.AjustarEncargos(obs)
	if err == nil {
		t.Fatalf("o ajuste não fechou e saiu como bom: antecipado=%s recorrente=%s",
			ajustados.Antecipado, ajustados.Recorrente)
	}
	if !errors.Is(err, dominio.ErrEncargosNaoAjustaveis) {
		t.Errorf("o erro não é da família dos não-ajustáveis: %v", err)
	}
	// ⚠️ A mensagem tem de nomear a coisa certa. «Não deu» manda quem lê procurar
	// no sítio errado — o que falta não são dados, é o modelo não os descrever.
	for _, exigido := range []string{"não convergiu", "recorrente preso em 0"} {
		if !strings.Contains(err.Error(), exigido) {
			t.Errorf("a mensagem não diz %q:\n  %v", exigido, err)
		}
	}
	t.Logf("recusado, e diz porquê: %v", err)
}

// Deslocar a TAEG de TODOS os prazos tem de mudar o ajuste (KAN-52).
//
// ⚠️ Era este o sintoma que denunciou o defeito: dois bancos com preçários
// diferentes saíam com encargos iguais ao cêntimo. Um ajuste preso na fronteira
// deixa de responder aos dados, e um modelo que não responde aos dados não é uma
// medição — é uma constante com ar de medição.
func TestDeslocarATAEGDeTodosOsPrazosMudaOAjuste(t *testing.T) {
	t.Parallel()

	capital, err := dominio.DinheiroDeTexto("320000")
	if err != nil {
		t.Fatalf("DinheiroDeTexto: %v", err)
	}
	tan := taxa(t, "3.200")

	// Uma série que FECHA, para o que se mede aqui ser a resposta aos dados e
	// não a recusa da anterior.
	ajustar := func(deslocamento string) dominio.Encargos {
		d := taxa(t, deslocamento)
		var obs []dominio.ObservacaoDeEncargo
		for _, o := range []struct {
			meses int
			taeg  string
		}{
			{120, "4.100"}, {360, "4.050"}, {480, "4.040"},
		} {
			obs = append(obs, dominio.ObservacaoDeEncargo{
				Capital: capital, PrazoMeses: o.meses, TAN: tan, TAEG: taxa(t, o.taeg).Add(d),
			})
		}
		e, _, err := dominio.AjustarEncargos(obs)
		if err != nil {
			t.Fatalf("deslocamento %s: %v", deslocamento, err)
		}
		return e
	}

	semDeslocamento := ajustar("0")
	comDeslocamento := ajustar("0.150")

	t.Logf("sem deslocamento: a=%s r=%s", semDeslocamento.Antecipado, semDeslocamento.Recorrente)
	t.Logf("com +0,150 p.p.:  a=%s r=%s", comDeslocamento.Antecipado, comDeslocamento.Recorrente)

	if semDeslocamento.Antecipado.Decimal().Equal(comDeslocamento.Antecipado.Decimal()) &&
		semDeslocamento.Recorrente.Decimal().Equal(comDeslocamento.Recorrente.Decimal()) {
		t.Error("deslocar a TAEG de todos os prazos em 0,150 p.p. não mudou os encargos ajustados")
	}
}

// O recorrente ajustado não vai para a frase com dezasseis casas (KAN-55).
//
// ⚠️ O valor deste teste é o que um ajuste a sério produz — saiu do catálogo de
// prova a 2026-08-06 — e não um número inventado com casas a mais. A KAN-51
// arrumou o antecipado na mesma frase e o recorrente ficou para trás, porque a
// fixture que a mediu tinha recorrente zero e zero não tem cauda decimal.
func TestORecorrenteAjustadoNaoVaiParaAFraseComDezasseisCasas(t *testing.T) {
	t.Parallel()

	recorrente, err := decimal.NewFromString("0.2490802248339295")
	if err != nil {
		t.Fatal(err)
	}
	e := dominio.Encargos{
		Antecipado: racio(t, "0.005066"),
		Recorrente: dominio.TaxaDeDecimal(recorrente),
	}

	var frase string
	for _, p := range e.Pressupostos(dominio.DinheiroDeInteiro(320_000)) {
		if strings.Contains(p, "ao ano sobre o capital") {
			frase = p
		}
	}
	if frase == "" {
		t.Fatal("não há pressuposto nenhum sobre o encargo recorrente")
	}
	if regexp.MustCompile(`\d,\d{4,}`).MatchString(frase) {
		t.Errorf("o pressuposto traz um número com quatro ou mais casas decimais, e é para uma pessoa ler: %s", frase)
	}
	t.Logf("%s", frase)
}
