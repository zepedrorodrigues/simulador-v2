package bancos

import (
	"context"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Banco é o que um banco sabe fazer. Nada mais entra aqui: se um banco precisar
// de um método que só ele tem, isso é um detalhe do seu pacote.
type Banco interface {
	ID() string
	Nome() string

	// Requisitos declara o que este banco usa, aceita e impõe: que inputs lê,
	// que períodos fixos são válidos, se deixa escolher o indexante Euribor, que
	// produtos expõe, e os limites de prazo. A app monta o formulário adaptativo
	// a partir disto — é a única fonte dessa informação.
	Requisitos() dominio.Requisitos

	// Simular interroga o banco.
	//
	// Devolve erro apenas quando o banco não conseguiu responder de todo; um
	// pedido que o banco ajustou devolve uma dominio.Oferta com os ajustes e as
	// notas preenchidos, e erro nulo. Quem transforma o erro numa observação de
	// falha é o orquestrador do varrimento — dentro de um banco, os erros
	// devolvem-se, não se engolem (ARQUITETURA.md §5).
	//
	// ⚠️ Respeita o ctx. O orquestrador impõe um prazo por pedido, e um banco que
	// ignore o cancelamento segura o varrimento inteiro: no v1 o BPI consumia
	// 52 s de um pedido de 52 s. Não é recomendação — é afirmável, e afirma-se
	// com prova.RespeitaPrazo no teste de cada banco.
	//
	// ⚠️ Quem chama isto é o CAMINHO DO CLIENTE, desde a reversão da §1
	// (2026-08-06). Esteve escrito aqui o contrário — «o varrimento, em hora
	// morta, e nunca um pedido de cliente» — entre a inversão de 2026-07-25 e a
	// reversão. Agora um pedido de comparação chega mesmo aqui, e o prazo que o
	// ctx traz é o de alguém à espera.
	Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error)
}

// Catalogos guarda o que um banco PUBLICA e que é preciso saber antes de lhe
// montar o pedido (KAN-36, `ARQUITETURA.md` §4).
//
// ⚠️ **Declarada aqui e implementada na infra**, como a `aovivo.Lotacao`. Um
// banco não pode falar com o Postgres — a regra de dependência é
// `bancos → dominio` e o `depguard` impõe-na —, portanto o que ele recebe é esta
// interface e não uma ligação.
//
// ⚠️ **Não é a cache de respostas, e a diferença é o que a tabela é.** Uma
// resposta é o preço de alguém e leva as guardas de privacidade do §7.6; um
// catálogo é o que o banco mostra a quem visite o site. Por isso a chave aqui é
// legível — `(bancoID, nome)` — e não um resumo.
//
// ⚠️ **Nulo desliga**, como a `Lotacao` nula desliga o tecto: o banco vai à fonte
// de cada vez. É o que os testes de banco usam, e o que faz o pacote de um banco
// continuar afirmável sem base de dados nenhuma.
//
// O `valor` é opaco para quem guarda: a forma é do banco que o escreve e só ele
// a lê. Guardar aqui um tipo do domínio obrigaria esta camada a conhecer os
// catálogos todos de todos os bancos.
type Catalogos interface {
	// Ler devolve o catálogo válido, ou `achou` falso quando não há nenhum — por
	// nunca ter sido lido, por ter expirado, ou por a base não responder.
	//
	// ⚠️ Não devolve erro de propósito. Um catálogo que não se consegue ler não é
	// uma falha do pedido: é uma ida à fonte, que é o que acontecia sempre antes
	// desta tabela existir. Falhar aqui fechado transformava uma avaria nossa na
	// impossibilidade de servir aquele banco.
	Ler(ctx context.Context, bancoID, nome string) (valor []byte, achou bool)

	// Guardar grava o catálogo lido da fonte, com a validade que a infra decide.
	//
	// ⚠️ Também não devolve erro: não conseguir guardar significa que o próximo
	// pedido vai outra vez à fonte — mais caro, e correcto na mesma.
	Guardar(ctx context.Context, bancoID, nome string, valor []byte)
}
