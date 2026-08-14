package aovivo_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/aovivo"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O tecto de concorrência por banco, do lado da política (KAN-7). Quem o conta é
// o Postgres, e isso afirma-se no `infra/lotacao`; o que se mede aqui é o que a
// aplicação faz com a resposta dele.

// TestUmBancoNoTectoNaoSaiComoOfertaEmFalha é a afirmação central desta parte.
//
// ⚠️ **A tentação é servir 200 com `banco_indisponivel`** — o caminho já lá está
// e o código já existe. Seria mentira: o banco está bem, e quem não tem lugar
// somos nós. Reverter para devolver uma oferta em falha em vez do erro faz este
// teste dizer que se culpou o banco por um limite nosso.
func TestUmBancoNoTectoNaoSaiComoOfertaEmFalha(t *testing.T) {
	t.Parallel()

	var perguntas atomic.Int32
	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			perguntas.Add(1)
			return dominio.Oferta{}, nil
		},
	}
	cheia := lotacaoFalsa{entrar: func(context.Context, string) (aovivo.Sair, error) {
		return nil, aovivo.ErrSemVaga
	}}

	o, err := aovivo.PedirComVaga(context.Background(), cheia, b, pedido(t), agora, time.Second, nil)

	if !errors.Is(err, aovivo.ErrSemVaga) {
		t.Fatalf("erro %v, esperava ErrSemVaga — um tecto nosso não é uma falha do banco", err)
	}
	if o.Erro != nil {
		t.Errorf("saiu uma oferta em falha (%v) por um limite que é nosso", o.Erro)
	}
	// ⚠️ E o pedido não chega a sair. Um tecto que conta o pedido depois de o
	// fazer não é um tecto.
	if n := perguntas.Load(); n != 0 {
		t.Errorf("com o banco no tecto perguntou-se-lhe %d vezes", n)
	}
}

// TestSemSaberSeHaVagaNaoSePerguntaAoBanco: a lotação avariada FECHA.
//
// ⚠️ É o contrário do tecto por IP, que falha aberto, e a assimetria é de quem
// paga: aquele protege-nos a nós — recusar tudo porque a base não responde
// transformava uma avaria nossa numa negação de serviço —, este protege o
// simulador de um terceiro que não tem voz nenhuma nisto. Reverter para
// prosseguir quando a lotação erra faz este teste contar o pedido que saiu.
func TestSemSaberSeHaVagaNaoSePerguntaAoBanco(t *testing.T) {
	t.Parallel()

	var perguntas atomic.Int32
	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			perguntas.Add(1)
			return dominio.Oferta{}, nil
		},
	}
	avariada := lotacaoFalsa{entrar: func(context.Context, string) (aovivo.Sair, error) {
		return nil, errors.New("a base não responde")
	}}

	_, err := aovivo.PedirComVaga(context.Background(), avariada, b, pedido(t), agora, time.Second, nil)

	if err == nil {
		t.Fatal("a lotação avariada deixou passar: serviu-se sem tecto contra o banco")
	}
	if errors.Is(err, aovivo.ErrSemVaga) {
		t.Errorf("erro %v: «não sei se há vaga» não é «não há vaga», e a mensagem tem de as separar", err)
	}
	if n := perguntas.Load(); n != 0 {
		t.Errorf("sem tecto garantido saíram %d pedidos ao banco", n)
	}
}

// TestAVagaVoltaMesmoQuandoOBancoRebenta: a vaga é um recurso emprestado, e um
// pânico do banco não a pode ficar a segurar.
//
// ⚠️ Uma vaga que não volta é pior do que não haver tecto: o banco fica com
// menos um lugar para sempre, e ao fim de N pânicos deixa de se lhe perguntar de
// todo — sem nada no caminho a dizer porquê.
func TestAVagaVoltaMesmoQuandoOBancoRebenta(t *testing.T) {
	t.Parallel()

	var saidas atomic.Int32
	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			panic("o banco rebentou a meio")
		},
	}
	uma := lotacaoFalsa{entrar: func(context.Context, string) (aovivo.Sair, error) {
		return func() { saidas.Add(1) }, nil
	}}

	o, err := aovivo.PedirComVaga(context.Background(), uma, b, pedido(t), agora, time.Second, nil)
	if err != nil {
		t.Fatalf("havia vaga e devolveu-se erro: %v", err)
	}
	if o.Sucesso() {
		t.Error("o banco entrou em pânico e a oferta saiu como boa")
	}
	if n := saidas.Load(); n != 1 {
		t.Errorf("a vaga voltou %d vezes, esperava 1", n)
	}
}

// TestSemLotacaoOCaminhoEOMesmo: lotação nula não trava nada. É a porta que os
// testes que não medem o tecto usam — e obriga a que ligá-la seja decisão de
// quem monta o servidor, não descuido de quem escreve o caso de uso.
func TestSemLotacaoOCaminhoEOMesmo(t *testing.T) {
	t.Parallel()

	tan := taxa(t, "3.250")
	b := &bancoFalso{
		id: "prova", nome: "Banco de Prova",
		responder: func(context.Context, dominio.Pedido) (dominio.Oferta, error) {
			return dominio.Oferta{TAN: &tan}, nil
		},
	}

	o, err := aovivo.PedirComVaga(context.Background(), nil, b, pedido(t), agora, time.Second, nil)
	if err != nil {
		t.Fatalf("sem lotação: %v", err)
	}
	if !o.Sucesso() {
		t.Fatalf("a oferta saiu em falha: %v", o.Erro)
	}
}

type lotacaoFalsa struct {
	entrar func(ctx context.Context, bancoID string) (aovivo.Sair, error)
}

func (l lotacaoFalsa) Entrar(ctx context.Context, bancoID string) (aovivo.Sair, error) {
	return l.entrar(ctx, bancoID)
}
