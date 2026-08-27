// Package aplicacao tem os casos de uso. Hoje é um: `aovivo`, pedir a UM banco
// com vaga. O `varrimento` e o `comparar` saíram a 2026-08-07 (§7 do
// ARQUITETURA.md).
//
// Sem HTTP e sem SQL. Recebe os bancos por injecção e nunca importa o registo
// directamente — é o que torna um caso de uso testável com bancos falsos.
package aplicacao
