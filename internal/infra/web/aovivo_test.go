package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// O `POST /api/v1/ofertas/{banco}` — o caminho do cliente desde a reversão da §1
// (KAN-7), à fronteira e **sem rede**.
//
// ⚠️ Sem rede é condição, não comodidade: os bancos que este endpoint interroga
// são simuladores públicos de terceiros, e um portão que lhes falasse somava aos
// ~10 pedidos por comparação que a §7 conta — a cada corrida, de cada máquina. É
// o `ComAoVivo` que troca quem se interroga.

// TestUmaOfertaAoVivoEServidaComoBoa é a afirmação de ponta a ponta deste
// caminho, e a que apanhou o defeito.
//
// ⚠️ **O `aovivo.Pedir` não carimbava o `CapturadoEm`.** O varrimento carimbava,
// o `dominio.Oferta` diz que é a camada de aplicação que o faz, e o caminho ao
// vivo saltava-o — com o efeito de a fronteira recusar servir **todas** as
// ofertas, porque um preço sem data apresenta-se como se fosse de agora. Reverter
// o carimbo faz este teste dizer exactamente isso.
func TestUmaOfertaAoVivoEServidaComoBoa(t *testing.T) {
	tan := taxa(t, "3.250")
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			return dominio.Oferta{TAN: &tan}, nil
		},
	})

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d: %s", resposta.Code, resposta.Body.String())
	}

	var o api.Oferta
	lerJSON(t, resposta, &o)
	if !o.Sucesso {
		t.Fatalf("o banco respondeu com preço e a oferta saiu em falha: %+v", o.Erro)
	}
	if o.Tan == nil || *o.Tan != 3.25 {
		t.Errorf("TAN servida %v, o banco respondeu %s", o.Tan, tan)
	}
	if o.CapturadoEm == nil {
		t.Fatal("a oferta não diz quando se falou com o banco")
	}
	// ⚠️ Ao vivo o `capturado_em` é o instante da conversa com o banco, e não o de
	// um varrimento (`API.md` §1). É a distância entre este instante e o `agora`
	// que diz a idade do preço — e aqui ela é zero, que é o ponto do caminho novo.
	if !o.CapturadoEm.Equal(relogio()) {
		t.Errorf("capturado em %s e falou-se com o banco em %s", o.CapturadoEm, relogio())
	}
}

// ⚠️ **Havia aqui um `TestUmaOfertaAoVivoNaoPrecisaDeVarrimento`**, que montava
// o servidor com uma fonte de série avariada e afirmava que esta rota servia à
// mesma. Saiu com a Fase 6, passo 5: **já não há fonte de série para avariar** —
// o `web.Novo` não recebe nenhuma. O desacoplamento que ele afirmava passou a ser
// estrutural em vez de verificado, que é a única forma de o garantir que não
// pode apodrecer.

// TestUmBancoEmBaixoSaiComo200ENaoComoErroDoPedido: não é este pedido que
// falhou, é aquele banco que não respondeu.
//
// ⚠️ A diferença é o que a app consegue mostrar: com 200 e a oferta em falha põe
// uma linha com o banco nomeado e o resto da lista continua a encher-se; com 5xx
// põe um ecrã de erro sobre uma comparação que está a correr bem nos outros
// quatro.
func TestUmBancoEmBaixoSaiComo200ENaoComoErroDoPedido(t *testing.T) {
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(ctx context.Context, _ dominio.Pedido) (dominio.Oferta, error) {
			<-ctx.Done() // o banco cala-se até desistirmos
			return dominio.Oferta{}, ctx.Err()
		},
	})

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if resposta.Code != http.StatusOK {
		t.Fatalf("um banco em baixo devolveu %d, e a falha é dele e não do pedido", resposta.Code)
	}

	var o api.Oferta
	lerJSON(t, resposta, &o)
	if o.Sucesso {
		t.Fatal("o banco não respondeu e a oferta saiu como boa")
	}
	if o.Erro == nil || o.Erro.Codigo != string(dominio.ErroBancoIndisponivel) {
		t.Errorf("código %+v, esperava %q", o.Erro, dominio.ErroBancoIndisponivel)
	}
	// ⚠️ Nomeado. Uma linha que diz «um banco falhou» sem dizer qual obriga quem
	// lê a contar os cartões para descobrir quem falta.
	if o.BancoNome != "CGD" {
		t.Errorf("a recusa não nomeia o banco: %q", o.BancoNome)
	}
}

