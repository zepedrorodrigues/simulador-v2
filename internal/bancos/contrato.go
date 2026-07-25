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
	// notas preenchidos, e erro nulo. Quem transforma o erro numa oferta de
	// falha é o orquestrador — dentro de um banco, os erros devolvem-se, não se
	// engolem (ARQUITETURA.md §5).
	//
	// ⚠️ Respeita o ctx. O orquestrador impõe um prazo por banco, e um banco que
	// ignore o cancelamento segura a comparação inteira: no v1 o BPI consumia
	// 52 s de um pedido de 52 s. Não é recomendação — é afirmável, e afirma-se
	// com prova.RespeitaPrazo no teste de cada banco.
	Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error)
}
