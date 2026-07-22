// Package bancos tem o contrato que cada banco cumpre, o registo que mapeia
// bancoID para construtor, e um subpacote por banco.
//
// As quatro estratégias de transporte vivem em transporte/. Um banco não sabe
// que existe base de dados nem cache: devolve tipos de dominio.
package bancos
