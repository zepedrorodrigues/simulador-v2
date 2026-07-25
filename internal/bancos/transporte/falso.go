package transporte

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// PedidoGravado é o que o Falso viu passar.
//
// ⚠️ O corpo vem já lido, e não o *http.Request. Ler o corpo de um pedido
// esvazia-o, e um teste que queira afirmar sobre o payload não pode depender de
// ser o primeiro a lê-lo — no v1 essa armadilha aparecia como "o payload está
// vazio" em testes que só se atrapalhavam uns aos outros.
type PedidoGravado struct {
	Metodo     string
	URL        *url.URL
	Cabecalhos http.Header
	Corpo      []byte
}

// Falso é o transporte dos testes: responde o que lhe programaram, grava o que
// lhe pediram, e não toca na rede.
//
// Vive aqui e não num ficheiro _test.go porque quem precisa dele são os testes
// de cada banco, que estão noutro pacote — é a mesma razão por que a stdlib tem
// o net/http/httptest e não ajudantes escondidos dentro do net/http.
//
// É seguro para uso concorrente: o portão corre com -race e o modelo é fan-out.
type Falso struct {
	// Estado e Corpo são a resposta do caso comum — o banco que devolve sempre a
	// mesma captura. Estado zero vale 200.
	Estado int
	Corpo  string

	// Responder, quando não é nulo, manda sobre Estado e Corpo: é para os testes
	// que precisam de responder em função do que foi pedido, ou de responder
	// diferente da segunda vez.
	Responder func(PedidoGravado) (*http.Response, error)

	// HTMLDeArranque é o corpo que Arrancar devolve.
	HTMLDeArranque []byte

	// ErroDeArranque, quando não é nulo, é o que Arrancar devolve em vez da
	// sessão — é assim que se ensaia o 410 do Montepio sem servidor nenhum.
	ErroDeArranque error

	// Atraso é o tempo que cada chamada demora a responder, e é ele que torna
	// mensurável um banco que ignore o ctx: o transporte desiste quando o prazo
	// acaba, e um banco que não passe o ctx fica cá dentro o Atraso inteiro.
	Atraso time.Duration

	mu        sync.Mutex
	pedidos   []PedidoGravado
	arranques []string
}

// Fazer grava o pedido e devolve a resposta programada.
//
// O pedido é gravado antes de se esperar pelo Atraso: um teste sobre um banco
// que desistiu a meio quer poder afirmar o que ele chegou a tentar.
func (f *Falso) Fazer(ctx context.Context, req *http.Request) (*http.Response, error) {
	gravado, err := gravar(req)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.pedidos = append(f.pedidos, gravado)
	responder, estado, corpo := f.Responder, f.Estado, f.Corpo
	f.mu.Unlock()

	if err := f.esperar(ctx); err != nil {
		return nil, err
	}
	if responder != nil {
		return responder(gravado)
	}
	if estado == 0 {
		estado = http.StatusOK
	}
	return RespostaDeTexto(estado, corpo), nil
}

// Arrancar grava a URL do arranque e devolve o HTML programado.
func (f *Falso) Arrancar(ctx context.Context, url string) (Sessao, error) {
	f.mu.Lock()
	f.arranques = append(f.arranques, url)
	html, erroDeArranque := f.HTMLDeArranque, f.ErroDeArranque
	f.mu.Unlock()

	if err := f.esperar(ctx); err != nil {
		return Sessao{}, err
	}
	if erroDeArranque != nil {
		return Sessao{}, erroDeArranque
	}
	return Sessao{HTML: html}, nil
}

// Pedidos devolve os pedidos gravados, pela ordem em que chegaram.
func (f *Falso) Pedidos() []PedidoGravado {
	f.mu.Lock()
	defer f.mu.Unlock()
	saida := make([]PedidoGravado, len(f.pedidos))
	copy(saida, f.pedidos)
	return saida
}

// Arranques devolve as URLs por que se arrancou, pela ordem em que chegaram.
func (f *Falso) Arranques() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	saida := make([]string, len(f.arranques))
	copy(saida, f.arranques)
	return saida
}

// esperar cumpre o Atraso, ou desiste com o ctx — o que vier primeiro. Sem
// Atraso, ainda assim respeita um ctx já cancelado, como faz o net/http.
func (f *Falso) esperar(ctx context.Context) error {
	if f.Atraso <= 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("transporte falso: %w", err)
		}
		return nil
	}

	temporizador := time.NewTimer(f.Atraso)
	defer temporizador.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("transporte falso: %w", ctx.Err())
	case <-temporizador.C:
		return nil
	}
}

func gravar(req *http.Request) (PedidoGravado, error) {
	g := PedidoGravado{
		Metodo:     req.Method,
		URL:        req.URL,
		Cabecalhos: req.Header.Clone(),
	}
	if req.Body == nil {
		return g, nil
	}

	corpo, err := io.ReadAll(req.Body)
	if err != nil {
		return PedidoGravado{}, fmt.Errorf("transporte falso: ler o corpo do pedido: %w", err)
	}
	_ = req.Body.Close()
	// O corpo volta a estar por ler, para quem receba este pedido depois.
	req.Body = io.NopCloser(bytes.NewReader(corpo))
	g.Corpo = corpo
	return g, nil
}

// RespostaDeTexto monta uma resposta com corpo. É o que a maior parte dos testes
// de um banco precisa: o JSON ou o HTML que a captura gravou.
func RespostaDeTexto(estado int, corpo string) *http.Response {
	return &http.Response{
		Status:     fmt.Sprintf("%d %s", estado, http.StatusText(estado)),
		StatusCode: estado,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(corpo)),
	}
}

var (
	_ HTTPSimples   = (*Falso)(nil)
	_ HTTPComSessao = (*Falso)(nil)
)
