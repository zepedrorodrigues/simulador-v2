package lotacao_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/aovivo"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/lotacao"
)

// TestDuasVagasDeixamDoisEntrarEOTerceiroDesiste é o que separa um tecto de um
// fecho, e é a diferença entre esta issue e o travão do varrimento.
//
// ⚠️ Reverter para uma chave só por banco — tirar o número da vaga do `chave` —
// faz o SEGUNDO entrar falhar, e o teste diz que o tecto de 2 se comporta como
// um fecho de 1. Que é exactamente o defeito que se veio corrigir.
func TestDuasVagasDeixamDoisEntrarEOTerceiroDesiste(t *testing.T) {
	ctx := context.Background()
	lot := lotacao.NovoPostgres(abrirPool(t, subirBase(t)), 2)

	primeiro := entrar(t, lot, "cgd", "a primeira vaga devia estar livre")
	entrar(t, lot, "cgd", "com duas vagas, dois clientes perguntam ao mesmo banco ao mesmo tempo")

	if _, err := lot.Entrar(ctx, "cgd"); !errors.Is(err, aovivo.ErrSemVaga) {
		t.Fatalf("o terceiro devia ter encontrado a cgd cheia, veio %v", err)
	}

	// Sair devolve a vaga a quem vier a seguir: o tecto é um lugar emprestado, e
	// não uma marca que fica lá.
	primeiro()
	entrar(t, lot, "cgd", "largada uma vaga, o seguinte devia entrar")
}

// TestBancosDiferentesNaoDisputamAsMesmasVagas: o tecto é POR banco. Um Montepio
// lento — 8,4 s medidos na cauda a 2026-08-06 — não pode ocupar o lugar de quem
// vai perguntar à CGD.
func TestBancosDiferentesNaoDisputamAsMesmasVagas(t *testing.T) {
	lot := lotacao.NovoPostgres(abrirPool(t, subirBase(t)), 1)

	entrar(t, lot, "cgd", "cgd")
	entrar(t, lot, "montepio", "o montepio não devia disputar a vaga da cgd")
}

// TestDoisProcessosPartilhamOTecto é a razão de isto viver na base.
//
// ⚠️ Duas lotações construídas à parte, cada uma com o seu pool, é o que dois
// workers são — e é o defeito que o v1 avisava no arranque: um tecto em variável
// de processo é uma instância por worker, e duas instâncias não se travam uma à
// outra. Com o tecto a 1, o segundo processo tem de encontrar o banco cheio.
func TestDoisProcessosPartilhamOTecto(t *testing.T) {
	ctx := context.Background()
	url := subirBase(t)

	primeiro := lotacao.NovoPostgres(abrirPool(t, url), 1)
	segundo := lotacao.NovoPostgres(abrirPool(t, url), 1)

	sair := entrar(t, primeiro, "cgd", "o primeiro processo devia ter entrado")

	if _, err := segundo.Entrar(ctx, "cgd"); !errors.Is(err, aovivo.ErrSemVaga) {
		t.Fatalf("o segundo processo devia ter encontrado a cgd cheia, veio %v", err)
	}

	sair()
	entrar(t, segundo, "cgd", "largada a vaga pelo outro processo, este devia entrar")
}

// TestSairComOCtxCanceladoDevolveAVagaEALigacao mede o `context.WithoutCancel`, e
// são duas afirmações e não uma.
//
// ⚠️ A primeira sozinha passa com o defeito reposto: sem o ctx destacado o
// UNLOCK não chega à base, o código mata a ligação, e o Postgres larga o lock com
// a sessão — a vaga volta pelo caminho caro. O que muda é o preço, e é a segunda
// asserção que o vê: **cada pedido cancelado deitava fora uma ligação**. Ao vivo
// isso conta por pedido de cliente, e não por corrida de varrimento.
func TestSairComOCtxCanceladoDevolveAVagaEALigacao(t *testing.T) {
	pool := abrirPool(t, subirBase(t))
	lot := lotacao.NovoPostgres(pool, 2)

	ctx, cancelar := context.WithCancel(context.Background())
	sair, err := lot.Entrar(ctx, "cgd")
	if err != nil {
		t.Fatalf("Entrar: %v", err)
	}

	cancelar()
	sair()

	if total := pool.Stat().TotalConns(); total != 1 {
		t.Errorf(
			"esperava a ligação de volta ao pool, há %d: o UNLOCK não chegou à base e a ligação foi morta",
			total)
	}

	seguinte, err := lot.Entrar(context.Background(), "cgd")
	if err != nil {
		t.Fatalf("depois de sair com o ctx cancelado, a cgd devia ter vaga: %v", err)
	}
	seguinte()
}

// TestUmTectoAbaixoDeUmNaoDesligaOTecto: zero vagas seria não servir banco
// nenhum, e ninguém quer dizer isso por engano — vale o de omissão.
func TestUmTectoAbaixoDeUmNaoDesligaOTecto(t *testing.T) {
	lot := lotacao.NovoPostgres(abrirPool(t, subirBase(t)), 0)
	if lot.Vagas() != lotacao.VagasOmissao {
		t.Errorf("vagas %d, esperava o de omissão (%d)", lot.Vagas(), lotacao.VagasOmissao)
	}
}

// entrar toma uma vaga e garante que ela volta, **mesmo que o teste falhe a
// meio**.
//
// ⚠️ Não é arrumação: uma vaga por devolver segura uma ligação, e o
// `pool.Close` do cleanup fica à espera dela. Medido aqui a 2026-08-06, a
// reverter o `chave` para uma chave só por banco: o teste que devia falhar a
// dizer «com duas vagas, dois clientes perguntam ao mesmo banco» **pendurou-se**
// — o `t.Fatalf` saltou o `sair()` que vinha a seguir. Um teste que se pendura
// em vez de falhar não nomeia coisa nenhuma. Os cleanups correm ao contrário da
// ordem em que se registam, e o do pool é o primeiro: as vagas saem todas antes
// de ele fechar.
func entrar(t *testing.T, lot *lotacao.Postgres, banco, porque string) func() {
	t.Helper()
	sair, err := lot.Entrar(context.Background(), banco)
	if err != nil {
		t.Fatalf("%s: %v", porque, err)
	}
	uma := sync.OnceFunc(sair)
	t.Cleanup(uma)
	return uma
}

// abrirPool abre um pool próprio contra a mesma base. Cada pool é um arranque
// independente — é o que faz do TestDoisProcessos uma afirmação sobre dois
// workers e não sobre duas chamadas.
func abrirPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("abrir pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// subirBase sobe um Postgres efémero e devolve a sua URL. Não migra nada: um
// advisory lock não precisa de tabela nenhuma. Precisa de Docker a correr; sem
// ele, o teste falha em vez de fingir que passou.
func subirBase(t *testing.T) string {
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
	return url
}
