// Package dominio tem os tipos e as regras do negócio: Pedido, Oferta, Fase,
// Produto, Requisitos e as políticas puras sobre eles.
//
// Zero I/O e zero dependências do projecto. Não sabe que existe HTTP nem base
// de dados, e não importa bancos, aplicacao, infra nem cmd — regra imposta pelo
// depguard, não pela boa vontade.
//
// A única dependência de terceiros é o shopspring/decimal: dinheiro e taxas não
// são vírgula flutuante (ARQUITETURA §4), e sem ele o domínio não conseguia
// representar aquilo de que trata.
//
// # Duas regras que vêm com o decimal
//
// Nunca comparar com ==. O decimal.Decimal guarda um ponteiro para um big.Int,
// por isso == compila e compara ponteiros: dá falso mesmo entre dois valores
// construídos exactamente da mesma maneira (medido). Nenhum linter do portão
// apanha isto. É por isso que Dinheiro, Taxa e Racio têm um campo [0]func() à
// cabeça, que torna == um erro de compilação, e é por isso que a comparação se
// faz por Equal ou Cmp.
//
// Nunca mexer no decimal.DivisionPrecision. É estado global mutável de uma
// biblioteca, partilhado por todo o processo; as 16 casas por omissão chegam.
//
// # O relógio não vive aqui
//
// Idade e prazo dependem de "hoje", e "hoje" é I/O. As políticas recebem a data
// por parâmetro; quem a produz é o relógio da infra.
package dominio
