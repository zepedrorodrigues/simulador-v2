// Package esquema aplica e verifica as migrações da base de dados.
//
// A regra é a do ARQUITETURA: numa base que existe, quem manda são as
// migrações, corridas de propósito — o arranque NÃO migra sozinho. Foi o
// create_all automático do v1 que deixou a base num estado híbrido que o
// Alembic desconhecia, e a migração seguinte morreu sobre uma tabela que já
// existia. Aqui o arranque só verifica (ExigirEmDia) e recusa-se a servir com
// uma base por migrar; aplicar é um acto explícito (Migrar).
//
// As migrações correm por database/sql (o que o goose usa), pelo driver pgx.
// É uma ligação à parte do pool pgx das queries: só o goose a usa, e só o
// tempo de migrar ou verificar.
package esquema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // regista o driver "pgx" no database/sql
	"github.com/pressly/goose/v3"

	"github.com/zepedrorodrigues/simulador-v2/db/migracoes"
)

// ErrPorMigrar diz que a base tem migrações por aplicar. O arranque devolve-o e
// recusa-se a servir; a mensagem nomeia o que fazer.
var ErrPorMigrar = errors.New("base de dados por migrar — corre `simulador migrar` antes de servir")

// Ligar abre uma ligação database/sql à base em url, pelo driver pgx. É a
// ligação que o goose usa para migrar e verificar. Quem chama fecha-a.
func Ligar(url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("abrir ligação à base: %w", err)
	}
	return db, nil
}

// novoProvider constrói o goose sobre as migrações embebidas. Um provider novo
// por operação: as migrações são SQL, sem registo global a partilhar.
func novoProvider(db *sql.DB) (*goose.Provider, error) {
	return goose.NewProvider(goose.DialectPostgres, db, migracoes.FS)
}

// Migrar aplica todas as migrações pendentes (goose up). É o acto explícito —
// nada disto acontece ao arrancar.
func Migrar(ctx context.Context, db *sql.DB) error {
	p, err := novoProvider(db)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("aplicar migrações: %w", err)
	}
	return nil
}

// Reverter desfaz a última migração aplicada (goose down).
func Reverter(ctx context.Context, db *sql.DB) error {
	p, err := novoProvider(db)
	if err != nil {
		return err
	}
	if _, err := p.Down(ctx); err != nil {
		return fmt.Errorf("reverter migração: %w", err)
	}
	return nil
}

// ExigirEmDia devolve ErrPorMigrar se houver migrações por aplicar. Não migra
// nada: é a guarda do arranque, não o migrador.
func ExigirEmDia(ctx context.Context, db *sql.DB) error {
	p, err := novoProvider(db)
	if err != nil {
		return err
	}
	pendente, err := p.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("verificar estado das migrações: %w", err)
	}
	if pendente {
		return ErrPorMigrar
	}
	return nil
}
