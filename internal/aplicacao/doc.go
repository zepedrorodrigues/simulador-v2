// Package aplicacao tem os casos de uso: varrer, comparar, limitar.
//
// ⚠️ Não há "cachear". Havia, e saiu a 2026-07-25 com a inversão da §1 do
// ARQUITETURA.md: sem pedido ao banco no caminho do cliente, não há espera para
// gerir nem resposta para guardar.
//
// Sem HTTP e sem SQL. Recebe os bancos por injecção e nunca importa o registo
// directamente — é o que torna um caso de uso testável com bancos falsos.
package aplicacao
