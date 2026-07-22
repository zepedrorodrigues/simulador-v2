// Package dominio tem os tipos e as regras do negócio: Pedido, Oferta, Fase,
// Produto, Requisitos e as políticas puras sobre eles.
//
// Zero I/O e zero dependências do projecto — só a biblioteca padrão. Não sabe
// que existe HTTP nem base de dados. A regra é imposta pelo depguard, não pela
// boa vontade.
package dominio
