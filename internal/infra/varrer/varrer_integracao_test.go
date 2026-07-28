package varrer_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/varrer"
)

// ⚠️ Este ficheiro monta os bancos A SÉRIO — é o que o subcomando faz — e
// nenhum destes testes deve deixar o varrimento chegar à rede. Cada um trava
// antes: dois pela guarda de idempotência, um por o id do banco não existir.
//
// ⚠️ E é preciso dizer o que isso NÃO garante. Construir os bancos não faz
// pedido nenhum — os construtores só guardam o transporte —, mas se alguém
// partir a guarda, o primeiro teste deixa de travar e passa a bater nos
// simuladores públicos uma vez, com os 66 pontos dos três bancos. O risco é
// esse, é limitado, e aceita-se porque o que este teste afirma é precisamente
// que a guarda impede a rede: um teste que não corresse o caminho a sério não
// afirmava nada sobre ele.
//
// A corrida a sério contra os bancos vive atrás de `//go:build rede`, como as
// de fidelidade, e não entra no `make verificar`.

func TestComAGuardaATravarNaoSeFalaComBancoNenhum(t *testing.T) {
	url, pool := subirBase(t)

	// Uma linha acabada de gravar põe o último varrimento «agora».
	semear(t, pool)

	antes := contarLinhas(t, pool)

	rel, err := varrer.Correr(t.Context(), url, varrer.Opcoes{SeAntigo: 6 * time.Hour})
	if err != nil {
		t.Fatalf("Correr: %v", err)
	}

	if !rel.Saltado {
		t.Fatal("a guarda não travou, e o varrimento foi à rede com os bancos a sério")
	}
	if depois := contarLinhas(t, pool); depois != antes {
		t.Errorf("gravou %d linhas com a guarda a travar", depois-antes)
	}
	if !strings.Contains(rel.Motivo, "recente") {
		t.Errorf("o motivo não diz que o último é recente: %q", rel.Motivo)
	}
}

func TestOSubcomandoContaOsPontosDeCadaBancoAntesDeCorrer(t *testing.T) {
	url, pool := subirBase(t)
	semear(t, pool)

	// ⚠️ A contagem é anterior à guarda de propósito: quem dispara à mão quer
	// saber o que a corrida ia custar, mesmo quando ela não corre. E o número
	// sai dos Requisitos de cada banco, sem tocar na rede.
	rel, err := varrer.Correr(t.Context(), url, varrer.Opcoes{SeAntigo: 6 * time.Hour})
	if err != nil {
		t.Fatalf("Correr: %v", err)
	}

	// Banco CTT 14 + CGD 20 + Montepio 23 + Novo Banco 29, medidos no
	// grelha.Pontos. ⚠️ O Banco CTT é o mais barato dos quatro porque tem o
	// leque mais curto: quatro períodos de mista, dois prazos de fixa, e um
	// indexante que não é escolhível.
	//
	// ⚠️ Cada banco leva mais **dois** pontos desde a 7.ª família (o prazo, nos
	// extremos que cada um serve). Não são para medir o spread — está medido que
	// ele não depende do prazo — mas a TAEG depende, e é da distância entre dois
	// prazos que a repartição dos encargos se torna identificável. Dois pedidos
	// por banco é o que separa uma TAEG medida de uma TAEG assumida.
	// ⚠️ O Santander (KAN-18) traz 10: uma variável, três mistas, três fixas, a
	// finalidade que ele não usa e os dois extremos de prazo. É o segundo mais
	// barato, e a razão é a mesma do Banco CTT — leque curto e indexante
	// imposto.
	const esperados = 14 + 20 + 23 + 29 + 10
	if rel.Pontos != esperados {
		t.Errorf("%d pontos, esperava %d — se um banco ganhou período fixo ou produto, é aqui que se vê",
			rel.Pontos, esperados)
	}
	if len(rel.Bancos) != 5 {
		t.Errorf("bancos = %v, esperava os cinco registados", rel.Bancos)
	}
}

func TestUmBancoDesconhecidoRecusaENomeiaOsQueExistem(t *testing.T) {
	url, _ := subirBase(t)

	// Errar o id não pode dar um varrimento silenciosamente mais pequeno: o
	// erro nomeia os que existem, para o engano se resolver na primeira leitura.
	_, err := varrer.Correr(t.Context(), url, varrer.Opcoes{Bancos: []string{"caixa"}})
	if err == nil {
		t.Fatal("um id desconhecido passou")
	}
	for _, id := range []string{"cgd", "montepio", "novobanco"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("o erro não nomeia o banco registado %q: %v", id, err)
		}
	}
}

// ------------------------------------------------------------------ ajudantes

// semear grava uma linha com capturado_em = agora, para a guarda ter contra o
// que decidir. É SQL directo e não o catálogo: o que está a teste é a guarda do
// subcomando, e não a escrita — essa tem os seus testes em infra/catalogo.
func semear(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO catalogo_taxas (
			varrimento_id, capturado_em, cenario, banco_id, banco_nome, rate_type,
			valor_imovel, montante, prazo_anos, euribor_indexante,
			tan, taeg, spread, euribor_valor, prestacao_mensal, mtic, sucesso
		) VALUES (
			gen_random_uuid(), now(), 'variavel/0/propria', 'cgd', 'Caixa Geral de Depósitos', 'variavel',
			400000, 320000, 30, '6m', 4.5, 4.7, 1.35, 2.45, 1600, 600000, true
		)`)
	if err != nil {
		t.Fatalf("semear: %v", err)
	}
}

func contarLinhas(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM catalogo_taxas`).Scan(&n); err != nil {
		t.Fatalf("contar: %v", err)
	}
	return n
}

func subirBase(t *testing.T) (string, *pgxpool.Pool) {
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
	return url, pool
}
