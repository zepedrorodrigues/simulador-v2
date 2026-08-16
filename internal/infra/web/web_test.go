package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// A fronteira HTTP, **sem base nem rede**.
//
// ⚠️ **Este ficheiro encolheu com a Fase 6, passo 5.** Tinha cinco testes do
// `POST /api/v1/comparacoes` — a comparação servida da série varrida — e a rota
// saiu com o varrimento que a alimentava. O que eles afirmavam sobre validação
// (campo nomeado, produto de outro banco, id que não é banco nenhum) **não se
// perdeu**: vive no `aovivo_test.go`, contra a rota que ficou, e lá é afirmado
// no caminho por onde os pedidos passam mesmo.

func TestOsBancosSaemDoRegistoEDosRequisitos(t *testing.T) {
	s := servidor(t)

	pedido := httptest.NewRequest(http.MethodGet, "/api/v1/bancos", nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d: %s", resposta.Code, resposta.Body.String())
	}
	var r api.BancosResposta
	lerJSON(t, resposta, &r)

	if len(r.Bancos) != len(bancos.Predefinido().IDs()) {
		// ⚠️ BPI fica indisponível sem browser — a lista é menor.
		// O teste verifica que todos os bancos disponíveis aparecem.
		if len(r.Bancos) < len(bancos.Predefinido().IDs())-1 {
			t.Errorf("o registo tem %d bancos e a resposta traz %d", len(bancos.Predefinido().IDs()), len(r.Bancos))
		}
	}
	if len(r.InputsCanonicos) == 0 {
		t.Error("a resposta não traz o vocabulário do formulário")
	}

	// ⚠️ O Banco CTT impõe a Euribor e não a deixa escolher. Vazio nas opções
	// não é «não sei» — é «o banco impõe o seu», e o imposto vai ao lado.
	for _, b := range r.Bancos {
		if b.Id != "bancoctt" {
			continue
		}
		if len(b.EuriborOpcoes) != 0 {
			t.Errorf("o Banco CTT declara %d opções de Euribor e não deixa escolher", len(b.EuriborOpcoes))
		}
		if b.EuriborImposto == nil || *b.EuriborImposto != "12m" {
			t.Errorf("o Banco CTT impõe a Euribor a 12 meses, e a resposta diz %v", b.EuriborImposto)
		}
	}
}

func TestTodasAsRespostasLevamRequestID(t *testing.T) {
	s := servidor(t)

	pedido := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	// ⚠️ Na resposta e não só no log: sem ele, quem reporta um problema não tem
	// como dizer qual das respostas correu mal.
	if resposta.Header().Get("X-Request-ID") == "" {
		t.Error("a resposta não traz X-Request-ID")
	}
}

// --- ajudantes ---------------------------------------------------------------------

var relogio = func() time.Time { return time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC) }

// servidor monta a fronteira.
//
// ⚠️ **Deixou de receber observações** (Fase 6, passo 5): o `web.Novo` já não
// recebe fonte de dados nenhuma, porque não há série. O que este servidor serve
// vem dos bancos, e quem os troca por falsos é o `ComAoVivo`.
func servidor(t *testing.T) *web.Servidor {
	t.Helper()
	s, err := web.Novo(bancos.Predefinido(), relogio)
	if err != nil {
		t.Fatalf("Novo: %v", err)
	}
	return s
}

// pedidoDeProva é o pedido que todos os testes usam quando o pedido em si não é
// o que está a ser medido.
func pedidoDeProva() api.Pedido {
	return api.Pedido{
		ValorImovel: 400_000,
		Montante:    320_000,
		PrazoAnos:   30,
		RateType:    api.PedidoRateType(dominio.TaxaVariavel),
		Finalidade:  api.PedidoFinalidade(dominio.FinalidadePropria),
		Localizacao: string(dominio.LocalizacaoContinente),
		Titulares: []api.Titular{{
			DataNascimento:   openapi_types.Date{Time: time.Date(1996, 1, 15, 0, 0, 0, 0, time.UTC)},
			RendimentoMensal: 2000,
		}},
	}
}

func lerJSON(t *testing.T, r *httptest.ResponseRecorder, alvo any) {
	t.Helper()
	if err := json.Unmarshal(r.Body.Bytes(), alvo); err != nil {
		t.Fatalf("ler a resposta (%s): %v", r.Body.String(), err)
	}
}

func taxa(t *testing.T, s string) dominio.Taxa {
	t.Helper()
	x, err := dominio.TaxaDeTexto(s)
	if err != nil {
		t.Fatalf("taxa inválida %q: %v", s, err)
	}
	return x
}