// TestUmPanicoNossoAtravessaAFronteiraSemCulparOBanco (KAN-30).
//
// ⚠️ **É aqui que a atribuição se vê**, e não no caso de uso: o que chega à app é
// este JSON, e é o `codigo` que ela usa para decidir se a linha fala de um banco
// em baixo ou de um defeito nosso. O caso de uso podia estar certo e a fronteira
// engolir o código na tradução — foi assim que o `CapturadoEm` se perdeu.
//
// ⚠️ **E continua a ser 200 com oferta em falha.** Um 500 aqui derrubava a linha
// deste banco *e* dizia à app que a comparação inteira falhou; o defeito é nosso
// e é de um banco só.
func TestUmPanicoNossoAtravessaAFronteiraSemCulparOBanco(t *testing.T) {
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			panic("índice fora dos limites")
		},
	})

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta(nil))
	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d: um defeito nosso num banco não é a falha do pedido: %s",
			resposta.Code, resposta.Body.String())
	}

	var o api.Oferta
	lerJSON(t, resposta, &o)
	if o.Sucesso {
		t.Fatal("houve um pânico e a oferta saiu como boa")
	}
	if o.Erro == nil || o.Erro.Codigo != string(dominio.ErroInterno) {
		t.Errorf("código %+v, esperava %q — a app conta isto como banco em baixo",
			o.Erro, dominio.ErroInterno)
	}
	if strings.Contains(o.Erro.Mensagem, "índice fora dos limites") {
		t.Errorf("o interior do programa saiu no corpo servido: %q", o.Erro.Mensagem)
	}
}

// TestUmIdQueNaoEBancoNenhumNoCaminhoEUm404: no `/comparacoes` um id inventado é
// 400 com `campo: bancos`, porque vai no corpo; aqui vai no **caminho**, e um
// caminho que não existe é 404.
func TestUmIdQueNaoEBancoNenhumNoCaminhoEUm404(t *testing.T) {
	s := servidorAoVivo(t, &bancoFalso{id: "cgd", nome: "CGD"})

	resposta := pedirOferta(t, s, "banco-que-nunca-existiu", corpoDeOferta(nil))
	if resposta.Code != http.StatusNotFound {
		t.Fatalf("estado %d, esperava 404: %s", resposta.Code, resposta.Body.String())
	}

	var e api.RespostaErro
	lerJSON(t, resposta, &e)
	if e.Erro.Codigo != "banco_desconhecido" {
		t.Errorf("código %q — «não há tal banco» tem de se distinguir de «este banco não respondeu»",
			e.Erro.Codigo)
	}
	if !strings.Contains(e.Erro.Mensagem, "banco-que-nunca-existiu") {
		t.Errorf("a mensagem não diz qual o id que não existe: %q", e.Erro.Mensagem)
	}
}

// TestUmBancoRegistadoSemTransporteEUm404: o BPI está no registo mas o `servir`
// não lhe dá browser, e o construtor devolve nada. Saía `500 erro_interno` — a
// afirmar que estávamos avariados — para um banco que o `GET /api/v1/bancos` nem
// lista. Para quem pede, é um banco que não existe aqui.
func TestUmBancoRegistadoSemTransporteEUm404(t *testing.T) {
	s := servidor(t).ComAoVivo(func(string) (bancos.Banco, error) {
		return nil, fmt.Errorf("%w: o construtor de %q devolveu nada", bancos.ErrBancoIndisponivel, "bpi")
	}, 50*time.Millisecond)

	resposta := pedirOferta(t, s, "bpi", corpoDeOferta(nil))
	if resposta.Code != http.StatusNotFound {
		t.Fatalf("estado %d, esperava 404: %s", resposta.Code, resposta.Body.String())
	}

	var e api.RespostaErro
	lerJSON(t, resposta, &e)
	if e.Erro.Codigo != "banco_desconhecido" {
		t.Errorf("código %q — um banco sem transporte não é uma avaria nossa", e.Erro.Codigo)
	}
	if !strings.Contains(e.Erro.Mensagem, `"bpi"`) {
		t.Errorf("a mensagem não nomeia o banco: %q", e.Erro.Mensagem)
	}
}

