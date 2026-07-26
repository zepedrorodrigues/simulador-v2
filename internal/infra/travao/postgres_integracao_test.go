package travao_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/travao"
)

// O critério de pronto do KAN-7: dois arranques concorrentes sobre o mesmo
// banco, e só um corre.
//
// ⚠️ Os dois arranques são dois travões construídos à parte, cada um com o seu
// pool. É o que dois workers são — e é o que reprova o defeito que o v1 avisava
// no arranque: um travão em variável de processo é uma instância por worker, e
// duas instâncias não se travam uma à outra. Só um travão fora do processo
// trava os dois, e é por isso que ele vive na base.
func TestDoisArranquesSobreOMesmoBancoESoUmCorre(t *testing.T) {
	ctx := context.Background()
	url := subirBase(t)

	primeiro := travao.NovoPostgres(abrirPool(t, url))
	segundo := travao.NovoPostgres(abrirPool(t, url))

	largar, err := primeiro.Tomar(ctx, "cgd")
	if err != nil {
		t.Fatalf("o primeiro arranque devia ter tomado o travão: %v", err)
	}

	if _, err := segundo.Tomar(ctx, "cgd"); !errors.Is(err, varrimento.ErrBancoTravado) {
		t.Fatalf("o segundo arranque devia ter encontrado a cgd travada, veio %v", err)
	}

	// Largo o primeiro: o banco fica livre para a corrida seguinte, e o travão
	// não é uma marca que fica lá.
	largar()

	largarSegundo, err := segundo.Tomar(ctx, "cgd")
	if err != nil {
		t.Fatalf("largado o primeiro, o segundo devia poder tomar: %v", err)
	}
	largarSegundo()
}

// O travão é por banco e não pelo varrimento inteiro: dois bancos diferentes
// varrem-se ao mesmo tempo, que é o desenho todo.
func TestBancosDiferentesNaoSeTravamUmAoOutro(t *testing.T) {
	ctx := context.Background()
	url := subirBase(t)

	tr := travao.NovoPostgres(abrirPool(t, url))

	largarCGD, err := tr.Tomar(ctx, "cgd")
	if err != nil {
		t.Fatalf("cgd: %v", err)
	}
	defer largarCGD()

	largarNB, err := tr.Tomar(ctx, "novobanco")
	if err != nil {
		t.Fatalf("o novobanco não devia estar travado pela cgd: %v", err)
	}
	largarNB()
}

// ⚠️ Largar depois de o varrimento ser cancelado ainda larga. É o segundo
// medido de 2026-07-25: um ctx derivado de um pai que termina fica `context
// canceled` no mesmo instante, e sem o context.WithoutCancel o UNLOCK nunca
// chegava à base — o banco ficava travado até a sessão morrer.
func TestLargarFuncionaComOCtxDoVarrimentoJaCancelado(t *testing.T) {
	url := subirBase(t)
	tr := travao.NovoPostgres(abrirPool(t, url))

	ctx, cancelar := context.WithCancel(context.Background())
	largar, err := tr.Tomar(ctx, "cgd")
	if err != nil {
		t.Fatalf("Tomar: %v", err)
	}

	cancelar()
	largar()

	segundo, err := tr.Tomar(context.Background(), "cgd")
	if err != nil {
		t.Fatalf("depois de largado com o ctx cancelado, a cgd devia estar livre: %v", err)
	}
	segundo()
}

// abrirPool abre um pool próprio contra a mesma base. Cada pool é um arranque
// independente — é o que faz deste teste uma afirmação sobre dois workers e não
// sobre duas chamadas.
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
// advisory lock não precisa de tabela nenhuma, e é essa uma das razões de ser
// ele e não uma linha. Precisa de Docker a correr; sem ele, o teste falha em
// vez de fingir que passou.
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
