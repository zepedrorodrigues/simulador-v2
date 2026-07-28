package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// O CORS da §6: lista explícita, sem credenciais, e o catálogo de fora.

const origemDaApp = "https://simulador.exemplo.pt"

func TestOCoringaERecusadoAoArranque(t *testing.T) {
	// ⚠️ É a configuração que alguém escreve para «desbloquear» a app, e o custo
	// dela não é visível no momento em que resolve o problema.
	_, err := web.OrigensDe("*")
	if err == nil {
		t.Fatal("`*` foi aceite como lista de origens")
	}
	if !strings.Contains(err.Error(), "lista explícita") {
		t.Errorf("o erro não diz porque é que o coringa não serve: %v", err)
	}
}

func TestUmaOrigemMalEscritaFalhaOArranqueEmVezDeFalharEmSilencio(t *testing.T) {
	// Cada uma destas nunca casaria com o `Origin` que um browser envia — e sem
	// esta guarda o CORS ficava ligado, a configuração parecia certa, e a app
	// recebia erros que não nomeiam nada.
	casos := map[string]string{
		"com barra final":    "https://exemplo.pt/",
		"com caminho":        "https://exemplo.pt/app",
		"sem esquema":        "exemplo.pt",
		"com esquema errado": "ftp://exemplo.pt",
		"sem host":           "https://",
		"com query":          "https://exemplo.pt?x=1",
	}
	for nome, origem := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := web.OrigensDe(origem); err == nil {
				t.Errorf("%q foi aceite como origem", origem)
			}
		})
	}

	// E as boas passam, incluindo porta e localhost — que é o caso de quem
	// desenvolve.
	origens, err := web.OrigensDe(" https://simulador.exemplo.pt , http://localhost:8081 ")
	if err != nil {
		t.Fatalf("origens válidas recusadas: %v", err)
	}
	if len(origens) != 2 {
		t.Errorf("esperava 2 origens, vieram %v", origens)
	}
}

func TestSoAOrigemDaListaRecebePermissao(t *testing.T) {
	servidor := servidorComOrigens(t, origemDaApp)

	casos := []struct {
		nome      string
		origem    string
		permitida bool
	}{
		{"a da lista", origemDaApp, true},
		{"outra qualquer", "https://atacante.exemplo", false},
		// ⚠️ Sem correspondência por sufixo: um casamento ingénuo por «acaba em
		// exemplo.pt» deixava passar esta.
		{"a que parece a da lista", "https://simulador.exemplo.pt.atacante.example", false},
		{"a mesma noutro esquema", "http://simulador.exemplo.pt", false},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			req.Header.Set("Origin", c.origem)
			resp := httptest.NewRecorder()
			servidor.Rotas().ServeHTTP(resp, req)

			permitida := resp.Header().Get("Access-Control-Allow-Origin")
			switch {
			case c.permitida && permitida != c.origem:
				t.Errorf("a origem da lista não foi permitida: Allow-Origin = %q", permitida)
			case !c.permitida && permitida != "":
				t.Errorf("origem fora da lista recebeu permissão: Allow-Origin = %q", permitida)
			}

			// ⚠️ O pedido é servido de qualquer forma. Quem bloqueia é o browser;
			// devolver 403 aqui partia todos os clientes que não são browsers.
			if resp.Code != http.StatusOK {
				t.Errorf("o pedido não foi servido: %d", resp.Code)
			}
		})
	}
}

func TestNuncaSaiUmCoringaNemCredenciais(t *testing.T) {
	servidor := servidorComOrigens(t, origemDaApp)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", origemDaApp)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	if got := resp.Header().Get("Access-Control-Allow-Origin"); got == "*" {
		t.Error("saiu `*` no Access-Control-Allow-Origin")
	}
	// Sem cookies e sem sessões, não há nada para autenticar — e com credenciais
	// a verdade, a §6 deixava de poder confiar na lista.
	if got := resp.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("saiu Allow-Credentials = %q, e não há credenciais nesta API", got)
	}
}

func TestARespostaDizQueVariaComAOrigem(t *testing.T) {
	// ⚠️ Sem `Vary: Origin`, uma cache pelo caminho serve a toda a gente a
	// resposta que guardou para a primeira origem que passou — incluindo o
	// Allow-Origin dela.
	servidor := servidorComOrigens(t, origemDaApp)

	for _, origem := range []string{origemDaApp, "https://outra.exemplo"} {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", origem)
		resp := httptest.NewRecorder()
		servidor.Rotas().ServeHTTP(resp, req)

		if !strings.Contains(resp.Header().Get("Vary"), "Origin") {
			t.Errorf("origem %q: falta `Vary: Origin` (Vary = %q)", origem, resp.Header().Get("Vary"))
		}
	}
}

