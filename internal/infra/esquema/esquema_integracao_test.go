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

	// A 00003 acrescenta o intervalo de LTV medido e a 00004 o resíduo da §7.4.
	for _, coluna := range []string{"ltv_min", "ltv_max", "spread_minimo", "residuo_prestacao"} {
		if !existeColuna(t, db, "catalogo_taxas", coluna) {
			t.Errorf("Migrar devia ter criado catalogo_taxas.%s", coluna)
		}
	}

	// Down desfaz a última migração aplicada — hoje a 00004_residuo.
	if err := esquema.Reverter(ctx, db); err != nil {
		t.Fatalf("Reverter: %v", err)
	}
	if existeColuna(t, db, "catalogo_taxas", "residuo_prestacao") {
		t.Error("Reverter devia ter removido catalogo_taxas.residuo_prestacao")
	}
	// ⚠️ E só essa. Um Down que levasse a migração anterior atrás apagaria o
	// intervalo de LTV medido — ~86 pedidos por banco — sem ninguém pedir.
	for _, coluna := range []string{"ltv_min", "ltv_max", "spread_minimo"} {
		if !existeColuna(t, db, "catalogo_taxas", coluna) {
			t.Errorf("Reverter desfez mais do que a última: catalogo_taxas.%s desapareceu", coluna)
		}
	}
	if !existeTabela(t, db, "catalogo_taxas") || !existeTabela(t, db, "limites") {
		t.Fatal("Reverter só devia desfazer a última: as duas tabelas ficam")
	}
}

// Os CHECK da 00003 não são decoração: cada um recusa uma linha que, passando,
// serviria um número errado com ar de certo (§7.4). Prova-se um a um contra o
// Postgres, porque é ele que os impõe — em Go não há como os ver falhar.
func TestOsCheckDoIntervaloDeLTVRecusamOQueNaoEUmDegrau(t *testing.T) {
	ctx := context.Background()
	db := subirBase(t)
	if err := esquema.Migrar(ctx, db); err != nil {
		t.Fatalf("Migrar: %v", err)
	}

	for _, caso := range []struct {
		nome           string
		ltvMin, ltvMax any
		spread         any
		spreadMinimo   any
		aceita         bool
	}{
		{
			nome:   "um degrau resolvido, com o intervalo inteiro",
			ltvMin: "0.66593750", ltvMax: "0.67875000", spread: "2.050", spreadMinimo: nil,
			aceita: true,
		},
		{
			nome:   "um degrau por resolver, com o lado barato guardado",
			ltvMin: "0.66500000", ltvMax: "0.66750000", spread: "2.050", spreadMinimo: "2.000",
			aceita: true,
		},
		{
			nome:   "uma observação num ponto: sem intervalo nenhum",
			ltvMin: nil, ltvMax: nil, spread: "1.350", spreadMinimo: nil,
			aceita: true,
		},
		{
			nome:   "meio intervalo — um degrau sem fim, e ninguém sabe até onde o spread vale",
			ltvMin: "0.66500000", ltvMax: nil, spread: "2.050", spreadMinimo: nil,
			aceita: false,
		},
		{
			nome:   "um intervalo ao contrário",
			ltvMin: "0.68000000", ltvMax: "0.66000000", spread: "2.050", spreadMinimo: nil,
			aceita: false,
		},
		{
			nome:   "um intervalo de largura zero — afirmava que o preço muda num ponto",
			ltvMin: "0.80000000", ltvMax: "0.80000000", spread: "1.350", spreadMinimo: nil,
			aceita: false,
		},
		{
			// ⚠️ O caso que interessa: servir o lado BARATO num intervalo onde
			// não se sabe qual é. É o oposto do que o Anexo I, Parte II, alínea
			// (d) da MCD manda, e é a mesma guarda que o NovaEscalaDeLTV faz.
			nome:   "o spread_minimo é o lado caro",
			ltvMin: "0.66500000", ltvMax: "0.66750000", spread: "2.000", spreadMinimo: "2.050",
			aceita: false,
		},
		{
			nome:   "spread_minimo numa linha que não é degrau",
			ltvMin: nil, ltvMax: nil, spread: "2.000", spreadMinimo: "1.500",
			aceita: false,
		},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			err := inserirLinha(db, caso.ltvMin, caso.ltvMax, caso.spread, caso.spreadMinimo)
			if caso.aceita && err != nil {
				t.Errorf("a base recusou uma linha válida: %v", err)
			}
			if !caso.aceita && err == nil {
				t.Error("a base aceitou a linha, e um CHECK devia tê-la recusado")
			}
		})
	}
}

// A precisão declarada tem de chegar para as fronteiras que a descoberta
// produz: sete casas decimais no pior caso do plano de omissão (duas do domínio
// mais cinco bissecções). Em numeric(6,3) — o que a §4 declarava até
// 2026-07-27 — a fronteira de 0,6659375 ficava 0,666, e os ~18 pedidos gastos a
// estreitá-la não tinham servido para nada.
func TestAPrecisaoDoLTVGuardaAFronteiraQueSeMediu(t *testing.T) {
	ctx := context.Background()
	db := subirBase(t)
	if err := esquema.Migrar(ctx, db); err != nil {
		t.Fatalf("Migrar: %v", err)
	}

	const medida = "0.66593750" // fronteira real da CGD, medida a 2026-07-27
	if err := inserirLinha(db, medida, "0.67875000", "2.050", nil); err != nil {
		t.Fatalf("inserir: %v", err)
	}

	var guardado string
	if err := db.QueryRow(`SELECT ltv_min::text FROM catalogo_taxas LIMIT 1`).Scan(&guardado); err != nil {
		t.Fatalf("ler: %v", err)
	}
	if guardado != medida {
		t.Errorf("mediu-se %s e a base guardou %s", medida, guardado)
	}
}

// inserirLinha grava uma observação de sucesso, variando só o que este ficheiro
// afirma. O resto dos campos é preenchimento válido: o que está a teste são os
// CHECK do intervalo de LTV, não os que a 00001 já trazia.
func inserirLinha(db *sql.DB, ltvMin, ltvMax, spread, spreadMinimo any) error {
	_, err := db.Exec(`
		INSERT INTO catalogo_taxas (
			varrimento_id, capturado_em, cenario, banco_id, banco_nome, rate_type,
			valor_imovel, montante, prazo_anos, euribor_indexante,
			tan, taeg, spread, euribor_valor, prestacao_mensal, mtic,
			ltv_min, ltv_max, spread_minimo, sucesso
		) VALUES (
			gen_random_uuid(), now(), 'variavel/0/propria', 'cgd', 'Caixa Geral de Depósitos', 'variavel',
			400000, 320000, 30, '6m',
			4.5, 4.7, $3, 2.45, 1600, 600000,
			$1, $2, $4, true
		)`, ltvMin, ltvMax, spread, spreadMinimo)
	return err
}

func existeColuna(t *testing.T, db *sql.DB, tabela, coluna string) bool {
	t.Helper()
	var existe bool
	err := db.QueryRow(
		`SELECT EXISTS (
			SELECT FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)`, tabela, coluna,
	).Scan(&existe)
	if err != nil {
		t.Fatalf("existeColuna(%s.%s): %v", tabela, coluna, err)
	}
	return existe
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
