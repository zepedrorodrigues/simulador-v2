package novobanco_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/novobanco"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Os limites do Novo Banco vêm do GET /configuracoes (KAN-37), que o banco
// publica sem parâmetros nenhuns e que se gravou em `capturas/configuracoes.resposta.json`.
//
// ⚠️ Os limites vêm DO BANCO e não de constantes nossas — a mesma regra do
// /credit_limit do Santander e do /limits da CGD: uma tabela nossa envelhecia em
// silêncio, e o critério de pronto do KAN-37 é exactamente este: um pedido
// abaixo do mínimo é recusado **sem ir à rede**, com uma mensagem que nomeia o
// limite e o valor.

// montarConfiguracoes monta o Novo Banco com o transporte a responder às duas
// capturas — o /configuracoes e o /calculo — e uma loja de catálogos (nil vale
// "sem loja", e então o /configuracoes é pedido a cada simulação).
func montarConfiguracoes(t *testing.T, cat novobanco.Catalogos) (*novobanco.Banco, *transporte.Falso) {
	t.Helper()
	config := string(captura(t, "configuracoes.resposta.json"))
	calculo := string(captura(t, "variavel_12m.resposta.json"))

	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			switch {
			case strings.HasSuffix(p.URL.Path, "/configuracoes"):
				return transporte.RespostaDeTexto(http.StatusOK, config), nil
			case strings.HasSuffix(p.URL.Path, "/simulacao/calculo"):
				return transporte.RespostaDeTexto(http.StatusOK, calculo), nil
			default:
				t.Errorf("pedido a um caminho que o Novo Banco não tem: %q", p.URL.Path)
				return transporte.RespostaDeTexto(http.StatusNotFound, ""), nil
			}
		},
	}
	return novobanco.Novo(falso, cat), falso
}

// contouCalculos conta as idas ao endpoint que calcula — o que KAN-37 quer que
// um pedido abaixo do mínimo **não** faça.
func contouCalculos(f *transporte.Falso) int {
	n := 0
	for _, p := range f.Pedidos() {
		if strings.HasSuffix(p.URL.Path, "/simulacao/calculo") {
			n++
		}
	}
	return n
}

// TestAbaixoDoMinimoErecusadoSemIrARede é o critério de pronto do KAN-37.
//
// ⚠️ Conta os pedidos, e não só o erro: uma versão que fosse à rede buscar a
// recusa do banco e a traduzisse continuaria a "recusar", mas gastava uma
// chamada para receber um erro que os limites já diziam — que é exactamente o
// que esta issue existe para tirar.
func TestAbaixoDoMinimoErecusadoSemIrARede(t *testing.T) {
	banco, falso := montarConfiguracoes(t, nil)

	p := pedidoBase()
	p.Montante = dominio.DinheiroDeInteiro(1_000)

	_, err := banco.Simular(t.Context(), p)

	var erroOferta *dominio.ErroOferta
	if !errors.As(err, &erroOferta) {
		t.Fatalf("esperava um ErroOferta, veio %v", err)
	}
	if erroOferta.Codigo != dominio.ErroProdutoIndisponivel {
		t.Errorf("código: esperava %q, veio %q", dominio.ErroProdutoIndisponivel, erroOferta.Codigo)
	}
	// A mensagem tem de nomear o limite (2 000 €) e o valor que se pediu (1 000 €).
	if !strings.Contains(erroOferta.Mensagem, "2 000") || !strings.Contains(erroOferta.Mensagem, "1 000") {
		t.Errorf("a mensagem tem de nomear o limite e o valor: %q", erroOferta.Mensagem)
	}
	if n := contouCalculos(falso); n != 0 {
		t.Errorf("o pedido abaixo do mínimo foi ao /calculo %d vezes; tinha de ser recusado antes", n)
	}
}