func TestOPreflightDaComparacaoResponde(t *testing.T) {
	servidor := servidorComOrigens(t, origemDaApp)

	// É o que o browser manda antes de um POST com Content-Type: application/json.
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/comparacoes", nil)
	req.Header.Set("Origin", origemDaApp)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("o preflight devolveu %d — sem rota registada o chi responde 405 e a app "+
			"falha com um erro de CORS que não nomeia nada", resp.Code)
	}
	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != origemDaApp {
		t.Errorf("o preflight não permitiu a origem: %q", got)
	}
	if got := resp.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
		t.Errorf("o preflight não permite POST: %q", got)
	}
	if got := resp.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(
		strings.ToLower(got), "content-type") {
		t.Errorf("o preflight não permite Content-Type, e sem ele não há JSON: %q", got)
	}
}

// TestOCatalogoNaoRespondeAUmBrowser é a afirmação central desta secção.
//
// ⚠️ O `/api/rate-catalog` autentica-se por `X-API-Key`. Se respondesse a
// preflights, estava a convidar quem escreve a app a pôr lá a chave — e uma
// chave dentro de um bundle de browser é pública.
func TestOCatalogoNaoRespondeAUmBrowser(t *testing.T) {
	servidor := servidorComOrigens(t, origemDaApp)

	req := httptest.NewRequest(http.MethodGet, "/api/rate-catalog", nil)
	req.Header.Set("Origin", origemDaApp)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("o catálogo deu permissão de CORS a %q — a chave dele acabaria num browser", got)
	}
}

func TestSemOrigensDeclaradasNaoSaiCabecalhoNenhum(t *testing.T) {
	// Vazio não é «tudo»: é «sem cabeçalhos», ou seja, só a mesma origem.
	servidor := servidorComOrigens(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", origemDaApp)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("sem origens declaradas saiu Allow-Origin = %q", got)
	}
}

func servidorComOrigens(t *testing.T, origens ...string) *web.Servidor {
	t.Helper()
	return servidorComCatalogo(t, nil).ComOrigens(origens)
}

// TestOPreflightNaoGastaDoTecto é a consequência que ligar o CORS trouxe, e que
// não estava à vista.
//
// ⚠️ Um browser manda um `OPTIONS` antes de cada `POST /api/v1/comparacoes` — o
// `Content-Type: application/json` obriga-o. Sem esta guarda, **cada comparação
// feita da app custava dois do tecto** e a mesma comparação feita por `curl`
// custava um: o tecto passava a medir o cliente em vez do uso, e a app ficava
// com metade do limite anunciado.
func TestOPreflightNaoGastaDoTecto(t *testing.T) {
	const tectoDeDois = 2
	servidor := servidorComTecto(t, web.Tecto{Pedidos: tectoDeDois, Janela: time.Minute}).
		ComOrigens([]string{origemDaApp})

	// Três preflights — mais do que o tecto inteiro.
	for i := range 3 {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/comparacoes", nil)
		req.Header.Set("Origin", origemDaApp)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		resp := httptest.NewRecorder()
		servidor.Rotas().ServeHTTP(resp, req)

		if resp.Code == http.StatusTooManyRequests {
			t.Fatalf("o preflight %d levou 429: os preflights estão a gastar do tecto, "+
				"e a app fica com metade do limite que um cliente sem browser tem", i+1)
		}
	}

	// E o tecto continua a valer para os pedidos a sério.
	for i := range tectoDeDois {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		resp := httptest.NewRecorder()
		servidor.Rotas().ServeHTTP(resp, req)
		if resp.Code == http.StatusTooManyRequests {
			t.Fatalf("o pedido %d de %d levou 429 — os preflights consumiram o tecto na mesma",
				i+1, tectoDeDois)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)
	if resp.Code != http.StatusTooManyRequests {
		t.Errorf("o tecto deixou de morder: o pedido acima do limite devolveu %d", resp.Code)
	}
}

// TestUmOptionsQualquerNaoEscapaAoTecto fecha o buraco que o teste acima abriria
// sozinho.
//
// ⚠️ Se bastasse o método para escapar ao tecto, qualquer cliente o contornava
// escolhendo `OPTIONS`. O que define um preflight é o
// `Access-Control-Request-Method`, e é isso que se exige.
func TestUmOptionsQualquerNaoEscapaAoTecto(t *testing.T) {
	servidor := servidorComTecto(t, web.Tecto{Pedidos: 1, Janela: time.Minute}).
		ComOrigens([]string{origemDaApp})

	for i := range 3 {
		// Sem `Access-Control-Request-Method`: não é preflight nenhum.
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/comparacoes", nil)
		req.Header.Set("Origin", origemDaApp)
		resp := httptest.NewRecorder()
		servidor.Rotas().ServeHTTP(resp, req)

		if i > 0 && resp.Code == http.StatusTooManyRequests {
			return // mordeu, como devia
		}
	}
	t.Error("três OPTIONS sem Access-Control-Request-Method passaram um tecto de 1: " +
		"escolher o método chega para escapar ao tecto")
}
