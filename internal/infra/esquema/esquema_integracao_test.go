package esquema_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
)

// Migrar cria as tabelas do esquema; Reverter desfaz a última. Prova o goose
// up/down contra um Postgres a sério — a mesma imagem do docker-compose.
func TestMigrarCriaTabelasEReverterDesfazAUltima(t *testing.T) {
	ctx := context.Background()
	db := subirBase(t)

	// Base acabada de subir: sem as nossas tabelas.
	if existeTabela(t, db, "catalogo_taxas") || existeTabela(t, db, "limites") {
		t.Fatal("uma base recém-subida não devia ter catalogo_taxas nem limites")
	}

	if err := esquema.Migrar(ctx, db); err != nil {
		t.Fatalf("Migrar: %v", err)
	}
	if !existeTabela(t, db, "catalogo_taxas") || !existeTabela(t, db, "limites") {
		t.Fatal("Migrar devia ter criado catalogo_taxas e limites")
	}

	// Down desfaz a última migração aplicada — a 00002_limites.
	if err := esquema.Reverter(ctx, db); err != nil {
		t.Fatalf("Reverter: %v", err)
	}
	if existeTabela(t, db, "limites") {
		t.Fatal("Reverter devia ter removido limites")
	}
	if !existeTabela(t, db, "catalogo_taxas") {
		t.Fatal("Reverter só devia desfazer a última: catalogo_taxas fica")
	}
}

// O critério de pronto do KAN-1: contra uma base por migrar, ExigirEmDia recusa
// com ErrPorMigrar; depois de migrada, aceita. É o que separa o arranque
// guardado de um create_all silencioso.
func TestExigirEmDiaRecusaBasePorMigrarEAceitaMigrada(t *testing.T) {
	ctx := context.Background()
	db := subirBase(t)

	if err := esquema.ExigirEmDia(ctx, db); !errors.Is(err, esquema.ErrPorMigrar) {
		t.Fatalf("base por migrar: esperava ErrPorMigrar, veio %v", err)
	}

	if err := esquema.Migrar(ctx, db); err != nil {
		t.Fatalf("Migrar: %v", err)
	}
	if err := esquema.ExigirEmDia(ctx, db); err != nil {
		t.Fatalf("base migrada: esperava nil, veio %v", err)
	}
}

// subirBase sobe um Postgres efémero (um por teste — isolamento total, sem
// estado partilhado a limpar) e devolve uma ligação já aberta. Precisa de
// Docker a correr; sem ele, o teste falha em vez de fingir que passou.
func subirBase(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()

	ctr, err := postgres.Run(ctx, "postgres:18.4-alpine",
		postgres.WithDatabase("simulador"),
		postgres.WithUsername("simulador"),
		postgres.WithPassword("simulador"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("subir Postgres de teste (Docker a correr?): %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	db, err := esquema.Ligar(url)
	if err != nil {
		t.Fatalf("Ligar: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func existeTabela(t *testing.T, db *sql.DB, nome string) bool {
	t.Helper()
	var existe bool
	err := db.QueryRow(
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)`, nome,
	).Scan(&existe)
	if err != nil {
		t.Fatalf("existeTabela(%s): %v", nome, err)
	}
	return existe
}
