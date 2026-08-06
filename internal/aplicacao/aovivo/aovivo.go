// Package aovivo pergunta a UM banco o preço do crédito que o cliente descreveu.
//
// É o caminho do cliente desde a reversão da §1 (2026-08-06): não há grelha, não
// há fotografia, não há modelo de preço nosso. O que se serve é o que o banco
// respondeu, traduzido para o vocabulário do domínio e mais nada.
//
// ⚠️ **Uma oferta por chamada, e não N.** O fan-out vive na app (D2): ela pede um
// banco de cada vez e mostra a lista a encher-se. Pôr o fan-out aqui obrigaria a
// esperar pelo banco mais lento para mostrar o primeiro preço, que é exactamente
// o que a D2 recusou.
package aovivo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// ErrPanico marca um pânico nosso a simular, para o traduzir poder distingui-lo
// de o banco não responder.
//
// ⚠️ Hoje sai como banco_indisponivel, que culpa o banco por um erro nosso. É
// conhecido e é a KAN-30 — o sentinela existe para que essa decisão tenha por
// onde pegar.
var ErrPanico = errors.New("pânico ao simular")

// Pedir interroga o banco e devolve sempre uma Oferta — nunca um erro.
//
// ⚠️ **Nunca devolve erro, e é decisão e não descuido.** Quem chama isto serve um
// cliente, e um banco em baixo não é uma falha do pedido: é uma linha da lista
// que sai com o banco nomeado. O código de erro por oferta existe precisamente
// para isso (§5), e a alternativa — propagar o erro — obrigaria cada chamador a
// reinventar a mesma conversão.
//
// ⚠️ **O prazo é por banco.** Um banco lento não pode segurar a comparação, e ao
// vivo a espera é de uma pessoa: medido a 2026-08-06, o Montepio não respondeu
// dentro de 10 s em 4 cenários, em hora de expediente.
// ⚠️ O `hoje` entra por argumento e não se lê aqui de um relógio: a validação do
// pedido depende dele (a idade decide o prazo máximo), e um caso de uso que leia
// o relógio deixa de ser afirmável sem o falsear. É a mesma escolha do `Comparar`.
func Pedir(
	ctx context.Context, b bancos.Banco, p dominio.Pedido, hoje dominio.Data, prazo time.Duration,
) dominio.Oferta {
	if err := p.Validar(hoje); err != nil {
		return dominio.Falhar(b.ID(), b.Nome(), &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: fmt.Sprintf("O pedido não é válido: %v", err),
		})
	}

	ctx, cancelar := context.WithTimeout(ctx, prazo)
	defer cancelar()

	type resposta struct {
		oferta dominio.Oferta
		err    error
	}
	// ⚠️ Espaço para uma. Sem ele, um banco surdo que voltasse depois de nós
	// desistirmos ficava bloqueado para sempre a escrever num canal sem leitor —
	// e aí a fuga já era nossa. Ao vivo isso acumula **por pedido de cliente**,
	// que é a diferença para o varrimento, onde acumulava por corrida.
	resp := make(chan resposta, 1)

	go func() {
		oferta, err := correr(ctx, b, p)
		resp <- resposta{oferta: oferta, err: err}
	}()

	var oferta dominio.Oferta
	select {
	case r := <-resp:
		if r.err != nil {
			oferta = dominio.Falhar(b.ID(), b.Nome(), traduzir(r.err, b.Nome()))
			break
		}
		oferta = r.oferta
	case <-ctx.Done():
		oferta = dominio.Falhar(b.ID(), b.Nome(), &dominio.ErroOferta{
			Codigo: dominio.ErroBancoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O %s não respondeu dentro do prazo de %s e não desistiu quando lho pedimos.",
				b.Nome(), prazo),
		})
	}

	// Quem sabe a quem se perguntou é este lado, e a resposta tem de o dizer
	// mesmo que o banco se esqueça de se identificar.
	oferta.BancoID, oferta.BancoNome = b.ID(), b.Nome()
	return oferta
}

// correr chama o banco com o pânico recuperado.
//
// ⚠️ É aqui que fica a captura genérica, e num sítio só. No v1 ela obrigava a um
// `except Exception` dentro de cada scraper; aqui, dentro de um banco, os erros
// devolvem-se e não se engolem (§5).
func correr(ctx context.Context, b bancos.Banco, p dominio.Pedido) (oferta dominio.Oferta, err error) {
	defer func() {
		if r := recover(); r != nil {
			oferta, err = dominio.Oferta{}, fmt.Errorf("%w: %v", ErrPanico, r)
		}
	}()
	return b.Simular(ctx, p)
}

// traduzir converte o erro de um banco no erro estruturado que a oferta leva.
//
// ⚠️ Duplica o `varrimento.traduzir` de propósito, e a duplicação tem prazo: o
// `varrimento` sai no passo 6 da Fase 6 do `PLAN.md`. Refactorizar agora um
// pacote que se vai apagar custava mais do que a cópia, e a cópia morre com ele.
func traduzir(err error, bancoNome string) *dominio.ErroOferta {
	// O banco que já se explicou em dominio.ErroOferta sabe melhor do que nós o
	// que lhe aconteceu: produto indisponível, prazo impossível, resposta
	// ilegível. Não se sobrepõe.
	var estruturado *dominio.ErroOferta
	if errors.As(err, &estruturado) {
		return estruturado
	}

	if errors.Is(err, ErrPanico) {
		// ⚠️ Culpa o banco por um erro nosso. Conhecido, e é a KAN-30.
		return &dominio.ErroOferta{
			Codigo:   dominio.ErroBancoIndisponivel,
			Mensagem: fmt.Sprintf("Erro nosso ao simular o %s: %v", bancoNome, err),
		}
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &dominio.ErroOferta{
			Codigo:   dominio.ErroBancoIndisponivel,
			Mensagem: fmt.Sprintf("O %s não respondeu dentro do prazo.", bancoNome),
		}
	}

	// O contrato do banco diz que só se devolve erro quando não se conseguiu
	// responder de todo — logo, o que sobra é indisponibilidade.
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroBancoIndisponivel,
		Mensagem: fmt.Sprintf("O %s não respondeu: %v", bancoNome, err),
	}
}
