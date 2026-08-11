package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// TestVersaoComparaNumerosENaoTexto é o teste que esta funcionalidade existe
// para poder falhar.
//
// ⚠️ **Comparadas como texto, "1.10.0" fica ANTES de "1.9.0".** E o modo de
// falhar é o pior possível para o que isto faz: no dia em que a versão menor
// passar de 9 para 10, um servidor com o mínimo em 1.9.0 começa a mandar parar
// exactamente as apps **mais recentes** — as que acabaram de sair.
func TestVersaoComparaNumerosENaoTexto(t *testing.T) {
	dez := versao(t, "1.10.0")
	nove := versao(t, "1.9.0")

	if dez.Anterior(nove) {
		t.Errorf("a 1.10.0 foi dada como anterior à 1.9.0 — a comparação está a ser feita como texto")
	}
	if !nove.Anterior(dez) {
		t.Errorf("a 1.9.0 não foi dada como anterior à 1.10.0")
	}
	if strings.Compare("1.10.0", "1.9.0") >= 0 {
		t.Fatal("o pressuposto deste teste caiu: em texto, a 1.10.0 já não vem antes da 1.9.0")
	}
}

func TestVersaoOrdenaPelosTresNumeros(t *testing.T) {
	casos := []struct {
		a, b     string
		anterior bool
	}{
		{"0.9.9", "1.0.0", true},
		{"1.0.0", "0.9.9", false},
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "1.2.3", false},
		{"2.0.0", "10.0.0", true},
	}
	for _, c := range casos {
		if got := versao(t, c.a).Anterior(versao(t, c.b)); got != c.anterior {
			t.Errorf("%s anterior a %s: deu %v, esperava %v", c.a, c.b, got, c.anterior)
		}
	}
}

// ⚠️ Um sufixo é recusado em vez de ignorado: aceitá-lo obrigava a decidir se
// `1.2.3-rc1` vem antes ou depois de `1.2.3`, e essa ordem não se inventa antes
// de existir uma pré-lançamento.
func TestVersaoRecusaOQueNaoSaoTresNumeros(t *testing.T) {
	for _, s := range []string{"", "1", "1.2", "1.2.3.4", "1.2.x", "1.2.3-rc1", "v1.2.3", "-1.2.3"} {
		if _, err := web.VersaoDe(s); err == nil {
			t.Errorf("%q foi aceite como versão", s)
		}
	}
}

// ⚠️ Vazio desliga, e não é o mesmo que um erro: ligar um mínimo sem versão
// publicada no terreno não protege nada e arrisca mandar parar a única app que
// existe.
func TestVersaoMinimaVaziaDesliga(t *testing.T) {
	v, err := web.VersaoMinimaDe("   ")
	if err != nil {
		t.Fatalf("vazio devia desligar e deu erro: %v", err)
	}
	if v != nil {
		t.Errorf("vazio devia desligar e deu %v", v)
	}

	if _, err := web.VersaoMinimaDe("nada disto"); err == nil {
		t.Error("uma versão mínima ilegível tem de falhar o arranque, e passou")
	}
}

func TestVersaoDemasiadoAntigaRecebe426(t *testing.T) {
	resp := pedirComVersao(t, versaoPtr(t, "1.2.0"), "1.1.9")

	if resp.Code != http.StatusUpgradeRequired {
		t.Fatalf("uma app abaixo do mínimo devia receber 426 e recebeu %d", resp.Code)
	}

	var corpo struct {
		Erro struct {
			Codigo   string `json:"codigo"`
			Mensagem string `json:"mensagem"`
		} `json:"erro"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &corpo); err != nil {
		t.Fatalf("o corpo do 426 não é o envelope de erro: %v", err)
	}
	if corpo.Erro.Codigo != "versao_demasiado_antiga" {
		t.Errorf("o código do 426 é %q e devia ser versao_demasiado_antiga", corpo.Erro.Codigo)
	}
	// ⚠️ A mensagem é para a pessoa ler e tem de dizer **para onde** actualizar.
	// «Versão não suportada» sem o número manda alguém procurar o quê.
	if !strings.Contains(corpo.Erro.Mensagem, "1.2.0") {
		t.Errorf("a mensagem não diz para que versão actualizar: %q", corpo.Erro.Mensagem)
	}
}

// ⚠️ **A ausência do cabeçalho passa, e é a afirmação mais importante daqui.**
// O alvo web, o `curl` e a app de hoje não o mandam. Exigi-lo era, em si, uma
// mudança que parte o `/api/v1` — exactamente o que este caminho existe para
// evitar.
func TestSemCabecalhoServeSe(t *testing.T) {
	if resp := pedirComVersao(t, versaoPtr(t, "9.9.9"), ""); resp.Code == http.StatusUpgradeRequired {
		t.Error("um pedido sem o cabeçalho de versão foi recusado por ser velho, e não diz que é")
	}
}

// ⚠️ Falha ABERTO, como o tecto: tratar o que não se percebe como demasiado
// antigo transforma um defeito de escrita do cliente num bloqueio total — e para
// o corrigir teria de sair uma versão nova, que é o que o bloqueio impede de
// instalar.
func TestVersaoIlegivelServeSe(t *testing.T) {
	for _, bruto := range []string{"a.b.c", "1.2", "muito recente"} {
		if resp := pedirComVersao(t, versaoPtr(t, "9.9.9"), bruto); resp.Code == http.StatusUpgradeRequired {
			t.Errorf("a versão ilegível %q foi tratada como velha", bruto)
		}
	}
}

func TestVersaoNoMinimoOuAcimaServeSe(t *testing.T) {
	for _, bruto := range []string{"1.2.0", "1.2.1", "1.3.0", "2.0.0"} {
		if resp := pedirComVersao(t, versaoPtr(t, "1.2.0"), bruto); resp.Code == http.StatusUpgradeRequired {
			t.Errorf("a versão %q não é anterior ao mínimo 1.2.0 e foi recusada", bruto)
		}
	}
}

// ⚠️ Sem mínimo declarado não se recusa ninguém, por muito velha que a versão
// seja. É o estado de partida do serviço.
func TestSemMinimoNaoSeRecusaNinguem(t *testing.T) {
	if resp := pedirComVersao(t, nil, "0.0.1"); resp.Code == http.StatusUpgradeRequired {
		t.Error("sem APP_VERSAO_MINIMA declarada, uma versão antiga foi recusada")
	}
}

func versao(t *testing.T, s string) web.Versao {
	t.Helper()
	v, err := web.VersaoDe(s)
	if err != nil {
		t.Fatalf("ler a versão %q: %v", s, err)
	}
	return v
}

func versaoPtr(t *testing.T, s string) *web.Versao {
	t.Helper()
	v := versao(t, s)
	return &v
}

// pedirComVersao faz um pedido ao `/api/v1/bancos` com o cabeçalho dado.
//
// ⚠️ Usa-se uma rota que **não** fala com bancos: o que se mede é o middleware,
// e uma rota de ofertas mandava este teste à rede a um simulador de terceiros.
func pedirComVersao(t *testing.T, minima *web.Versao, cabecalho string) *httptest.ResponseRecorder {
	t.Helper()

	s := servidor(t).ComVersaoMinima(minima)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bancos", nil)
	if cabecalho != "" {
		req.Header.Set(web.CabecalhoDaVersao, cabecalho)
	}
	resp := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resp, req)
	return resp
}
