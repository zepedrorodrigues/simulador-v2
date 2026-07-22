// Package aplicacao tem os casos de uso: comparar, varrer, cachear, limitar.
//
// Sem HTTP e sem SQL. Recebe os bancos por injecção e nunca importa o registo
// directamente — é o que torna um caso de uso testável com bancos falsos.
package aplicacao