// TestUmPedidoInvalidoNaoSeTornaUmPedidoAoBanco é a guarda de amplificação.
//
// ⚠️ **Somos um amplificador**, e o multiplicador aplica-se ao lixo tal como ao
// bom: um pedido que nós próprios sabemos recusar não pode virar tráfego para o
// simulador público de um banco, com origem aparente nossa.
func TestUmPedidoInvalidoNaoSeTornaUmPedidoAoBanco(t *testing.T) {
	var perguntas atomic.Int32
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			perguntas.Add(1)
			return dominio.Oferta{}, nil
		},
	})

	corpo := corpoDeOferta(nil)
	corpo.Pedido.Montante = 900_000 // acima do valor do imóvel

	resposta := pedirOferta(t, s, "cgd", corpo)
	if resposta.Code != http.StatusBadRequest {
		t.Fatalf("estado %d, esperava 400: %s", resposta.Code, resposta.Body.String())
	}
	var e api.RespostaErro
	lerJSON(t, resposta, &e)
	if e.Erro.Campo == nil || *e.Erro.Campo != "montante" {
		t.Errorf("o erro não nomeia o campo: %+v", e.Erro)
	}
	if n := perguntas.Load(); n != 0 {
		t.Errorf("gastaram-se %d pedidos ao banco com um pedido que nós recusamos", n)
	}
}

// TestOsProdutosEscolhidosChegamAoBanco: o corpo deste endpoint traz os produtos
// numa lista simples — o banco vem no caminho — e eles têm de chegar ao banco.
func TestOsProdutosEscolhidosChegamAoBanco(t *testing.T) {
	var vistos []string
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(_ context.Context, p dominio.Pedido) (dominio.Oferta, error) {
			vistos = p.Produtos
			return dominio.Oferta{}, nil
		},
	})

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta([]string{"cgd:seguro-vida"}))
	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d: %s", resposta.Code, resposta.Body.String())
	}
	if len(vistos) != 1 || vistos[0] != "cgd:seguro-vida" {
		t.Errorf("o banco recebeu %v e a pessoa escolheu [cgd:seguro-vida]", vistos)
	}
}

// TestUmProdutoDeOutroBancoNaoPassaNemVaiAoBanco fecha o outro lado.
//
// ⚠️ **O silêncio era o risco, não o erro.** Os produtos repartem-se pelo
// prefixo do id; um produto de outro banco chegava ao `Simular`, ninguém o
// reclamava, e a oferta saía sem ele — com a pessoa convencida de que o tinha
// escolhido e o preço a dizer o contrário. Reverter para atribuir a lista sem a
// verificação do prefixo faz este teste servir um 200.
func TestUmProdutoDeOutroBancoNaoPassaNemVaiAoBanco(t *testing.T) {
	var perguntas atomic.Int32
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			perguntas.Add(1)
			return dominio.Oferta{}, nil
		},
	})

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta([]string{"novobanco:protecao"}))
	if resposta.Code != http.StatusBadRequest {
		t.Fatalf("estado %d, esperava 400: %s", resposta.Code, resposta.Body.String())
	}
	if !strings.Contains(resposta.Body.String(), "novobanco:protecao") {
		t.Errorf("a recusa não nomeia o produto: %s", resposta.Body.String())
	}
	if n := perguntas.Load(); n != 0 {
		t.Errorf("gastaram-se %d pedidos ao banco com um produto que não é dele", n)
	}
}

