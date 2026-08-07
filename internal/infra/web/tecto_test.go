package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// O tecto por IP, e o bug de produção do v1 que ele existe para não repetir.
//
// ⚠️ No v1, atrás de um proxy noutro contentor, o `X-Forwarded-For` não era
// aceite e o IP do cliente passava a ser o do proxy — igual para toda a gente.
// O tecto virava o tecto do site inteiro, e o primeiro visitante trancava os
// outros.

// TestUmXForwardedForDirectoEIgnorado é o primeiro critério de pronto da KAN-14.
//
// ⚠️ Sem proxy de confiança configurado, um `X-Forwarded-For` que chegue
// directamente à app é uma tentativa de escolher o próprio identificador. Aceitá-lo
// deixava qualquer cliente contornar o tecto de vez — que é pior do que não ter
// tecto, porque parece que se tem.
func TestUmXForwardedForDirectoEIgnorado(t *testing.T) {
	s := servidorComTecto(t, web.Tecto{Pedidos: 3, Janela: time.Minute})

	// Três pedidos gastam o tecto do endereço da ligação.
	for i := range 3 {
		if estado := pedirComXFF(t, s, "", ""); estado != http.StatusOK {
			t.Fatalf("pedido %d: estado %d", i+1, estado)
		}
	}

	// O quarto, com um XFF inventado a cada vez, continua a ser o mesmo cliente.
	for i, forjado := range []string{"1.2.3.4", "5.6.7.8", "9.10.11.12"} {
		if estado := pedirComXFF(t, s, forjado, ""); estado != http.StatusTooManyRequests {
			t.Errorf("com X-Forwarded-For: %s o cliente escapou ao tecto (tentativa %d, estado %d)",
				forjado, i+1, estado)
		}
	}
}

// TestComProxyDeConfiancaCadaClienteTemOSeuTecto é o segundo critério: o
// isolamento entre máquinas.
//
// ⚠️ E é medido contra um **proxy a sério** — um `httputil.ReverseProxy` a
// correr, que põe o `X-Forwarded-For` ele próprio — e não contra cabeçalhos
// escritos à mão no teste. A issue pede isso, e a razão é boa: o que partiu no v1
// foi a interacção com um proxy real, não a leitura de um cabeçalho.
func TestComProxyDeConfiancaCadaClienteTemOSeuTecto(t *testing.T) {
	// O proxy corre em 127.0.0.1, e é essa a rede de confiança.
	redes, err := web.RedesDe("127.0.0.0/8")
	if err != nil {
		t.Fatalf("RedesDe: %v", err)
	}
	s := servidorComTecto(t, web.Tecto{Pedidos: 2, Janela: time.Minute, ProxiesDeConfianca: redes})

	aplicacao := httptest.NewServer(s.Rotas())
	defer aplicacao.Close()

	alvo, err := url.Parse(aplicacao.URL)
	if err != nil {
		t.Fatalf("URL da aplicação: %v", err)
	}
	// ⚠️ O ReverseProxy da stdlib põe o X-Forwarded-For sozinho, a partir do
	// endereço de quem lhe fala. É o que um nginx faz, e é o que se quer medir.
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(alvo))
	defer proxy.Close()

	// Uma máquina esgota o seu tecto...
	for i := range 2 {
		if estado := pedirPeloProxy(t, proxy.URL, "203.0.113.10"); estado != http.StatusOK {
			t.Fatalf("máquina A, pedido %d: estado %d", i+1, estado)
		}
	}
	if estado := pedirPeloProxy(t, proxy.URL, "203.0.113.10"); estado != http.StatusTooManyRequests {
		t.Errorf("a máquina A devia ter esgotado o tecto: estado %d", estado)
	}

	// ...e a outra continua a passar. É isto que o v1 não fazia.
	if estado := pedirPeloProxy(t, proxy.URL, "203.0.113.20"); estado != http.StatusOK {
		t.Errorf("a máquina B foi trancada pelo tecto da A: estado %d — é o bug do v1", estado)
	}
}

func TestUm429TrazRetryAfter(t *testing.T) {
	s := servidorComTecto(t, web.Tecto{Pedidos: 1, Janela: 90 * time.Second})

	pedirComXFF(t, s, "", "")

	pedido := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	if resposta.Code != http.StatusTooManyRequests {
		t.Fatalf("estado %d, esperava 429", resposta.Code)
	}
	if got := resposta.Header().Get("Retry-After"); got != "90" {
		t.Errorf("Retry-After = %q, e a janela é de 90 s", got)
	}
	// ⚠️ Um 429 leva o X-Request-ID como qualquer outra resposta: é a que quem
	// reporta o problema tem na mão.
	if resposta.Header().Get("X-Request-ID") == "" {
		t.Error("o 429 saiu sem X-Request-ID")
	}
}