// TestAcimaDoMaximoErecusadoSemIrARede fecha o outro lado da fronteira do
// montante.
func TestAcimaDoMaximoErecusadoSemIrARede(t *testing.T) {
	banco, falso := montarConfiguracoes(t, nil)

	p := pedidoBase()
	p.Montante = dominio.DinheiroDeInteiro(2_000_000)

	_, err := banco.Simular(t.Context(), p)

	var erroOferta *dominio.ErroOferta
	if !errors.As(err, &erroOferta) {
		t.Fatalf("esperava um ErroOferta, veio %v", err)
	}
	if erroOferta.Codigo != dominio.ErroProdutoIndisponivel {
		t.Errorf("código: esperava %q, veio %q", dominio.ErroProdutoIndisponivel, erroOferta.Codigo)
	}
	if !strings.Contains(erroOferta.Mensagem, "1 800 000") || !strings.Contains(erroOferta.Mensagem, "2 000 000") {
		t.Errorf("a mensagem tem de nomear o limite e o valor: %q", erroOferta.Mensagem)
	}
	if n := contouCalculos(falso); n != 0 {
		t.Errorf("o pedido acima do máximo foi ao /calculo %d vezes; tinha de ser recusado antes", n)
	}
}

// O valor do imóvel tem a sua própria fronteira (11 000 € / 2 000 000 €), e
// esta é a recusa que a app vai mostrar a quem não percebe porquê.
func TestImovelAbaixoDoMinimoErecusadoSemIrARede(t *testing.T) {
	banco, falso := montarConfiguracoes(t, nil)

	p := pedidoBase()
	p.ValorImovel = dominio.DinheiroDeInteiro(5_000)

	_, err := banco.Simular(t.Context(), p)

	var erroOferta *dominio.ErroOferta
	if !errors.As(err, &erroOferta) {
		t.Fatalf("esperava um ErroOferta, veio %v", err)
	}
	if erroOferta.Codigo != dominio.ErroProdutoIndisponivel {
		t.Errorf("código: esperava %q, veio %q", dominio.ErroProdutoIndisponivel, erroOferta.Codigo)
	}
	if !strings.Contains(erroOferta.Mensagem, "11 000") || !strings.Contains(erroOferta.Mensagem, "5 000") {
		t.Errorf("a mensagem tem de nomear o limite e o valor: %q", erroOferta.Mensagem)
	}
	if n := contouCalculos(falso); n != 0 {
		t.Errorf("o pedido abaixo do mínimo foi ao /calculo %d vezes; tinha de ser recusado antes", n)
	}
}

// A idade entra pelos limites do banco (18 a 75), e manda a do titular mais
// velho — a mesma que aperta o prazo.
func TestIdadeAcimaDoMaximoErecusadaSemIrARede(t *testing.T) {
	banco, falso := montarConfiguracoes(t, nil)

	p := pedidoBase()
	p.Titulares = []dominio.Titular{{
		DataNascimento:   nascidoEm(1945, 1, 15), // 81 anos a 2026-08-15
		RendimentoMensal: dominio.DinheiroDeInteiro(2000),
	}}

	_, err := banco.Simular(t.Context(), p)

	var erroOferta *dominio.ErroOferta
	if !errors.As(err, &erroOferta) {
		t.Fatalf("esperava um ErroOferta, veio %v", err)
	}
	if erroOferta.Codigo != dominio.ErroProdutoIndisponivel {
		t.Errorf("código: esperava %q, veio %q", dominio.ErroProdutoIndisponivel, erroOferta.Codigo)
	}
	if !strings.Contains(erroOferta.Mensagem, "75") {
		t.Errorf("a mensagem tem de nomear o tecto de idade: %q", erroOferta.Mensagem)
	}
	if n := contouCalculos(falso); n != 0 {
		t.Errorf("o pedido com titular acima do tecto foi ao /calculo %d vezes; tinha de ser recusado antes", n)
	}
}