// --- ajudantes -------------------------------------------------------------

type bancoFalso struct {
	id, nome  string
	responder func(context.Context, dominio.Pedido) (dominio.Oferta, error)
}

func (b *bancoFalso) ID() string                     { return b.id }
func (b *bancoFalso) Nome() string                   { return b.nome }
func (b *bancoFalso) Requisitos() dominio.Requisitos { return dominio.Requisitos{BancoID: b.id} }

func (b *bancoFalso) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	if b.responder == nil {
		return dominio.Oferta{}, nil
	}
	return b.responder(ctx, p)
}

// TestALinhaDoPanicoCruzaSeComOPedido é a metade da KAN-61 que só a fronteira
// pode afirmar.
//
// ⚠️ **O caso de uso não conhece `X-Request-ID`, e é de propósito** — é um
// cabeçalho HTTP, e o `aovivo` não sabe que existe HTTP. Quem junta o
// identificador é o `diarioDoPedido`, entregando-lhe um diário já preso a este
// pedido. Sem isso, a linha do pânico existe e **não se cruza com nada**: quem
// reclama com um identificador na mão continua sem ter com que o confrontar, que
// era metade do problema.
func TestALinhaDoPanicoCruzaSeComOPedido(t *testing.T) {
	var escrito bytes.Buffer
	s := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			panic("índice fora dos limites")
		},
	}).ComDiario(web.DiarioDeOmissao(&escrito))

	resposta := pedirOferta(t, s, "cgd", corpoDeOferta(nil))

	id := resposta.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("a resposta não trouxe X-Request-ID, e sem ele não há o que cruzar")
	}

	// ⚠️ Duas linhas saem daqui — a do pedido, do middleware, e a do pânico. É a
	// do pânico que interessa, e procura-se pela mensagem dela.
	var linha string
	for _, l := range strings.Split(strings.TrimSpace(escrito.String()), "\n") {
		if strings.Contains(l, "pânico ao simular") {
			linha = l
		}
	}
	if linha == "" {
		t.Fatalf("o pânico não deixou linha no diário. Escrito: %s", escrito.String())
	}
	if !strings.Contains(linha, `"request_id":"`+id+`"`) {
		t.Errorf("a linha do pânico não traz o request_id %q que o cliente recebeu: %s", id, linha)
	}
}

