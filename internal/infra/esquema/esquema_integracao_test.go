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
	if existeTabela(t, db, "limites") || existeTabela(t, db, "respostas_em_cache") {
		t.Fatal("uma base recém-subida não devia ter limites nem respostas_em_cache")
	}

	if err := esquema.Migrar(ctx, db); err != nil {
		t.Fatalf("Migrar: %v", err)
	}

	// ⚠️ **As três tabelas da §4, e são as únicas**: o tecto por IP, a cache das
	// respostas ao vivo, e os catálogos que os bancos publicam (KAN-36). Eram duas
	// até 2026-08-14 — a terceira entrou com a §4 alterada primeiro, que é o que
	// ela manda.
	for _, viva := range []string{"limites", "respostas_em_cache", "catalogos_de_banco"} {
		if !existeTabela(t, db, viva) {
			t.Errorf("Migrar devia ter criado %s", viva)
		}
	}

	// ⚠️ **E as do varrimento têm de estar mesmo apagadas no fim da cadeia.** A
	// 00001 e a 00006 criam-nas e a 00008 deixa-as cair; o que importa é o estado
	// depois de migrar tudo, que é o que uma base nova recebe.
	//
	// ⚠️ O `PLAN.md` conta «tabelas com nome de funcionalidade morta» como
	// indicador de entulho, e o v1 tinha 1 em 3. Isto é o teste desse indicador.
	for _, morta := range []string{"catalogo_taxas", "sondagens"} {
		if existeTabela(t, db, morta) {
			t.Errorf("a %s sobreviveu à 00008 — morreu com o varrimento", morta)
		}
	}

	// Down desfaz **uma** migração de cada vez, a última aplicada. São duas
	// reversões porque a cadeia cresceu: a 00009 dos catálogos entrou por cima da
	// 00008 do varrimento, e cada uma tem a sua afirmação.
	//
	// ⚠️ Isto apanhou a 00009 a entrar: o teste dizia «o Down da 00008» com a
	// 00008 já não a ser a última, e passou a falhar a dizer isso mesmo. É o
	// comportamento que se quer de um teste preso à cabeça da cadeia.

	// A primeira reversão leva a 00009, e só ela.
	if err := esquema.Reverter(ctx, db); err != nil {
		t.Fatalf("Reverter a 00009: %v", err)
	}
	if existeTabela(t, db, "catalogos_de_banco") {
		t.Error("o Down da 00009 devia ter deixado cair a catalogos_de_banco")
	}
	if !existeTabela(t, db, "respostas_em_cache") || !existeTabela(t, db, "limites") {
		t.Error("Reverter desfez mais do que a última: levou uma tabela de outra migração")
	}

	// A segunda leva a 00008. ⚠️ E o Down dela **recria a forma e não os dados**,
	// que é o que uma migração de remoção pode prometer. O que se afirma aqui é
	// que ela é reversível sem partir, não que a série volta.
	if err := esquema.Reverter(ctx, db); err != nil {
		t.Fatalf("Reverter a 00008: %v", err)
	}
	if !existeTabela(t, db, "catalogo_taxas") || !existeTabela(t, db, "sondagens") {
		t.Error("o Down da 00008 devia ter recriado as tabelas vazias")
	}
	// ⚠️ E só essa: a `respostas_em_cache` é da 00007 e um Down a mais levava-a.
	if !existeTabela(t, db, "respostas_em_cache") {
		t.Error("Reverter desfez mais do que a última: a respostas_em_cache desapareceu")
	}
	if !existeTabela(t, db, "limites") {
		t.Error("Reverter desfez mais do que a última: a limites desapareceu")
	}
}

// ⚠️ **Havia aqui dois testes dos CHECK da `catalogo_taxas`** — o intervalo de
// LTV e a precisão de numeric(9,8) —, e a tabela caiu com o varrimento na 00008
// (Fase 6, passo 5). Iam com ela: um CHECK de uma tabela que não existe não é
// afirmável, e mantê-los obrigava a recriar a tabela só para os correr.
//
// ⚠️ O que se perdeu com eles foi a **prova de que a base recusa**, e não a
// razão: ela está escrita na 00003, que continua no histórico. O que fica de pé
// é o teste acima, que agora também vigia o Down desta remoção.

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
