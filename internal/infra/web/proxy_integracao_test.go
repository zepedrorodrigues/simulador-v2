package web_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// O tecto por IP medido contra um proxy A SÉRIO — Caddy num contentor, a
// reescrever o `X-Forwarded-For` como um proxy de produção o faz.
//
// ⚠️ **Porque é que isto existe ao lado do `tecto_test.go`, que já usa um
// `httputil.ReverseProxy`:** aquele prova que a leitura da cadeia funciona, mas
// distingue as «duas máquinas» por um `X-Forwarded-For` que o próprio cliente
// escreve. Nessa montagem um cliente honesto e um falsificador são
// indistinguíveis — o que faltava medir é o que acontece a quem **forja o
// cabeçalho atrás de um proxy de confiança**, e é o caso que interessa, porque é
// o único em que o tecto se contorna sem se notar.
//
// ⚠️ E foi a interacção com um proxy REAL que partiu no v1, não a leitura de um
// cabeçalho. Um proxy escrito em Go e chamado do mesmo processo não pode
// contradizer o que o Go acha que um proxy faz.

// TestAtrasDeUmProxyASerioUmClienteQueForjaOCabecalhoNaoEscapaAoTecto é o
// primeiro critério da KAN-14 visto onde ele ainda não estava: **através** de um
// proxy de confiança, e não ao lado dele.
//
// O Caddy acrescenta ao `X-Forwarded-For` o endereço de quem lhe falou —
// medido: um cliente que manda `203.0.113.10` faz chegar cá
// `"203.0.113.10, 172.17.0.1"`. Como o endereço atestado pelo proxy fica à
// DIREITA do que o cliente escreveu, e a leitura anda da direita para a
// esquerda, o que o cliente inventou nunca é alcançado. Reverter a leitura para
// a esquerda — pegar no primeiro da lista — faz este teste dizer que o cliente
// escolheu o seu próprio identificador e escapou.
func TestAtrasDeUmProxyASerioUmClienteQueForjaOCabecalhoNaoEscapaAoTecto(t *testing.T) {
	// ⚠️ Confia-se SÓ no salto do lado da aplicação. O endereço que o Caddy
	// acrescenta é então o primeiro não-confiável a contar da direita — logo é
	// ele o cliente, e é atestado pelo proxy em vez de declarado por quem pede.
	redes, err := web.RedesDe("127.0.0.0/8")
	if err != nil {
		t.Fatalf("RedesDe: %v", err)
	}
	const tecto = 3
	proxy := subirProxy(t, web.Tecto{Pedidos: tecto, Janela: time.Minute, ProxiesDeConfianca: redes})

	// O cliente gasta o tecto sem forjar nada.
	for i := range tecto {
		if estado := pedirPeloProxyReal(t, proxy, ""); estado != http.StatusOK {
			t.Fatalf("pedido %d: estado %d", i+1, estado)
		}
	}
	if estado := pedirPeloProxyReal(t, proxy, ""); estado != http.StatusTooManyRequests {
		t.Fatalf("o tecto não fechou ao fim de %d pedidos: estado %d", tecto, estado)
	}

	// E agora tenta escapar, inventando um endereço diferente a cada pedido.
	for _, forjado := range []string{"9.9.9.9", "203.0.113.77", "8.8.8.8, 1.1.1.1"} {
		if estado := pedirPeloProxyReal(t, proxy, forjado); estado != http.StatusTooManyRequests {
			t.Errorf("com X-Forwarded-For: %q o cliente escapou ao tecto atrás do proxy (estado %d)",
				forjado, estado)
		}
	}
}

// TestAtrasDeUmProxyASerioCadaMaquinaTemOSeuTecto é o segundo critério — o
// isolamento —, e é o bug de produção do v1: o tecto por IP a virar o tecto do
// site inteiro porque toda a gente aparecia com o endereço do proxy.
//
// ⚠️ **Aqui confiam-se DOIS saltos**, e é a diferença para o teste acima: é a
// montagem de quem tem um CDN à frente do seu próprio proxy. Com o salto do
// Caddy também de confiança, o endereço que fica a valer é o que vem antes dele
// — que num CDN é o cliente, atestado pela borda.
func TestAtrasDeUmProxyASerioCadaMaquinaTemOSeuTecto(t *testing.T) {
	// De onde é que o Caddy nos fala? Descobre-se, não se adivinha — o endereço
	// depende da rede que o Docker montou nesta máquina.
	bordaDoCaddy := enderecoDeQuemFalaAoServico(t)

	redes, err := web.RedesDe("127.0.0.0/8, " + bordaDoCaddy)
	if err != nil {
		t.Fatalf("RedesDe: %v", err)
	}
	const tecto = 2
	proxy := subirProxy(t, web.Tecto{Pedidos: tecto, Janela: time.Minute, ProxiesDeConfianca: redes})

	// Uma máquina esgota o seu tecto...
	for i := range tecto {
		if estado := pedirPeloProxyReal(t, proxy, "203.0.113.10"); estado != http.StatusOK {
			t.Fatalf("máquina A, pedido %d: estado %d", i+1, estado)
		}
	}
	if estado := pedirPeloProxyReal(t, proxy, "203.0.113.10"); estado != http.StatusTooManyRequests {
		t.Errorf("a máquina A devia ter esgotado o tecto: estado %d", estado)
	}

	// ...e a outra continua a passar. É isto que o v1 não fazia.
	if estado := pedirPeloProxyReal(t, proxy, "203.0.113.20"); estado != http.StatusOK {
		t.Errorf("a máquina B foi trancada pelo tecto da A: estado %d — é o bug do v1", estado)
	}
}

