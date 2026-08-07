package web_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/cache"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// TestDoisPedidosIguaisCustamUmSoPedidoAoBanco é o critério de pronto da KAN-58,
// e a razão de a cache existir: somos um amplificador, e um pedido nosso vira
// ~10 aos bancos.
//
// ⚠️ Reverter — não ligar a cache, ou gravar depois de ler — faz este teste dizer
// quantos pedidos a mais saíram para o banco.
func TestDoisPedidosIguaisCustamUmSoPedidoAoBanco(t *testing.T) {
	var perguntas atomic.Int32
	s := servidorComCache(t, &perguntas, &cacheFalsa{})

	primeira := pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if primeira.Code != http.StatusOK {
		t.Fatalf("a primeira devia ter servido: %d %s", primeira.Code, primeira.Body.String())
	}
	segunda := pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if segunda.Code != http.StatusOK {
		t.Fatalf("a segunda devia ter servido: %d %s", segunda.Code, segunda.Body.String())
	}

	if n := perguntas.Load(); n != 1 {
		t.Errorf("o mesmo pedido custou %d idas ao banco, esperava 1", n)
	}
}

// TestUmPedidoDiferenteNaoAproveitaOAcertoDoOutro: a chave é o pedido exacto, e
// um cêntimo de diferença é outro pedido.
//
// ⚠️ É o outro lado do teste acima, e sem ele uma cache que devolvesse sempre a
// mesma resposta passava o primeiro. Um acerto a mais é o único erro grave que
// esta cache pode causar — serve um preço que não é o daquele pedido.
func TestUmPedidoDiferenteNaoAproveitaOAcertoDoOutro(t *testing.T) {
	var perguntas atomic.Int32
	s := servidorComCache(t, &perguntas, &cacheFalsa{})

	pedirOferta(t, s, "cgd", corpoDeOferta(nil))

	outro := corpoDeOferta(nil)
	outro.Pedido.Montante += 0.01
	pedirOferta(t, s, "cgd", outro)

	if n := perguntas.Load(); n != 2 {
		t.Errorf("dois pedidos diferentes custaram %d idas ao banco, esperava 2", n)
	}
}

// TestUmAcertoNaoGastaVagaDoBanco: a cache lê-se ANTES da lotação.
//
// ⚠️ **É a ordem que faz a cache valer alguma coisa.** Se o acerto gastasse uma
// das duas vagas, dez clientes com o mesmo pedido continuavam a fazer fila uns
// pelos outros — e a receber `503 banco_ocupado` — para uma resposta que já
// estava em Postgres. Reverter a ordem faz este teste contar a vaga gasta.
func TestUmAcertoNaoGastaVagaDoBanco(t *testing.T) {
	var perguntas atomic.Int32
	lot := &lotacaoFalsa{}
	s := servidorComCache(t, &perguntas, &cacheFalsa{}).ComLotacao(lot)

	pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if n := lot.saidas.Load(); n != 1 {
		t.Fatalf("a primeira devia ter gasto uma vaga, gastou %d", n)
	}

	pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if n := lot.saidas.Load(); n != 1 {
		t.Errorf("o acerto gastou vaga: %d ao todo, esperava continuar em 1", n)
	}
}

// TestUmaCacheAvariadaNaoDerrubaOPedido: a cache falha ABERTO.
//
// ⚠️ É o contrário da lotação, que falha fechado, e a assimetria é deliberada:
// ali o tecto é a última coisa entre nós e o simulador de um terceiro; aqui o
// tecto continua de pé com a cache em baixo — perde-se a poupança de pedidos,
// não a protecção. Reverter para falhar fechado faz este teste dizer que uma
// avaria nossa virou indisponibilidade.
func TestUmaCacheAvariadaNaoDerrubaOPedido(t *testing.T) {
	var perguntas atomic.Int32
	avariada := &cacheFalsa{erro: errors.New("a base não responde")}
	s := servidorComCache(t, &perguntas, avariada)

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d, esperava 200: uma cache avariada não pode fechar o caminho: %s",
			resposta.Code, resposta.Body.String())
	}
	if n := perguntas.Load(); n != 1 {
		t.Errorf("com a cache avariada foi-se ao banco %d vezes, esperava 1", n)
	}
}