// TestUmaOfertaAjustadaSaiMarcadaEUmaDirectaNao (KAN-26).
//
// ⚠️ **A marca de que uma oferta foi simulada noutros termos é o próprio
// `aplicado`** — presente só quando houve ajuste, com a nota ao lado a nomear os
// números. Não há campo nenhum a mais de propósito: um `directamente_comparavel`
// ao lado do `aplicado` era o mesmo facto com dois nomes, e um deles podia
// divergir do outro em silêncio. O que aqui se afirma é que essa garantia cruza
// a fronteira: o que a app recebe é este JSON, e é por ele — e não reimplementando
// a regra do domínio — que distingue o que entra numa ordenação do que só se
// apresenta com o desvio dito.
//
// ⚠️ As duas ofertas têm os mesmos números de propósito: se a única diferença
// servida for a marca, é ela que a app usa. Reverter o preenchimento do
// `aplicado` na tradução faz este teste falhar a nomear o banco ajustado — e não
// «campo em falta».
func TestUmaOfertaAjustadaSaiMarcadaEUmaDirectaNao(t *testing.T) {
	tan := taxa(t, "3.250")
	ajustada := servidorAoVivo(t, &bancoFalso{
		id: "montepio", nome: "Montepio",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			o := dominio.Oferta{TAN: &tan}
			o.Acrescentar(dominio.AjustePrazo(40, 35, "o Montepio financia até aos 80 anos"))
			return o, nil
		},
	})
	directa := servidorAoVivo(t, &bancoFalso{
		id: "cgd", nome: "CGD",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			return dominio.Oferta{TAN: &tan}, nil
		},
	})

	t.Run("a ajustada sai com aplicado e nota", func(t *testing.T) {
		resposta := pedirOferta(t, ajustada, "montepio", corpoDeOferta(nil))
		if resposta.Code != http.StatusOK {
			t.Fatalf("estado %d: %s", resposta.Code, resposta.Body.String())
		}
		var o api.Oferta
		lerJSON(t, resposta, &o)
		if !o.Sucesso {
			t.Fatalf("a oferta ajustada do Montepio saiu em falha: %+v", o.Erro)
		}
		if o.Aplicado == nil {
			t.Fatal("o Montepio foi simulado a 35 anos quando se pediram 40 e a resposta não traz aplicado — ao lado das outras lê-se como se fosse o que foi pedido")
		}
		if prazo := (*o.Aplicado)["prazo_anos"]; prazo != float64(35) {
			t.Errorf("aplicado[prazo_anos] = %v, o Montepio simulou 35", prazo)
		}
		if o.Notas == nil || len(*o.Notas) == 0 {
			t.Fatal("o ajuste do Montepio saiu sem nota — sem o valor pedido, o desvio não é verificável")
		}
		for _, n := range *o.Notas {
			if !strings.Contains(n, "40 anos") || !strings.Contains(n, "35 anos") {
				t.Errorf("a nota do Montepio não nomeia o pedido nem o aplicado: %q", n)
			}
		}
	})

	t.Run("a directa sai sem a marca", func(t *testing.T) {
		resposta := pedirOferta(t, directa, "cgd", corpoDeOferta(nil))
		if resposta.Code != http.StatusOK {
			t.Fatalf("estado %d: %s", resposta.Code, resposta.Body.String())
		}
		var o api.Oferta
		lerJSON(t, resposta, &o)
		if !o.Sucesso {
			t.Fatalf("a oferta da CGD saiu em falha: %+v", o.Erro)
		}
		// Ao corpo cru e não só ao struct: `"aplicado"` no JSON é exactamente o
		// que a app vê, e `produtos_aplicados` não pode fazer passar a procura.
		if strings.Contains(resposta.Body.String(), "\"aplicado\"") {
			t.Errorf("a CGD foi simulada exactamente com o pedido e a resposta trouxe aplicado: %s", resposta.Body.String())
		}
		if o.Aplicado != nil {
			t.Errorf("uma oferta da CGD sem ajuste nenhum saiu com aplicado: %v", *o.Aplicado)
		}
	})
}

// bancoQueResponde é o construtor que o `ComAoVivo` recebe: devolve este banco
// para o id dele, e o erro do registo para qualquer outro — que é o que faz o
// 404 ser afirmável sem inventar um segundo caminho para lá chegar.
func bancoQueResponde(b *bancoFalso) func(string) (bancos.Banco, error) {
	return func(id string) (bancos.Banco, error) {
		if id != b.id {
			return nil, bancos.ErrBancoDesconhecido
		}
		return b, nil
	}
}

func servidorAoVivo(t *testing.T, b *bancoFalso) *web.Servidor {
	t.Helper()
	// ⚠️ Prazo curto: os testes que esperam por um banco calado esperam-no por
	// inteiro, e o de omissão são 15 s.
	return servidor(t).ComAoVivo(bancoQueResponde(b), 50*time.Millisecond)
}

func corpoDeOferta(produtos []string) api.OfertaPedido {
	corpo := api.OfertaPedido{Pedido: pedidoDeProva()}
	if produtos != nil {
		corpo.Produtos = &produtos
	}
	return corpo
}

func pedirOferta(t *testing.T, s *web.Servidor, banco string, corpo api.OfertaPedido) *httptest.ResponseRecorder {
	t.Helper()
	bruto, err := json.Marshal(corpo)
	if err != nil {
		t.Fatalf("serializar o pedido: %v", err)
	}
	pedido := httptest.NewRequest(http.MethodPost, "/api/v1/ofertas/"+banco, bytes.NewReader(bruto))
	pedido.Header.Set("Content-Type", "application/json")

	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)
	return resposta
}
