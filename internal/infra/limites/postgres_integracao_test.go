package limites_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/limites"
)

// ⚠️ **Estes testes não existiam.** O contador do tecto por IP vivia dentro do
// `infra/catalogo` e só era exercitado por um duplo em memória, nos testes do
// `web` — ou seja, a instrução SQL que o torna correcto sob concorrência nunca
// tinha corrido contra um Postgres. Mudá-lo de sítio (2026-08-07) tornou a falta
// visível, e é aqui que se fecha.

// TestPedidosNaMesmaJanelaSomam: o caso base, contra a tabela a sério.
func TestPedidosNaMesmaJanelaSomam(t *testing.T) {
	c := limites.NovoPostgres(subirBase(t))
	agora := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	for esperado := 1; esperado <= 3; esperado++ {
		n, err := c.ContarPedido(t.Context(), "http:ip:203.0.113.9", time.Minute, agora)
		if err != nil {
			t.Fatalf("ContarPedido: %v", err)
		}
		if n != esperado {
			t.Errorf("contagem %d, esperava %d", n, esperado)
		}
	}
}

// TestUmaJanelaNovaRecomecaAContagem: a janela é FIXA e não deslizante — quando
// a actual expira, começa uma nova com contagem 1.
//
// ⚠️ Uma janela deslizante exigiria guardar cada pedido, e isto é uma tabela de
// contadores e não um registo de quem nos visitou.
func TestUmaJanelaNovaRecomecaAContagem(t *testing.T) {
	c := limites.NovoPostgres(subirBase(t))
	inicio := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	for range 3 {
		if _, err := c.ContarPedido(t.Context(), "http:ip:203.0.113.9", time.Minute, inicio); err != nil {
			t.Fatalf("ContarPedido: %v", err)
		}
	}

	// Ainda dentro da janela, continua a somar.
	n, err := c.ContarPedido(t.Context(), "http:ip:203.0.113.9", time.Minute, inicio.Add(59*time.Second))
	if err != nil {
		t.Fatalf("ContarPedido: %v", err)
	}
	if n != 4 {
		t.Errorf("dentro da janela a contagem foi %d, esperava 4", n)
	}

	// Passada a janela, recomeça.
	n, err = c.ContarPedido(t.Context(), "http:ip:203.0.113.9", time.Minute, inicio.Add(61*time.Second))
	if err != nil {
		t.Fatalf("ContarPedido: %v", err)
	}
	if n != 1 {
		t.Errorf("na janela seguinte a contagem foi %d, esperava recomeçar em 1", n)
	}
}

// TestChavesDiferentesNaoSePisam: é o isolamento entre clientes, na base.
func TestChavesDiferentesNaoSePisam(t *testing.T) {
	c := limites.NovoPostgres(subirBase(t))
	agora := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	for range 5 {
		if _, err := c.ContarPedido(t.Context(), "http:ip:203.0.113.10", time.Minute, agora); err != nil {
			t.Fatalf("ContarPedido A: %v", err)
		}
	}

	n, err := c.ContarPedido(t.Context(), "http:ip:203.0.113.20", time.Minute, agora)
	if err != nil {
		t.Fatalf("ContarPedido B: %v", err)
	}
	if n != 1 {
		t.Errorf("a segunda chave começou em %d: os contadores estão a partilhar linha", n)
	}
}

// TestSobConcorrenciaNenhumPedidoSePerde é a afirmação que o comentário do
// `limites.sql` faz e que ninguém tinha medido.
//
// ⚠️ **É o valor inteiro de a contagem ser uma instrução só.** Um SELECT seguido
// de UPDATE deixa duas ligações a lerem a mesma contagem e a escreverem a mesma
// soma: pedidos perdem-se, a contagem fica abaixo do real, e o tecto passa a
// valer o dobro — precisamente sob a carga que ele existe para travar. Reverter
// a query para ler-e-depois-escrever faz este teste dizer quantos pedidos se
// perderam.
func TestSobConcorrenciaNenhumPedidoSePerde(t *testing.T) {
	c := limites.NovoPostgres(subirBase(t))
	agora := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	const emParalelo = 30
	var espera sync.WaitGroup
	erros := make(chan error, emParalelo)
	vistas := make(chan int, emParalelo)

	for range emParalelo {
		espera.Add(1)
		go func() {
			defer espera.Done()
			n, err := c.ContarPedido(context.Background(), "http:ip:203.0.113.99", time.Minute, agora)
			if err != nil {
				erros <- err
				return
			}
			vistas <- n
		}()
	}
	espera.Wait()
	close(erros)
	close(vistas)

	for err := range erros {
		t.Fatalf("ContarPedido em paralelo: %v", err)
	}

	// ⚠️ Cada pedido tem de ver um número DIFERENTE, e juntos têm de dar 1..N.
	// Duas ligações a verem a mesma contagem é exactamente o defeito.
	visto := map[int]bool{}
	maior := 0
	for n := range vistas {
		if visto[n] {
			t.Errorf("a contagem %d saiu duas vezes: dois pedidos leram o mesmo valor", n)
		}
		visto[n] = true
		if n > maior {
			maior = n
		}
	}
	if maior != emParalelo {
		t.Errorf("a contagem mais alta foi %d com %d pedidos em paralelo: perderam-se %d",
			maior, emParalelo, emParalelo-maior)
	}
}

// subirBase sobe um Postgres efémero e migra-o: isto é uma TABELA, ao contrário
// do tecto por banco, que são advisory locks. Precisa de Docker a correr; sem
// ele, o teste falha em vez de fingir que passou.
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
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
