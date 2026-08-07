package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// Os cabeçalhos de defesa (KAN-46), que a API.md §3 prometia e ninguém emitia.
//
// ⚠️ O que este teste tem de apanhar não é «o caminho feliz leva os
// cabeçalhos» — é que **as respostas de erro também os levam**. Um middleware
// montado abaixo do `limitar` ou do `Recoverer` na cadeia serve os cabeçalhos
// nos 200 e não nos 429 nem nos 500, e a suite passava na mesma.

var cabecalhosDeDefesa = map[string]string{
	"X-Content-Type-Options": "nosniff",
	"Referrer-Policy":        "no-referrer",
}

func verDefesa(t *testing.T, r *httptest.ResponseRecorder, onde string) {
	t.Helper()
	for nome, esperado := range cabecalhosDeDefesa {
		// ⚠️ A mensagem nomeia o cabeçalho em falta. «Os cabeçalhos não batem
		// certo» obrigava a ir ao código descobrir qual.
		if lido := r.Header().Get(nome); lido != esperado {
			t.Errorf("%s: %s = %q, esperava %q", onde, nome, lido, esperado)
		}
	}
}

func TestOsCabecalhosDeDefesaVaoNasRespostasBoas(t *testing.T) {
	s := servidor(t)

	pedido := httptest.NewRequest(http.MethodGet, "/api/v1/bancos", nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d, esperava 200", resposta.Code)
	}
	verDefesa(t, resposta, "GET /api/v1/bancos")
}

// TestOsCabecalhosDeDefesaVaoTambemNasRespostasDeErro é o critério que decide
// onde o middleware se monta. As três respostas nascem em sítios diferentes da
// cadeia: o 400 num handler, o 429 no `limitar`, e o 404 no próprio chi, antes
// de qualquer handler nosso correr.
func TestOsCabecalhosDeDefesaVaoTambemNasRespostasDeErro(t *testing.T) {
	t.Run("400 de um handler", func(t *testing.T) {
		// ⚠️ Vinha do `/comparacoes` com um banco que não existe, e essa rota saiu
		// com o varrimento. Passa a vir do `/ofertas/{banco}` com um pedido que não
		// passa a validação: o que importa aqui é o 400 nascer **num handler**, e
		// não qual deles.
		s := servidorAoVivo(t, &bancoFalso{id: "cgd", nome: "CGD"})
		corpo := corpoDeOferta(nil)
		corpo.Pedido.Montante = 900_000 // acima do valor do imóvel

		resposta := pedirOferta(t, s, "cgd", corpo)
		if resposta.Code != http.StatusBadRequest {
			t.Fatalf("estado %d, esperava 400", resposta.Code)
		}
		verDefesa(t, resposta, "400")
	})

	t.Run("429 do tecto por IP", func(t *testing.T) {
		s := servidorComTecto(t, web.Tecto{Pedidos: 1, Janela: 90 * time.Second})
		pedirComXFF(t, s, "", "")

		pedido := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		resposta := httptest.NewRecorder()
		s.Rotas().ServeHTTP(resposta, pedido)

		if resposta.Code != http.StatusTooManyRequests {
			t.Fatalf("estado %d, esperava 429", resposta.Code)
		}
		verDefesa(t, resposta, "429")
	})

	t.Run("404 do router, antes de qualquer handler nosso", func(t *testing.T) {
		s := servidor(t)

		pedido := httptest.NewRequest(http.MethodGet, "/rota-que-nao-existe", nil)
		resposta := httptest.NewRecorder()
		s.Rotas().ServeHTTP(resposta, pedido)

		if resposta.Code != http.StatusNotFound {
			t.Fatalf("estado %d, esperava 404", resposta.Code)
		}
		verDefesa(t, resposta, "404")
	})
}

// ⚠️ **Havia aqui um `TestOCatalogoCongeladoTambemLevaOsCabecalhos`**, que
// afirmava que a fronteira congelada levava os cabeçalhos tanto no 200 como no
// 401 da chave. Saiu com a rota (Fase 6, passo 5), e com ela saiu o **único
// caso de 401** que este ficheiro cobria — não há hoje superfície autenticada
// nenhuma.
//
// O que ele respondia continua respondido pelos casos acima: os cabeçalhos
// montam-se no topo da cadeia e aparecem em respostas que nascem em três sítios
// diferentes — um handler, o `limitar` e o próprio chi.

// TestNaoSeEmiteHSTS fixa a decisão, e não é zelo: o serviço fala HTTP em claro
// atrás do proxy e **não tem como saber** se o que está à frente serve TLS.
// Emitir HSTS daqui num host de desenvolvimento prendia-o a HTTPS no browser
// durante meses. Quem termina o TLS é que o emite.
//
// ⚠️ Se um dia sair daqui, é porque o PROXIES_DE_CONFIANCA passou a estar
// medido — e então passam a ser duas coisas que falham juntas. Este teste é o
// sítio onde essa mudança tem de ser deliberada.
func TestNaoSeEmiteHSTS(t *testing.T) {
	s := servidor(t)

	pedido := httptest.NewRequest(http.MethodGet, "/api/v1/bancos", nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	if lido := resposta.Header().Get("Strict-Transport-Security"); lido != "" {
		t.Errorf("o serviço emitiu HSTS (%q), e a decisão é deixá-lo ao proxy", lido)
	}
}
