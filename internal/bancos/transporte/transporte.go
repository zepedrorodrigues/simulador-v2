// Package transporte tem as estratégias com que se fala com o simulador de um
// banco. São quatro porque o v1 provou que são precisas quatro; estão aqui as
// duas de HTTP (fases 1 e 2) e as duas de browser ficam para a fase 3, quando a
// biblioteca estiver decidida (KAN-20).
//
// ⚠️ O transporte é injectado, nunca instanciado dentro do banco. É isso que o
// torna substituível por um Falso nos testes, e é a diferença estrutural face ao
// v1 — onde cada scraper reimplementava a sua variante de "abrir sessão / fixar
// cookies / cunhar token", as variantes nunca convergiram, e os quatro bancos de
// browser não tinham forma nenhuma de ser testados sem rede.
package transporte

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
)

// HTTPSimples é um ou mais pedidos HTTP sem estado: CGD, Novo Banco, Banco CTT,
// Crédito Agrícola e Santander.
type HTTPSimples interface {
	// Fazer executa o pedido dentro do prazo de ctx.
	//
	// ⚠️ Quem chama fecha o corpo da resposta, como no net/http.
	Fazer(ctx context.Context, req *http.Request) (*http.Response, error)
}

// Sessao é o que o pedido de arranque deixou para o pedido real.
//
// Os cookies não passam por aqui — ficam no jar do cliente, que é o mesmo que
// vai fazer o POST. O HTML volta em bruto de propósito: quem sabe o que dele
// extrair é o banco, e essa leitura é uma função pura, testável contra uma
// captura sem tocar na rede.
type Sessao struct {
	HTML []byte
}

// HTTPComSessao é o Montepio: um GET de arranque que fixa os cookies e traz o
// HTML de onde sai o HashRequest, e só depois o POST. Sem o arranque, o gateway
// responde 410 «New open window with different context».
//
// ⚠️ Estende HTTPSimples de propósito: o pedido real tem de sair do mesmo
// cliente, senão vai sem os cookies que o arranque fixou. Duas interfaces
// separadas obrigariam a partilhar um jar entre elas, que é a mesma coisa dita
// de forma que se pode esquecer.
type HTTPComSessao interface {
	HTTPSimples

	// Arrancar faz o GET inicial: fixa os cookies no jar e devolve o corpo.
	Arrancar(ctx context.Context, url string) (Sessao, error)
}

// LimiteCorpoArranque é o tecto do que se lê do HTML de arranque. Um simulador
// que responda um corpo sem fim não enche a memória do processo por isso.
const LimiteCorpoArranque = 4 << 20 // 4 MiB

// Cliente implementa HTTPSimples sobre um *http.Client.
type Cliente struct {
	http *http.Client
}

// NovoCliente embrulha um *http.Client. Nulo dá um cliente com o transporte por
// omissão e **sem** Timeout próprio: o prazo é o do ctx, imposto por banco pelo
// orquestrador, e um Timeout do cliente por cima tornaria o prazo efectivo o
// menor dos dois — difícil de explicar quando um banco desiste antes de tempo.
func NovoCliente(base *http.Client) *Cliente {
	if base == nil {
		return &Cliente{http: &http.Client{}}
	}
	copia := *base
	return &Cliente{http: &copia}
}

// Fazer executa o pedido com o prazo de ctx.
func (c *Cliente) Fazer(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("pedido %s %s: %w", req.Method, req.URL.Redacted(), err)
	}
	return resp, nil
}

// ClienteComSessao implementa HTTPComSessao.
//
// ⚠️ Tem jar de cookies próprio, e por isso não se partilha entre simulações: os
// cookies de sessão de um pedido não têm nada que ir no pedido de outra pessoa.
// Constrói-se um por simulação — é barato, e o contrário é um erro difícil de
// ver.
type ClienteComSessao struct {
	http *http.Client
}

// NovoClienteComSessao monta um cliente com jar de cookies. base serve para
// injectar o que a infra configurar (proxy, TLS, cabeçalhos por omissão no
// transporte); o jar é sempre novo e é sempre o desta sessão.
func NovoClienteComSessao(base *http.Client) (*ClienteComSessao, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("jar de cookies: %w", err)
	}
	c := &http.Client{}
	if base != nil {
		copia := *base
		c = &copia
	}
	c.Jar = jar
	return &ClienteComSessao{http: c}, nil
}

// Fazer executa o pedido com o prazo de ctx, com os cookies que o arranque fixou.
func (c *ClienteComSessao) Fazer(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("pedido %s %s: %w", req.Method, req.URL.Redacted(), err)
	}
	return resp, nil
}

// Arrancar faz o GET que fixa os cookies e devolve o corpo para o banco o ler.
//
// Um estado que não seja 2xx é erro aqui e não mais à frente: sem o arranque, o
// pedido real responde uma coisa que não se parece com uma falha de arranque —
// no Montepio, um 410 com uma mensagem sobre janelas.
func (c *ClienteComSessao) Arrancar(ctx context.Context, url string) (Sessao, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Sessao{}, fmt.Errorf("arranque em %s: %w", url, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Sessao{}, fmt.Errorf("arranque em %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Sessao{}, fmt.Errorf("arranque em %s respondeu %s", url, resp.Status)
	}

	html, err := io.ReadAll(io.LimitReader(resp.Body, LimiteCorpoArranque))
	if err != nil {
		return Sessao{}, fmt.Errorf("arranque em %s: ler o corpo: %w", url, err)
	}
	return Sessao{HTML: html}, nil
}

var (
	_ HTTPSimples   = (*Cliente)(nil)
	_ HTTPComSessao = (*ClienteComSessao)(nil)
)

// BrowserComoCliente é o Bankinter (Cloudflare) e o BPI (formulário OutSystems).
// O browser abre a página, executa o callback que conduz o formulário, e devolve
// o HTML da página como resultado.
//
// ⚠️ Este é o transporte mais caro e lento: cada simulação arranca um browser
// headless. A optimização passa por reaproveitar o browser entre simulações
// quando possível — mas isso é detalhe da infra, não do banco.
type BrowserComoCliente interface {
	// Executar abre o browser, navega para url, executa o callback, e devolve
	// o HTML da página no fim.
	//
	// O callback recebe um context.Context e uma referência opaca à página.
	// O banco usa essa referência para interagir com a página (preencher
	// formulários, clicar, esperar por elementos).
	//
	// ⚠️ O callback é chamado dentro do prazo de ctx. Um banco que ignore o
	// cancelamento segura o varrimento inteiro.
	Executar(ctx context.Context, url, userAgent string, callback func(ctx context.Context, page interface{}) error) (string, error)
}

// BrowserParaCredencial é o ActivoBank e o Millennium BCP: o browser abre
// a página apenas para cunhar um token OAuth, e a simulação é HTTP.
type BrowserParaCredencial interface {
	// CunharToken abre o browser, navega para url, e devolve os headers
	// de autenticação que o browser extraiu da página.
	//
	// ⚠️ O token é válido por um tempo limitado. A infra decide quando
	// revalidar.
	CunharToken(ctx context.Context, url, userAgent string) (http.Header, error)
}
