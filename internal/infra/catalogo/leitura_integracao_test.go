package catalogo_test

import (
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/comparar"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/catalogo"
)

// A volta completa: grava-se um lote, lê-se, e responde-se com ele.
//
// ⚠️ É o teste que fecha o círculo do desenho invertido — varrer, gravar, ler,
// responder — e é o primeiro sítio onde as duas metades se encontram. Uma
// observação que não sobreviva à ida e volta parte a resposta **em produção**,
// e não aqui: sem isto, o `comparar` só estava afirmado sobre observações
// construídas em memória.
func TestOQueSeGravaVoltaEServeParaResponder(t *testing.T) {
	pool := subirBase(t)
	cat := catalogo.NovoPostgres(pool)

	lote := append(degrausDaCGD(t), fixaDaCGD(t), observacaoDeProva())
	if _, err := cat.GravarLote(t.Context(), lote); err != nil {
		t.Fatalf("gravar: %v", err)
	}

	obs, err := cat.UltimoVarrimento(t.Context())
	if err != nil {
		t.Fatalf("UltimoVarrimento: %v", err)
	}
	if len(obs) != len(lote) {
		t.Fatalf("gravaram-se %d observações e voltaram %d", len(lote), len(obs))
	}

	// ⚠️ Os degraus têm de voltar COM o intervalo: sem eles não há escala, e sem
	// escala o comparar não responde a quem não caia no ponto medido.
	comDegrau := 0
	for _, o := range obs {
		if o.Degrau != nil {
			comDegrau++
		}
	}
	if comDegrau != len(degrausDaCGD(t)) {
		t.Errorf("voltaram %d degraus de %d gravados", comDegrau, len(degrausDaCGD(t)))
	}

	// E a escala reconstrói-se a partir do que voltou.
	if _, tem := varrimento.EscalasPorBanco(obs)["cgd"]; !tem {
		t.Fatal("a escala da CGD não se reconstruiu a partir do que voltou da base")
	}

	// A prova final: o catálogo de resposta constrói-se com isto.
	if _, err := comparar.NovoCatalogo(obs); err != nil {
		t.Fatalf("o que voltou da base não serve para responder: %v", err)
	}
}

// TestUmaObservacaoVoltaComOsNumerosQueLaForam: a ida e volta não pode arredondar
// nem perder campos — é sobre estes números que uma resposta se calcula.
func TestUmaObservacaoVoltaComOsNumerosQueLaForam(t *testing.T) {
	pool := subirBase(t)
	cat := catalogo.NovoPostgres(pool)

	original := observacaoDeProva()
	if _, err := cat.GravarLote(t.Context(), []varrimento.Observacao{original}); err != nil {
		t.Fatalf("gravar: %v", err)
	}

	obs, err := cat.UltimoVarrimento(t.Context())
	if err != nil {
		t.Fatalf("UltimoVarrimento: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("esperava uma observação, vieram %d", len(obs))
	}
	voltou := obs[0]

	verTaxaIgual(t, "TAN", original.Oferta.TAN, voltou.Oferta.TAN)
	verTaxaIgual(t, "TAEG", original.Oferta.TAEG, voltou.Oferta.TAEG)
	verTaxaIgual(t, "spread", original.Oferta.Spread, voltou.Oferta.Spread)
	verTaxaIgual(t, "Euribor", original.Oferta.EuriborValor, voltou.Oferta.EuriborValor)

	if voltou.Ponto.Cenario != original.Ponto.Cenario {
		t.Errorf("cenário: foi %q, voltou %q", original.Ponto.Cenario, voltou.Ponto.Cenario)
	}
	if !voltou.Ponto.Pedido.Montante.Equal(original.Ponto.Pedido.Montante) {
		t.Errorf("montante: foi %s, voltou %s", original.Ponto.Pedido.Montante, voltou.Ponto.Pedido.Montante)
	}
	// ⚠️ A hora do varrimento é o que torna a resposta honesta. Perdê-la na ida
	// e volta era servir um preço de ontem como se fosse de agora.
	if voltou.Oferta.CapturadoEm.IsZero() {
		t.Error("a observação voltou sem a hora a que o preço foi medido")
	}
}

func verTaxaIgual(t *testing.T, nome string, foi, voltou *dominio.Taxa) {
	t.Helper()
	if foi == nil && voltou == nil {
		return
	}
	if foi == nil || voltou == nil {
		t.Errorf("%s: foi %v, voltou %v", nome, foi, voltou)
		return
	}
	if !foi.Equal(*voltou) {
		t.Errorf("%s: foi %s, voltou %s", nome, foi, voltou)
	}
}
