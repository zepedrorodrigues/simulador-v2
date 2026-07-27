// Package infra tem o que fala com o mundo: os routers chi e os handlers, o
// código gerado pelo sqlc, o travão por banco, a configuração e o relógio.
//
// ⚠️ Aqui dizia «o cliente Redis», e não havia nenhum — nem no go.mod. O Redis
// saiu do desenho na §7 do ARQUITETURA.md, e o travão que ele existia para
// guardar vive no `travao`, sobre o `pg_try_advisory_lock` do PostgreSQL.
//
// É a única camada que pode importar todas as outras. Os tipos que saem em JSON
// vivem aqui e são distintos dos tipos de dominio: são a fronteira publicada e
// versionada, que não pode mudar só porque o domínio mudou.
package infra
