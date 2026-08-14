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
// ✅ Sai como `erro_interno` desde 2026-08-11 (KAN-30). Saía como
// `banco_indisponivel`, e era o banco a levar com um defeito nosso.
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
//
// ⚠️ O relógio entra por argumento e não se lê aqui do `time.Now`: um caso de uso
// que leia o relógio deixa de ser afirmável sem o falsear. É a mesma escolha do
// `Varredor.Agora`. Serve duas coisas — a data valida o pedido (a idade decide o
// prazo máximo) e o instante carimba a captura.
func Pedir(
	ctx context.Context, b bancos.Banco, p dominio.Pedido, agora func() time.Time, prazo time.Duration,
) dominio.Oferta {
	// ⚠️ **`erro_interno` e não `resposta_ilegivel`** (KAN-60). Aquele código diz
	// «o banco respondeu, e o que veio não se consegue ler», e aqui não houve
	// resposta nenhuma — esta guarda existe para não se gastar um pedido a um
	// terceiro. ⚠️ E a culpa também não é de quem pediu: a fronteira valida antes
	// (`web.go`) e devolve `400 pedido_invalido` com o campo nomeado, portanto por
	// HTTP não se chega aqui. Chegar significa que dois validadores nossos
	// discordam, ou que um chamador novo se esqueceu de validar.
	if err := p.Validar(dominio.DataDeInstante(agora())); err != nil {
		return dominio.Falhar(b.ID(), b.Nome(), &dominio.ErroOferta{
			Codigo: dominio.ErroInterno,
			Mensagem: fmt.Sprintf(
				"Não se chegou a perguntar ao %s: o pedido não passou na nossa própria validação (%v). "+
					"Não é uma falha do banco.", b.Nome(), err),
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

	// ⚠️ **E quando se perguntou.** O `dominio.Oferta` diz que o `CapturadoEm` é
	// preenchido por esta camada, e ao vivo o instante é este — o da conversa com
	// o banco, e não o de um varrimento (`API.md`, §1). Sem ele a fronteira recusa
	// servir a oferta («não diz de quando é o preço»), e recusa-a a **todas**: um
	// preço sem data apresenta-se como se fosse de agora.
	oferta.CapturadoEm = agora()
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
		// ⚠️ **O `err` não entra na mensagem, e a omissão é a metade que falta**
		// (KAN-30). O valor de um pânico é o interior do programa; despejá-lo num
		// ecrã de crédito à habitação não ajuda quem lê e diz a quem não devia o
		// que rebentou cá dentro. E não se perde rasto por sair daqui: rasto não
		// havia — o diário regista método, caminho e estatuto, e o texto do pânico
		// ia só para o telemóvel de quem o apanhou. Pô-lo onde se procura é a
		// KAN-22.
		return &dominio.ErroOferta{
			Codigo: dominio.ErroInterno,
			Mensagem: fmt.Sprintf(
				"Não se conseguiu pedir a simulação ao %s por uma falha nossa. Não é uma falha do banco.",
				bancoNome),
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