// TestSemProxiesDeConfiancaODefaultEVazio: um serviço que arranca sem saber quem
// está à frente dele não deve acreditar em cabeçalhos.
func TestSemProxiesDeConfiancaODefaultEVazio(t *testing.T) {
	redes, err := web.RedesDe("")
	if err != nil {
		t.Fatalf("RedesDe(\"\"): %v", err)
	}
	if len(redes) != 0 {
		t.Errorf("o default traz %d redes de confiança, e devia ser vazio", len(redes))
	}
}

func TestUmaRedeDeConfiancaMalEscritaNaoPassa(t *testing.T) {
	for _, bruto := range []string{"10.0.0.0/33", "não-é-um-ip", "10.0.0.0/8, lixo"} {
		if _, err := web.RedesDe(bruto); err == nil {
			t.Errorf("%q foi aceite como rede de confiança", bruto)
		}
	}

	// ⚠️ Um endereço solto vale como /32, e não como «esta rede». O engano
	// contrário abria o cabeçalho a uma rede inteira sem dar erro nenhum.
	redes, err := web.RedesDe("192.168.1.5")
	if err != nil {
		t.Fatalf("RedesDe: %v", err)
	}
	if tamanho, _ := redes[0].Mask.Size(); tamanho != 32 {
		t.Errorf("192.168.1.5 virou uma rede /%d", tamanho)
	}
}

// TestOContadorEmBaixoNaoFechaOServico: um contador indisponível não pode tornar
// o serviço inacessível — a alternativa transformava uma avaria da base numa
// negação de serviço completa.
func TestOContadorEmBaixoNaoFechaOServico(t *testing.T) {
	s := servidor(t).ComTecto(contadorAvariado{}, web.Tecto{Pedidos: 1, Janela: time.Minute})

	for i := range 3 {
		if estado := pedirComXFF(t, s, "", ""); estado != http.StatusOK {
			t.Errorf("pedido %d: estado %d com o contador em baixo — o tecto falha ABERTO", i+1, estado)
		}
	}
}

// --- ajudantes ---------------------------------------------------------------------

// contadorEmMemoria conta por chave, como a tabela `limites` faz.
type contadorEmMemoria struct {
	mu      sync.Mutex
	janelas map[string]*janela
}

type janela struct {
	inicio   time.Time
	contagem int
}

func (c *contadorEmMemoria) ContarPedido(
	_ context.Context, chave string, duracao time.Duration, agora time.Time,
) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.janelas == nil {
		c.janelas = map[string]*janela{}
	}
	j, existe := c.janelas[chave]
	if !existe || !j.inicio.Add(duracao).After(agora) {
		c.janelas[chave] = &janela{inicio: agora, contagem: 1}
		return 1, nil
	}
	j.contagem++
	return j.contagem, nil
}

type contadorAvariado struct{}

func (contadorAvariado) ContarPedido(context.Context, string, time.Duration, time.Time) (int, error) {
	return 0, context.DeadlineExceeded
}

func servidorComTecto(t *testing.T, tecto web.Tecto) *web.Servidor {
	t.Helper()
	return servidor(t).ComTecto(&contadorEmMemoria{}, tecto)
}

func pedirComXFF(t *testing.T, s *web.Servidor, forjado, remoto string) int {
	t.Helper()
	pedido := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	if forjado != "" {
		pedido.Header.Set("X-Forwarded-For", forjado)
	}
	if remoto != "" {
		pedido.RemoteAddr = remoto + ":54321"
	}
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)
	return resposta.Code
}

// pedirPeloProxy faz o pedido ao proxy, dizendo-lhe de que máquina vem.
//
// ⚠️ O `X-Forwarded-For` que a app vê é o que o PROXY escreve — o nosso só entra
// na cadeia à esquerda do dele, que é exactamente a situação real: um cliente
// atrás de outro proxy, ou a tentar forjar.
func pedirPeloProxy(t *testing.T, urlDoProxy, maquina string) int {
	t.Helper()
	pedido, err := http.NewRequest(http.MethodGet, urlDoProxy+"/healthz", nil)
	if err != nil {
		t.Fatalf("montar o pedido: %v", err)
	}
	pedido.Header.Set("X-Forwarded-For", maquina)

	resposta, err := http.DefaultClient.Do(pedido)
	if err != nil {
		t.Fatalf("pedir pelo proxy: %v", err)
	}
	defer func() { _ = resposta.Body.Close() }()
	return resposta.StatusCode
}
