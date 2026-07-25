package dominio_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

func dinheiro(t *testing.T, s string) dominio.Dinheiro {
	t.Helper()
	d, err := dominio.DinheiroDeTexto(s)
	if err != nil {
		t.Fatalf("montante de teste inválido %q: %v", s, err)
	}
	return d
}

func taxa(t *testing.T, s string) dominio.Taxa {
	t.Helper()
	x, err := dominio.TaxaDeTexto(s)
	if err != nil {
		t.Fatalf("taxa de teste inválida %q: %v", s, err)
	}
	return x
}

func TestDinheiroEqualIgnoraAEscala(t *testing.T) {
	// 100 e 100.00 são o mesmo dinheiro. Com == seriam diferentes — e é por
	// isso que == não compila neste tipo.
	if !dinheiro(t, "100").Equal(dinheiro(t, "100.00")) {
		t.Error("100 e 100.00 deviam ser iguais")
	}
	if dinheiro(t, "100").Equal(dinheiro(t, "100.01")) {
		t.Error("100 e 100.01 não são iguais")
	}
}

func TestDinheiroZeroEUsavel(t *testing.T) {
	var d dominio.Dinheiro
	if d.String() != "0" {
		t.Errorf("valor por omissão = %q, queria \"0\"", d)
	}
	if d.Positivo() {
		t.Error("zero não é positivo")
	}
	if !d.Equal(dominio.DinheiroDeInteiro(0)) {
		t.Error("o valor por omissão devia ser igual a zero construído")
	}
}

func TestCmpDiffUsaOEqualDosTipos(t *testing.T) {
	// O go-cmp encontra o método Equal por reflexão. É por isso que os testes
	// dos bancos vão poder comparar Ofertas inteiras com cmp.Diff, sem
	// cmp.Comparer nenhum — e é por isso que o método se chama Equal e não
	// Igual.
	if d := cmp.Diff(dinheiro(t, "100"), dinheiro(t, "100.00")); d != "" {
		t.Errorf("cmp.Diff devia ver 100 e 100.00 como iguais, e disse:\n%s", d)
	}
	if cmp.Diff(taxa(t, "3.25"), taxa(t, "3.26")) == "" {
		t.Error("cmp.Diff devia distinguir 3,25 de 3,26")
	}
}

func TestDinheiroDeTextoRecusaLixo(t *testing.T) {
	// ⚠️ O formato português — vírgula decimal, espaço não-quebrável — é
	// traduzido por cada banco no seu resposta.go, e nunca por float64.
	for _, s := range []string{"", "1.303,23", "mil euros", "1 303,23"} {
		if _, err := dominio.DinheiroDeTexto(s); err == nil {
			t.Errorf("%q foi aceite e não devia", s)
		}
	}
}

func TestTaxaSomaESubtrai(t *testing.T) {
	// Spread mais Euribor é a TAN da fase indexada.
	if got := taxa(t, "0.9").Add(taxa(t, "2.351")); !got.Equal(taxa(t, "3.251")) {
		t.Errorf("0,9 + 2,351 = %s, queria 3,251", got)
	}
	// E a Euribor do Crédito Agrícola obtém-se ao contrário: a TAN menos o
	// campo que diz chamar-se índice e é o spread.
	if got := taxa(t, "3.251").Sub(taxa(t, "0.9")); !got.Equal(taxa(t, "2.351")) {
		t.Errorf("3,251 - 0,9 = %s, queria 2,351", got)
	}
}
