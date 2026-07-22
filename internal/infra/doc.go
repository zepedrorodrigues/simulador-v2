// Package infra tem o que fala com o mundo: os routers chi e os handlers, o
// código gerado pelo sqlc, o cliente Redis, a configuração e o relógio.
//
// É a única camada que pode importar todas as outras. Os tipos que saem em JSON
// vivem aqui e são distintos dos tipos de dominio: são a fronteira publicada e
// versionada (docs/ARQUITETURA.md §3).
package infra
