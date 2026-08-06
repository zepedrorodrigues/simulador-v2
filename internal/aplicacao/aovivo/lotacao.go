package aovivo

import (
	"context"
	"errors"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O tecto de concorrência por banco (§7.2). O que ele é, e porque não é o
// travão do varrimento, está na Lotacao.

// ErrSemVaga é o banco no tecto: já vão N pedidos nossos em voo contra ele.
//
// ⚠️ **Não é uma oferta em falha, e a distinção é o ponto.** Quem está no limite
// somos nós, não o banco — escrever `banco_indisponivel` dizia à pessoa que o
// banco está em baixo quando ele está bem e quem não tem lugar é o serviço. É a
// mesma decisão que a KAN-7 já tinha registado para o travão do varrimento, e a
// razão de o `Pedir` continuar a nunca devolver erro: o que sai daqui é um erro
// **do serviço**, e a fronteira traduz-o num 503 com o banco nomeado.
var ErrSemVaga = errors.New("o banco está com a lotação cheia")

// Sair devolve a vaga. Chama-se sempre, e uma vez.
type Sair func()

// Lotacao é quantos pedidos nossos podem estar em voo contra o MESMO banco.
//
// ⚠️ **É um tecto e não um fecho, e é aí que difere do `varrimento.Travao`.**
// Aquele existia para impedir dois varrimentos do mesmo banco em simultâneo — um
// mutex —, e ao vivo isso está errado: dois clientes diferentes a perguntar pelo
// mesmo banco ao mesmo tempo é o funcionamento normal (§7.2). O que se limita é
// quantos de cada vez, e o N+1 desiste.
//
// Declara-se aqui e implementa-se em `infra/lotacao`, como o `varrimento.Travao`
// se implementava em `infra/travao`: é o que deixa afirmar a política sem
// Postgres de pé.
type Lotacao interface {
	Entrar(ctx context.Context, bancoID string) (Sair, error)
}

// PedirComVaga arranja vaga contra o banco e só então lhe pergunta.
//
// Devolve ErrSemVaga quando o banco está no tecto, e o erro da lotação quando
// não se conseguiu sequer saber se havia vaga.
//
// ⚠️ **E aí falha FECHADO — ao contrário do tecto por IP, que falha aberto.** A
// assimetria é de quem paga: o tecto por IP protege-nos a nós, e recusar tudo
// porque a base não responde transformava uma avaria nossa numa negação de
// serviço; este protege o **simulador de um terceiro** que não tem voz nenhuma
// nisto, e servir sem tecto é servir com o único travão que impede um pedido
// nosso de virar dez a alguém. Uma base em baixo já é uma avaria visível; um
// tecto que se desliga sozinho não é.
//
// ⚠️ Lotação nula desliga o tecto — é o que os testes que não a medem usam, e é
// a mesma porta do `ComTecto`. Em produção liga-se sempre (`infra/web/servir`).
func PedirComVaga(
	ctx context.Context, lot Lotacao, b bancos.Banco, p dominio.Pedido,
	agora func() time.Time, prazo time.Duration,
) (dominio.Oferta, error) {
	if lot == nil {
		return Pedir(ctx, b, p, agora, prazo), nil
	}

	sair, err := lot.Entrar(ctx, b.ID())
	if err != nil {
		return dominio.Oferta{}, err
	}
	defer sair()

	return Pedir(ctx, b, p, agora, prazo), nil
}
