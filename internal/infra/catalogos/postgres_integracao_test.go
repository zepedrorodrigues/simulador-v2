package catalogos_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/catalogos"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
)

// A loja de catálogos contra Postgres a sério (KAN-36).
//
// ⚠️ Contra a base e não contra um duplo: o que aqui se afirma é a validade a
// filtrar na consulta e o `ON CONFLICT` a renovar — as duas coisas são SQL, e um
// duplo em memória diria que sim a ambas sem as ter.

// TestUmCatalogoExpiradoNaoSeServe.
//
// ⚠️ **É o defeito que esta tabela pode causar, e por isso é o primeiro teste.**
// A limpeza corre por manutenção e pode não ter corrido; se a validade só
// vivesse lá, uma entrada velha era servida na janela entre expirar e ser
// apagada — e servir um catálogo velho não é lentidão, é propor a alguém um
// período que o banco deixou de praticar.
func TestUmCatalogoExpiradoNaoSeServe(t *testing.T) {
	pool := subirBase(t)
	agora := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	relogio := func() time.Time { return agora }

	loja := catalogos.NovoPostgres(pool, time.Hour, relogio, nil)
	loja.Guardar(t.Context(), "cgd", "periodos", []byte(`{"fixa":{"10":"5"}}`))

	if _, achou := loja.Ler(t.Context(), "cgd", "periodos"); !achou {
		t.Fatal("acabou de se guardar e não se lê")
	}

	// O relógio avança para lá da validade. A limpeza NÃO corre de propósito.
	agora = agora.Add(time.Hour + time.Minute)
	if _, achou := loja.Ler(t.Context(), "cgd", "periodos"); achou {
		t.Error("serviu-se um catálogo expirado: a validade não está a filtrar na consulta")
	}
}

// TestGuardarOMesmoCatalogoRenovaAValidade.
//
// ⚠️ Com `ON CONFLICT DO NOTHING` isto passava a meio: a leitura encontrava a
// entrada velha, a validade nunca se estendia, e a página voltava a ser pedida a
// cada simulação — que é exactamente o defeito que a tabela existe para corrigir.
func TestGuardarOMesmoCatalogoRenovaAValidade(t *testing.T) {
	pool := subirBase(t)
	agora := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	relogio := func() time.Time { return agora }

	loja := catalogos.NovoPostgres(pool, time.Hour, relogio, nil)
	loja.Guardar(t.Context(), "cgd", "periodos", []byte(`{"v":1}`))

	// Meia hora depois, lê-se da CGD outra vez e grava-se.
	agora = agora.Add(30 * time.Minute)
	loja.Guardar(t.Context(), "cgd", "periodos", []byte(`{"v":2}`))

	// Mais 59 min: a entrada ORIGINAL já expirou (era T0+1h), a segunda ainda não
	// (é T0+1h30). ⚠️ 59 e não 60: o `expira_em > agora` é estrito, e avançar para
	// o instante exacto da expiração media a fronteira em vez da renovação — foi
	// o que fez este teste falhar à primeira.
	agora = agora.Add(59 * time.Minute)
	valor, achou := loja.Ler(t.Context(), "cgd", "periodos")
	if !achou {
		t.Fatal("a segunda gravação não estendeu a validade")
	}
	if !bytes.Contains(valor, []byte(`"v"`)) || !bytes.Contains(valor, []byte(`2`)) {
		t.Errorf("valor %q, esperava o segundo: a gravação não substituiu", valor)
	}
}

// TestCadaBancoTemOSeuCatalogo: a chave é (banco_id, nome), e um banco não lê o
// do outro.
//
// ⚠️ **Compara-se o CONTEÚDO e não o texto.** A coluna é `jsonb` e o Postgres
// normaliza o que lá entra — `{"de":"cgd"}` volta como `{"de": "cgd"}`, com
// espaço. Um teste que comparasse bytes falhava por causa de um espaço que não é
// do contrato, e foi o que aconteceu à primeira versão deste.
func TestCadaBancoTemOSeuCatalogo(t *testing.T) {
	pool := subirBase(t)
	relogio := func() time.Time { return time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC) }
	loja := catalogos.NovoPostgres(pool, time.Hour, relogio, nil)

	loja.Guardar(t.Context(), "cgd", "periodos", []byte(`{"de":"cgd"}`))
	loja.Guardar(t.Context(), "novobanco", "periodos", []byte(`{"de":"novobanco"}`))

	valor, achou := loja.Ler(t.Context(), "cgd", "periodos")
	if !achou || !bytes.Contains(valor, []byte(`"cgd"`)) {
		t.Errorf("a CGD leu %q (achou=%v): os catálogos estão a misturar-se", valor, achou)
	}
	if _, achou := loja.Ler(t.Context(), "montepio", "periodos"); achou {
		t.Error("um banco sem catálogo leu o de outro")
	}
}

// TestLimparApagaSoOExpirado.
func TestLimparApagaSoOExpirado(t *testing.T) {
	pool := subirBase(t)
	agora := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	relogio := func() time.Time { return agora }

	curta := catalogos.NovoPostgres(pool, time.Minute, relogio, nil)
	longa := catalogos.NovoPostgres(pool, 24*time.Hour, relogio, nil)
	curta.Guardar(t.Context(), "cgd", "periodos", []byte(`{}`))
	longa.Guardar(t.Context(), "novobanco", "configuracoes", []byte(`{}`))

	agora = agora.Add(2 * time.Minute)
	apagadas, err := curta.Limpar(t.Context())
	if err != nil {
		t.Fatalf("Limpar: %v", err)
	}
	if apagadas != 1 {
		t.Errorf("apagou %d linhas, esperava 1 — só a expirada", apagadas)
	}
	if _, achou := longa.Ler(t.Context(), "novobanco", "configuracoes"); !achou {
		t.Error("a limpeza levou uma entrada que ainda era válida")
	}
}

// TestUmaLojaSemBaseNaoDerrubaOPedido.
//
// ⚠️ **É a decisão de falhar ABERTO, afirmada.** Um catálogo que não se lê é uma
// ida à fonte — o que acontecia sempre antes desta tabela existir. Se isto
// devolvesse erro, ou entrasse em pânico, uma avaria nossa virava a
// impossibilidade de servir aquele banco.
func TestUmaLojaSemBaseNaoDerrubaOPedido(t *testing.T) {
	t.Parallel()

	pool := subirBase(t)
	relogio := func() time.Time { return time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC) }
	loja := catalogos.NovoPostgres(pool, time.Hour, relogio, nil)
	loja.Guardar(t.Context(), "cgd", "periodos", []byte(`{}`))
	pool.Close() // a base desaparece debaixo dos pés

	if _, achou := loja.Ler(t.Context(), "cgd", "periodos"); achou {
		t.Error("disse que achou com a base fechada")
	}
	// E o Guardar também não pode explodir.
	loja.Guardar(t.Context(), "cgd", "periodos", []byte(`{}`))
}

func subirBase(t *testing.T) *pgxpool.Pool {
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
	defer func() { _ = db.Close() }()
	if err := esquema.Migrar(ctx, db); err != nil {
		t.Fatalf("Migrar: %v", err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("abrir o pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