// lerConfiguracoes é puro e testa-se contra a captura gravada, sem rede: os
// números que o KAN-37 regista são os que saem daqui.
func TestLerConfiguracoesLeOsLimitesDaCaptura(t *testing.T) {
	banco, falso := montarConfiguracoes(t, nil)

	// Um pedido válido passa os limites e chega ao /calculo — o que prova que a
	// leitura aconteceu e não recusou nada que coubesse.
	if _, err := banco.Simular(t.Context(), pedidoBase()); err != nil {
		t.Fatalf("um pedido dentro dos limites foi recusado: %v", err)
	}
	if n := contouCalculos(falso); n != 1 {
		t.Errorf("o pedido dentro dos limites foi ao /calculo %d vezes, esperava 1", n)
	}
}

// --- o catálogo guardado (KAN-37) ---------------------------------------------

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

// contouConfiguracoes conta as idas ao GET /configuracoes — é o pedido que o
// catálogo guardado existe para poupar.
func contouConfiguracoes(f *transporte.Falso) int {
	n := 0
	for _, p := range f.Pedidos() {
		if strings.HasSuffix(p.URL.Path, "/configuracoes") {
			n++
		}
	}
	return n
}

// TestOConfiguracoesGuardadoPoupaOPedido é a afirmação de cache do KAN-37: o
// /configuracoes é pedido uma vez e, enquanto a loja o tiver, não volta a ser
// pedido — mesmo num banco novo, como um segundo cliente noutro processo.
//
// ⚠️ **Conta o pedido, e não o resultado.** Uma versão que só comparasse ofertas
// passava com o /configuracoes a ser pedido na mesma — e o que esta issue existe
// para tirar é o pedido.
func TestOConfiguracoesGuardadoPoupaOPedido(t *testing.T) {
	loja := novoCatalogoFalso()

	// Primeira simulação: não há catálogo, vai-se ao /configuracoes.
	primeiro, tPrimeiro := montarConfiguracoes(t, loja)
	if _, err := primeiro.Simular(t.Context(), pedidoBase()); err != nil {
		t.Fatalf("primeira simulação: %v", err)
	}
	if n := contouConfiguracoes(tPrimeiro); n != 1 {
		t.Errorf("a primeira simulação pediu o /configuracoes %d vezes, esperava 1", n)
	}
	if loja.escritas != 1 {
		t.Fatalf("o /configuracoes lido não foi guardado (%d escritas)", loja.escritas)
	}

	// Segunda simulação, banco novo — como um segundo cliente, noutro processo.
	segundo, tSegundo := montarConfiguracoes(t, loja)
	if _, err := segundo.Simular(t.Context(), pedidoBase()); err != nil {
		t.Fatalf("segunda simulação: %v", err)
	}
	if n := contouConfiguracoes(tSegundo); n != 0 {
		t.Errorf("a segunda simulação foi buscar o /configuracoes %d vezes: o catálogo guardado não o poupou", n)
	}
	// E continua a fazer o resto: o cálculo não se poupa.
	if n := contouCalculos(tSegundo); n != 1 {
		t.Errorf("a segunda simulação foi ao /calculo %d vezes, esperava 1", n)
	}
}

// TestConfiguracoesEmBaixoNaoSeGuardaENaoTrava é o falha aberto do KAN-37: um
// /configuracoes que não responde não trava a simulação, e a falha nunca se
// guarda — guardá-la fazia o próximo cliente servir limites de ontem como se
// fossem os de hoje.
func TestConfiguracoesEmBaixoNaoSeGuardaENaoTrava(t *testing.T) {
	loja := novoCatalogoFalso()
	calculo := string(captura(t, "variavel_12m.resposta.json"))

	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			if strings.HasSuffix(p.URL.Path, "/configuracoes") {
				return transporte.RespostaDeTexto(http.StatusInternalServerError, ""), nil
			}
			return transporte.RespostaDeTexto(http.StatusOK, calculo), nil
		},
	}

	if _, err := novobanco.Novo(falso, loja).Simular(t.Context(), pedidoBase()); err != nil {
		t.Fatalf("com o /configuracoes em baixo o banco tinha de simular na mesma: %v", err)
	}
	if loja.escritas != 0 {
		t.Errorf("guardou-se uma falha do /configuracoes (%d escritas)", loja.escritas)
	}
}