// --- a montagem --------------------------------------------------------------------

// enderecoDeQuemFalaAoServico devolve o endereço com que o Caddy chega ao
// serviço, lido do `X-Forwarded-For` que ele próprio escreve.
//
// ⚠️ Mede-se em vez de se escrever à mão: a rede do Docker muda de máquina para
// máquina, e um `172.17.0.1` fixo passava aqui e falhava no CI sem dizer porquê.
func enderecoDeQuemFalaAoServico(t *testing.T) string {
	t.Helper()

	var visto string
	sonda := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		visto = r.Header.Get("X-Forwarded-For")
	})
	proxy := subirProxyPara(t, sonda)

	resposta, err := http.Get(proxy + "/sonda") //nolint:noctx // o teste tem o seu prazo
	if err != nil {
		t.Fatalf("sondar o proxy: %v", err)
	}
	_ = resposta.Body.Close()

	if visto == "" {
		t.Fatal("o Caddy não pôs X-Forwarded-For: sem ele não há nada a medir aqui")
	}
	partes := strings.Split(visto, ",")
	return strings.TrimSpace(partes[len(partes)-1])
}

// subirProxy monta o serviço com o tecto dado e um Caddy à frente. Devolve o URL
// do Caddy — os pedidos entram por lá, como em produção.
func subirProxy(t *testing.T, tecto web.Tecto) string {
	t.Helper()
	return subirProxyPara(t, servidorComTectoDado(t, tecto).Rotas())
}

func servidorComTectoDado(t *testing.T, tecto web.Tecto) *web.Servidor {
	t.Helper()
	return servidor(t).ComTecto(&contadorEmMemoria{}, tecto)
}

// subirProxyPara põe um Caddy a sério à frente do handler dado.
//
// ⚠️ O serviço escuta em 127.0.0.1 e o contentor chega lá pelo encaminhamento do
// testcontainers (`host.testcontainers.internal`), que é um túnel SSH — por isso
// o serviço vê a ligação vir de 127.0.0.1, e é essa a rede que os testes
// declaram como o salto de confiança do lado da aplicação.
func subirProxyPara(t *testing.T, handler http.Handler) string {
	t.Helper()
	ctx := context.Background()

	servico := httptest.NewServer(handler)
	t.Cleanup(servico.Close)

	porta := portaDe(t, servico.URL)

	// `auto_https off` porque isto fala HTTP em claro; `admin off` porque não há
	// ninguém para administrar um proxy que vive o que um teste dura.
	//
	// ⚠️ **O `trusted_proxies` está aqui por uma razão medida, e não por gosto.**
	// Por omissão o Caddy 2 **substitui** o `X-Forwarded-For` pelo endereço de
	// quem lhe falou, deitando fora o que o cliente tenha escrito — medido nesta
	// montagem a 2026-08-07: com o cliente a mandar `203.0.113.10`, o serviço
	// recebeu `172.17.0.1` e mais nada. É bom comportamento, e não é o que se quer
	// medir aqui: com ele, quem defende contra a falsificação é o Caddy, e o nosso
	// código nunca chega a ser exercitado.
	//
	// Declarando a rede do Docker como de confiança, o Caddy passa a **acrescentar**
	// — que é o que um nginx faz com `$proxy_add_x_forwarded_for`, e é a montagem
	// em que a cadeia chega cá com mais do que uma entrada. É aí que a leitura da
	// direita para a esquerda tem de estar certa, e é isso que estes testes medem.
	caddyfile := fmt.Sprintf(`{
	admin off
	auto_https off
	servers {
		trusted_proxies static 172.16.0.0/12 10.0.0.0/8 192.168.0.0/16
	}
}
:80 {
	reverse_proxy %s:%d
}
`, testcontainers.HostInternal, porta)

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:           "caddy:2-alpine",
			ExposedPorts:    []string{"80/tcp"},
			HostAccessPorts: []int{porta},
			Files: []testcontainers.ContainerFile{{
				Reader:            strings.NewReader(caddyfile),
				ContainerFilePath: "/etc/caddy/Caddyfile",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForListeningPort("80/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("subir o Caddy (Docker a correr?): %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	endereco, err := ctr.PortEndpoint(ctx, "80/tcp", "http")
	if err != nil {
		t.Fatalf("endereço do Caddy: %v", err)
	}
	return endereco
}

func portaDe(t *testing.T, bruto string) int {
	t.Helper()
	sem := strings.TrimPrefix(bruto, "http://")
	_, porta, err := net.SplitHostPort(sem)
	if err != nil {
		t.Fatalf("porta de %q: %v", bruto, err)
	}
	var n int
	if _, err := fmt.Sscanf(porta, "%d", &n); err != nil {
		t.Fatalf("porta %q não é um número: %v", porta, err)
	}
	return n
}

// pedirPeloProxyReal faz um pedido que entra pelo Caddy. Com `forjado`, o cliente
// tenta declarar quem é.
func pedirPeloProxyReal(t *testing.T, urlDoProxy, forjado string) int {
	t.Helper()
	pedido, err := http.NewRequestWithContext(
		t.Context(), http.MethodGet, urlDoProxy+"/healthz", nil)
	if err != nil {
		t.Fatalf("montar o pedido: %v", err)
	}
	if forjado != "" {
		pedido.Header.Set("X-Forwarded-For", forjado)
	}

	resposta, err := http.DefaultClient.Do(pedido)
	if err != nil {
		t.Fatalf("pedir pelo proxy: %v", err)
	}
	defer func() { _ = resposta.Body.Close() }()
	return resposta.StatusCode
}
