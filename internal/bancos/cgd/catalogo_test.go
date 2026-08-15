package cgd_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/cgd"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O catálogo de períodos da CGD deixou de ser um pedido por simulação (KAN-36).
//
// ⚠️ **O que se mede aqui são PEDIDOS À CGD, não tempo nem bytes.** Medido a
// 2026-08-14 nas capturas: um pedido com fase fixa custava 3 pedidos e 82 333 B,
// dos quais 75 291 (91,4 %) eram a página inicial — para catorze pares
// `ano → código`. Enquanto houve varrimento isto diluía-se por dezenas de pontos
// da grelha; ao vivo é por pessoa que pergunta.

// catalogoFalso é a loja de catálogos dos testes: em memória, e a contar.
type catalogoFalso struct {
	mu       sync.Mutex
	valores  map[string][]byte
	leituras int
	escritas int
}

func novoCatalogoFalso() *catalogoFalso {
	return &catalogoFalso{valores: map[string][]byte{}}
}

func (c *catalogoFalso) Ler(_ context.Context, bancoID, nome string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.leituras++
	v, ok := c.valores[bancoID+"/"+nome]
	return v, ok
}

func (c *catalogoFalso) Guardar(_ context.Context, bancoID, nome string, valor []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.escritas++
	c.valores[bancoID+"/"+nome] = valor
}

// bancoComCatalogo monta a CGD com uma loja de catálogos e um transporte que
// conta o que lhe pedem.
func bancoComCatalogo(t *testing.T, cat cgd.Catalogos) (*cgd.Banco, *transporte.Falso) {
	t.Helper()
	home := string(captura(t, "home.html"))
	limites := string(captura(t, "limites_propria.resposta.json"))
	calculo := string(captura(t, "fixa_10a.resposta.json"))

	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			switch p.URL.Path {
			case "/":
				return transporte.RespostaDeTexto(http.StatusOK, home), nil
			case "/limits":
				return transporte.RespostaDeTexto(http.StatusOK, limites), nil
			case "/calculate":
				return transporte.RespostaDeTexto(http.StatusOK, calculo), nil
			default:
				t.Errorf("pedido a um caminho que a CGD não tem: %q", p.URL.Path)
				return transporte.RespostaDeTexto(http.StatusNotFound, ""), nil
			}
		},
	}
	return cgd.Novo(falso, cat), falso
}

// pedidoComFaseFixa é o cenário das capturas em taxa fixa a 10 anos — o caso em
// que a CGD obriga a ler a página.
func pedidoComFaseFixa() dominio.Pedido {
	return comFixa(pedidoBase(), 10)
}

// pediuAPagina conta as idas a `/` — é a página de 73,5 KiB.
func pediuAPagina(f *transporte.Falso) int {
	n := 0
	for _, p := range f.Pedidos() {
		if p.URL.Path == "/" {
			n++
		}
	}
	return n
}

// TestOCatalogoGuardadoPoupaAPaginaÀCGD é a afirmação central da KAN-36.
//
// ⚠️ **Conta o pedido, e não o resultado.** Uma versão que só comparasse as
// ofertas passava com a página a ser pedida na mesma — e o que esta issue existe
// para tirar é o pedido.
func TestOCatalogoGuardadoPoupaAPaginaACGD(t *testing.T) {
	t.Parallel()

	loja := novoCatalogoFalso()
	p := pedidoComFaseFixa()

	// Primeira simulação: não há catálogo, vai-se à página.
	primeiro, tPrimeiro := bancoComCatalogo(t, loja)
	if _, err := primeiro.Simular(context.Background(), p); err != nil {
		t.Fatalf("primeira simulação: %v", err)
	}
	if n := pediuAPagina(tPrimeiro); n != 1 {
		t.Fatalf("a primeira simulação pediu a página %d vezes, esperava 1", n)
	}
	if loja.escritas != 1 {
		t.Fatalf("o catálogo lido não foi guardado (%d escritas)", loja.escritas)
	}

	// Segunda simulação, banco novo — como um segundo cliente, noutro processo.
	segundo, tSegundo := bancoComCatalogo(t, loja)
	if _, err := segundo.Simular(context.Background(), p); err != nil {
		t.Fatalf("segunda simulação: %v", err)
	}
	if n := pediuAPagina(tSegundo); n != 0 {
		t.Errorf("a segunda simulação foi buscar a página %d vezes: o catálogo guardado não a poupou", n)
	}
	// E continua a fazer o resto: os limites e o cálculo não se poupam.
	if n := len(tSegundo.Pedidos()); n != 2 {
		t.Errorf("a segunda simulação fez %d pedidos, esperava 2 (/limits e /calculate)", n)
	}
}

// TestSemLojaDeCatalogosAPaginaContinuaAVirDaCGD: a loja nula desliga, e é o que
// mantém este banco afirmável sem base de dados nenhuma.
func TestSemLojaDeCatalogosAPaginaContinuaAVirDaCGD(t *testing.T) {
	t.Parallel()

	banco, falso := bancoComCatalogo(t, nil)
	if _, err := banco.Simular(context.Background(), pedidoComFaseFixa()); err != nil {
		t.Fatalf("simular: %v", err)
	}
	if n := pediuAPagina(falso); n != 1 {
		t.Errorf("com loja nula pediu a página %d vezes, esperava 1", n)
	}
}

// TestUmCatalogoDeRecursoNaoSeGuarda.
//
// ⚠️ **É a guarda que impede o defeito de se tornar permanente.** Se a página não
// se deixar ler, usa-se a lista conhecida — e a oferta di-lo. Guardar essa lista
// fazia um soluço de segundos virar «os períodos do ano passado» durante a
// validade inteira, **e calava o aviso**, porque a leitura seguinte encontrava
// catálogo e não anotava nada.
func TestUmCatalogoDeRecursoNaoSeGuarda(t *testing.T) {
	t.Parallel()

	loja := novoCatalogoFalso()
	limites := string(captura(t, "limites_propria.resposta.json"))
	calculo := string(captura(t, "fixa_10a.resposta.json"))

	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			switch p.URL.Path {
			case "/": // a página não se deixa ler
				return transporte.RespostaDeTexto(http.StatusInternalServerError, ""), nil
			case "/limits":
				return transporte.RespostaDeTexto(http.StatusOK, limites), nil
			default:
				return transporte.RespostaDeTexto(http.StatusOK, calculo), nil
			}
		},
	}

	banco := cgd.Novo(falso, loja)
	oferta, err := banco.Simular(context.Background(), pedidoComFaseFixa())
	if err != nil {
		t.Fatalf("simular: %v", err)
	}

	if loja.escritas != 0 {
		t.Errorf("guardou-se um catálogo de recurso (%d escritas): o próximo pedido serviria "+
			"os períodos conhecidos como se fossem os de hoje, e sem aviso", loja.escritas)
	}
	// E o aviso continua a sair, que é o que diz a quem lê que os períodos podem
	// não ser os que a CGD vende agora.
	if len(oferta.Notas()) == 0 {
		t.Error("a oferta não avisa que os períodos não são os que a CGD publica hoje")
	}
}
