// Package migracoes embebe as migrações .sql no binário.
//
// O executável não anda acompanhado de uma pasta de .sql soltos: as migrações
// viajam dentro dele, e é a mesma cópia que o `simulador migrar` aplica e que o
// arranque consulta para saber se a base está em dia. As .sql continuam aqui
// porque é também daqui que o sqlc lê o esquema (db/sqlc.yaml) — uma só fonte.
package migracoes

import "embed"

// FS são as migrações goose, na raiz deste sistema de ficheiros embebido. O
// goose recebe-o por SetBaseFS/NewProvider e lê os .sql pela ordem do nome.
//
//go:embed *.sql
var FS embed.FS