// TestOAcertoDizEmCacheEMantemOCapturadoEmDoBanco: um acerto distingue-se de uma
// resposta fresca, e não mente sobre a idade do preço.
//
// ⚠️ O `capturado_em` de um acerto é o instante em que se FALOU com o banco, e
// não o de agora. Reverter para carimbar o instante da leitura faz este teste
// dizer que um preço de há minutos se apresentou como acabado de cotar.
func TestOAcertoDizEmCacheEMantemOCapturadoEmDoBanco(t *testing.T) {
	var perguntas atomic.Int32
	s := servidorComCache(t, &perguntas, &cacheFalsa{})

	var fresca api.Oferta
	lerJSON(t, pedirOferta(t, s, "cgd", corpoDeOferta(nil)), &fresca)
	if fresca.EmCache != nil && *fresca.EmCache {
		t.Error("a primeira resposta veio do banco e diz em_cache=true")
	}
	if fresca.CapturadoEm == nil {
		t.Fatal("uma resposta servida tem de dizer de quando é o preço")
	}

	var acerto api.Oferta
	lerJSON(t, pedirOferta(t, s, "cgd", corpoDeOferta(nil)), &acerto)
	if acerto.EmCache == nil || !*acerto.EmCache {
		t.Error("o acerto não se distingue de uma resposta fresca")
	}
	if acerto.CapturadoEm == nil || !acerto.CapturadoEm.Equal(*fresca.CapturadoEm) {
		t.Errorf("o capturado_em do acerto é %v, esperava o do banco (%v)",
			acerto.CapturadoEm, fresca.CapturadoEm)
	}
}

// TestUmCacheValidadeIlegivelFalhaOArranque, pela mesma razão das vagas — com uma
// excepção que é decisão e não descuido: o zero passa, e desliga a cache.
func TestUmCacheValidadeIlegivelFalhaOArranque(t *testing.T) {
	for _, bruto := range []string{"cinco minutos", "5", "-1s", "5x"} {
		if validade, err := web.ValidadeDeCacheDe(bruto); err == nil {
			t.Errorf("CACHE_VALIDADE=%q passou e valeu %s", bruto, validade)
		}
	}

	validade, err := web.ValidadeDeCacheDe("")
	if err != nil {
		t.Fatalf("vazio devia valer o de omissão: %v", err)
	}
	if validade != cache.ValidadeOmissao {
		t.Errorf("vazio valeu %s, esperava o de omissão (%s)", validade, cache.ValidadeOmissao)
	}

	// ⚠️ Zero é legítimo: desligar a cache é uma coisa que alguém pode querer
	// mesmo — para medir a validade, por exemplo — e tem de se poder dizer sem
	// editar código.
	if _, err := web.ValidadeDeCacheDe("0s"); err != nil {
		t.Errorf("CACHE_VALIDADE=0s devia passar e desligar a cache: %v", err)
	}
}

// servidorComCache monta o caminho ao vivo com um banco que conta quantas vezes
// lhe perguntaram, e com a cache dada — chaveada pela derivação a sério, que é o
// que liga estes testes ao `infra/cache`.
func servidorComCache(t *testing.T, perguntas *atomic.Int32, c web.Cache) *web.Servidor {
	t.Helper()
	tan := taxa(t, "3.250")
	return servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			perguntas.Add(1)
			return dominio.Oferta{TAN: &tan}, nil
		},
	}).ComCache(c, cache.Chave)
}

// cacheFalsa é a cache em memória — o que se mede aqui é o caminho, e não o
// Postgres. Com `erro`, avaria-se nas duas pontas.
type cacheFalsa struct {
	erro error

	mu       sync.Mutex
	guardado map[string]api.Oferta
}

func (c *cacheFalsa) Ler(_ context.Context, chave string, _ time.Time) (api.Oferta, bool, error) {
	if c.erro != nil {
		return api.Oferta{}, false, c.erro
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	oferta, houve := c.guardado[chave]
	if !houve {
		return api.Oferta{}, false, nil
	}
	emCache := true
	oferta.EmCache = &emCache
	return oferta, true, nil
}

func (c *cacheFalsa) Gravar(
	_ context.Context, chave, _ string, oferta api.Oferta, _ time.Time,
) error {
	if c.erro != nil {
		return c.erro
	}
	if !oferta.Sucesso {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.guardado == nil {
		c.guardado = map[string]api.Oferta{}
	}
	oferta.EmCache = nil
	c.guardado[chave] = oferta
	return nil
}
