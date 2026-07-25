package transporte_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
)

// lerTudo lê o corpo e não o fecha: fecha-o quem o pediu, como no net/http.
func lerTudo(t *testing.T, resp *http.Response) string {
	t.Helper()

	corpo, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ler o corpo: %v", err)
	}
	return string(corpo)
}

func TestClienteFazOPedido(t *testing.T) {
	t.Parallel()

	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "método="+r.Method)
	}))
	defer servidor.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, servidor.URL, strings.NewReader("x=1"))
	if err != nil {
		t.Fatalf("montar o pedido: %v", err)
	}

	resp, err := transporte.NovoCliente(nil).Fazer(t.Context(), req)
	if err != nil {
		t.Fatalf("Fazer: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if corpo := lerTudo(t, resp); corpo != "método=POST" {
		t.Errorf("corpo = %q", corpo)
	}
}

// O prazo é o do ctx e não o do cliente: é o orquestrador que o impõe, por
// banco.
func TestClienteDesisteComOCtx(t *testing.T) {
	t.Parallel()

	solto := make(chan struct{})
	servidor := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-solto
	}))
	defer func() {
		close(solto)
		servidor.Close()
	}()

	ctx, cancelar := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancelar()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, servidor.URL, nil)
	if err != nil {
		t.Fatalf("montar o pedido: %v", err)
	}

	inicio := time.Now()
	resp, err := transporte.NovoCliente(nil).Fazer(ctx, req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Fazer devolveu nil com o prazo esgotado")
	}
	if demorou := time.Since(inicio); demorou > time.Second {
		t.Errorf("desistiu ao fim de %s, com um prazo de 50ms", demorou)
	}
}

// O que a estratégia de sessão existe para fazer: o GET fixa os cookies e traz
// o HTML, e o pedido seguinte leva os cookies de volta. Sem isto, o Montepio
// responde 410.
func TestClienteComSessaoArrancaEGuardaOsCookies(t *testing.T) {
	t.Parallel()

	const html = `<input id="HashRequest" value="H4SH" />`

	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/arranque" {
			http.SetCookie(w, &http.Cookie{Name: "ASP.NET_SessionId", Value: "sess-123", Path: "/"})
			_, _ = io.WriteString(w, html)
			return
		}
		cookie, err := r.Cookie("ASP.NET_SessionId")
		if err != nil {
			w.WriteHeader(http.StatusGone)
			_, _ = io.WriteString(w, "New open window with different context")
			return
		}
		_, _ = io.WriteString(w, "sessão="+cookie.Value)
	}))
	defer servidor.Close()

	cliente, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		t.Fatalf("NovoClienteComSessao: %v", err)
	}

	sessao, err := cliente.Arrancar(t.Context(), servidor.URL+"/arranque")
	if err != nil {
		t.Fatalf("Arrancar: %v", err)
	}
	if string(sessao.HTML) != html {
		t.Errorf("HTML de arranque = %q", sessao.HTML)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, servidor.URL+"/simular", nil)
	if err != nil {
		t.Fatalf("montar o pedido: %v", err)
	}
	resp, err := cliente.Fazer(t.Context(), req)
	if err != nil {
		t.Fatalf("Fazer: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if corpo := lerTudo(t, resp); corpo != "sessão=sess-123" {
		t.Errorf("o POST foi sem os cookies do arranque: %q", corpo)
	}
}

// Cada simulação leva o seu cliente, e por isso o seu jar: os cookies de sessão
// de um pedido não têm nada que ir no pedido de outra pessoa.
func TestClientesComSessaoNaoPartilhamCookies(t *testing.T) {
	t.Parallel()

	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/arranque" {
			http.SetCookie(w, &http.Cookie{Name: "sessao", Value: "de-outra-pessoa", Path: "/"})
			return
		}
		if _, err := r.Cookie("sessao"); err != nil {
			_, _ = io.WriteString(w, "sem cookie")
			return
		}
		_, _ = io.WriteString(w, "com cookie")
	}))
	defer servidor.Close()

	primeiro, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		t.Fatalf("NovoClienteComSessao: %v", err)
	}
	if _, err := primeiro.Arrancar(t.Context(), servidor.URL+"/arranque"); err != nil {
		t.Fatalf("Arrancar: %v", err)
	}

	segundo, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		t.Fatalf("NovoClienteComSessao: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, servidor.URL+"/simular", nil)
	if err != nil {
		t.Fatalf("montar o pedido: %v", err)
	}
	resp, err := segundo.Fazer(t.Context(), req)
	if err != nil {
		t.Fatalf("Fazer: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if corpo := lerTudo(t, resp); corpo != "sem cookie" {
		t.Errorf("o segundo cliente foi com os cookies do primeiro: %q", corpo)
	}
}

// Um arranque que responde mal é erro aqui, e não uma resposta estranha mais à
// frente: no Montepio, quem chega ao gateway sem sessão leva um 410 com uma
// mensagem sobre janelas — que não se parece nada com "o arranque falhou".
func TestArranqueQueNaoRespondeOKEErro(t *testing.T) {
	t.Parallel()

	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer servidor.Close()

	cliente, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		t.Fatalf("NovoClienteComSessao: %v", err)
	}

	_, err = cliente.Arrancar(t.Context(), servidor.URL)
	if err == nil {
		t.Fatal("Arrancar devolveu nil com um 410")
	}
	if !strings.Contains(err.Error(), "410") {
		t.Errorf("o erro não diz o estado: %v", err)
	}
}
