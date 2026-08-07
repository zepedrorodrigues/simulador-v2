package cache_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/cache"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
)

// TestOPedidoEmClaroNaoChegaAoDisco é o critério de privacidade da KAN-58 visto
// onde ele importa: na linha gravada, e não só na chave.
//
// ⚠️ Reverter a derivação — gravar o pedido serializado como chave — faz este
// teste encontrar a data de nascimento na tabela. É a §4 em código: um pedido
// traz data de nascimento e rendimento de alguém, e nada disso toca no disco.
func TestOPedidoEmClaroNaoChegaAoDisco(t *testing.T) {
	pool := subirBase(t)
	c := cache.NovoPostgres(pool, time.Minute)
	agora := time.Date(2026, 8, 7, 11, 0, 0, 0, time.UTC)

	pedido := pedidoDe(t, "1987-03-14", "4321.50", "250000")
	chave := chaveDe(t, "cgd", pedido)
	if err := c.Gravar(t.Context(), chave, "cgd", ofertaServida(agora), agora); err != nil {
		t.Fatalf("Gravar: %v", err)
	}

	// A linha inteira, como texto: chave, banco, resposta e as datas.
	var linha string
	if err := pool.QueryRow(t.Context(),
		`SELECT chave || ' ' || banco_id || ' ' || resposta::text || ' ' ||
		        capturado_em::text || ' ' || expira_em::text
		 FROM respostas_em_cache`).Scan(&linha); err != nil {
		t.Fatalf("ler a linha: %v", err)
	}

	for _, pessoal := range []string{"1987", "03-14", "4321.50", "4321"} {
		if strings.Contains(linha, pessoal) {
			t.Errorf("a linha gravada leva %q em claro:\n%s", pessoal, linha)
		}
	}
}

// TestUmaRespostaGuardadaVoltaComOCapturadoEmDoBancoEEmCache.
//
// ⚠️ O `capturado_em` de um acerto é o instante em que se FALOU com o banco, e
// não o de agora — reverter para carimbar a leitura apresenta um preço de há
// minutos como acabado de cotar, que é o que o campo existe para impedir.
func TestUmaRespostaGuardadaVoltaComOCapturadoEmDoBancoEEmCache(t *testing.T) {
	pool := subirBase(t)
	c := cache.NovoPostgres(pool, time.Hour)

	falouSeComOBanco := time.Date(2026, 8, 7, 10, 30, 0, 0, time.UTC)
	guardouSe := time.Date(2026, 8, 7, 10, 30, 1, 0, time.UTC)
	leuSe := time.Date(2026, 8, 7, 10, 45, 0, 0, time.UTC)

	chave := chaveDe(t, "cgd", pedidoDe(t, "1987-03-14", "4321.50", "250000"))
	if err := c.Gravar(t.Context(), chave, "cgd", ofertaServida(falouSeComOBanco), guardouSe); err != nil {
		t.Fatalf("Gravar: %v", err)
	}

	oferta, houve, err := c.Ler(t.Context(), chave, leuSe)
	if err != nil {
		t.Fatalf("Ler: %v", err)
	}
	if !houve {
		t.Fatal("gravou-se e não se leu: a cache não acerta e não corta pedido nenhum")
	}
	if oferta.EmCache == nil || !*oferta.EmCache {
		t.Error("o acerto não se distingue de uma resposta fresca")
	}
	if oferta.CapturadoEm == nil || !oferta.CapturadoEm.Equal(falouSeComOBanco) {
		t.Errorf("capturado_em %v, esperava o instante em que se falou com o banco (%v)",
			oferta.CapturadoEm, falouSeComOBanco)
	}
	if oferta.Tan == nil || *oferta.Tan != 3.25 {
		t.Errorf("a resposta voltou diferente do que se guardou: %+v", oferta)
	}
}

// TestUmaEntradaExpiradaNaoSeServe é o terceiro critério de pronto da KAN-58.
//
// ⚠️ E mede-se **sem** correr a limpeza de propósito: se a validade só vivesse
// no `LimparExpiradas`, uma entrada expirada era servida durante toda a janela
// entre expirar e ser apagada. Reverter — tirar o `expira_em > @agora` da
// consulta — faz este teste servir um preço fora de prazo.
func TestUmaEntradaExpiradaNaoSeServe(t *testing.T) {
	pool := subirBase(t)
	c := cache.NovoPostgres(pool, 5*time.Minute)

	gravouSe := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	chave := chaveDe(t, "cgd", pedidoDe(t, "1987-03-14", "4321.50", "250000"))
	if err := c.Gravar(t.Context(), chave, "cgd", ofertaServida(gravouSe), gravouSe); err != nil {
		t.Fatalf("Gravar: %v", err)
	}

	// Um segundo antes de expirar ainda serve.
	if _, houve, err := c.Ler(t.Context(), chave, gravouSe.Add(5*time.Minute-time.Second)); err != nil || !houve {
		t.Fatalf("dentro da validade devia servir (houve=%v, err=%v)", houve, err)
	}

	// Um segundo depois, não.
	_, houve, err := c.Ler(t.Context(), chave, gravouSe.Add(5*time.Minute+time.Second))
	if err != nil {
		t.Fatalf("Ler: %v", err)
	}
	if houve {
		t.Error("serviu-se uma entrada expirada — é o preço que já mudou a ser dado como bom")
	}
}

// TestUmaOfertaEmFalhaNaoSeGuarda.
//
// ⚠️ Guardar um `banco_indisponivel` transformava um soluço de segundos numa
// indisponibilidade de toda a validade, e multiplicava-a por todos os clientes
// com o mesmo pedido. Reverter — guardar tudo — faz este teste servir a falha em
// vez de voltar ao banco.
func TestUmaOfertaEmFalhaNaoSeGuarda(t *testing.T) {
	pool := subirBase(t)
	c := cache.NovoPostgres(pool, time.Hour)
	agora := time.Date(2026, 8, 7, 11, 0, 0, 0, time.UTC)

	chave := chaveDe(t, "cgd", pedidoDe(t, "1987-03-14", "4321.50", "250000"))
	falha := api.Oferta{
		BancoId: "cgd", BancoNome: "CGD", Sucesso: false,
		Erro: &api.OfertaErro{Codigo: "banco_indisponivel", Mensagem: "O CGD não respondeu."},
	}
	if err := c.Gravar(t.Context(), chave, "cgd", falha, agora); err != nil {
		t.Fatalf("Gravar de uma falha devia ser silencioso, veio: %v", err)
	}

	if _, houve, err := c.Ler(t.Context(), chave, agora); err != nil || houve {
		t.Errorf("a falha ficou guardada (houve=%v, err=%v): um soluço vira indisponibilidade", houve, err)
	}
}

// TestGravarDuasVezesRenovaAEntrada: `ON CONFLICT DO UPDATE` e não `DO NOTHING`.
// Com `DO NOTHING` uma entrada a expirar nunca se renovava, e a cache deixava de
// acertar sem nada a dizer porquê.
func TestGravarDuasVezesRenovaAEntrada(t *testing.T) {
	pool := subirBase(t)
	c := cache.NovoPostgres(pool, 5*time.Minute)

	primeiro := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	chave := chaveDe(t, "cgd", pedidoDe(t, "1987-03-14", "4321.50", "250000"))
	if err := c.Gravar(t.Context(), chave, "cgd", ofertaServida(primeiro), primeiro); err != nil {
		t.Fatalf("primeiro Gravar: %v", err)
	}

	// Quatro minutos depois volta-se ao banco (outro cliente, outra vaga) e
	// grava-se de novo: a validade conta a partir de agora.
	segundo := primeiro.Add(4 * time.Minute)
	if err := c.Gravar(t.Context(), chave, "cgd", ofertaServida(segundo), segundo); err != nil {
		t.Fatalf("segundo Gravar: %v", err)
	}

	// Aos 6 minutos do primeiro, a entrada renovada ainda serve.
	oferta, houve, err := c.Ler(t.Context(), chave, primeiro.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("Ler: %v", err)
	}
	if !houve {
		t.Fatal("a segunda gravação não renovou a validade")
	}
	if oferta.CapturadoEm == nil || !oferta.CapturadoEm.Equal(segundo) {
		t.Errorf("capturado_em %v, esperava o da resposta mais fresca (%v)", oferta.CapturadoEm, segundo)
	}
}

// TestLimparExpiradasSoLevaOQueJaNaoServe.
func TestLimparExpiradasSoLevaOQueJaNaoServe(t *testing.T) {
	pool := subirBase(t)
	c := cache.NovoPostgres(pool, 5*time.Minute)
	agora := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)

	velha := chaveDe(t, "cgd", pedidoDe(t, "1987-03-14", "4321.50", "250000"))
	nova := chaveDe(t, "montepio", pedidoDe(t, "1987-03-14", "4321.50", "250000"))
	if err := c.Gravar(t.Context(), velha, "cgd", ofertaServida(agora), agora); err != nil {
		t.Fatalf("Gravar a velha: %v", err)
	}
	if err := c.Gravar(t.Context(), nova, "montepio", ofertaServida(agora), agora.Add(4*time.Minute)); err != nil {
		t.Fatalf("Gravar a nova: %v", err)
	}

	caidas, err := c.LimparExpiradas(t.Context(), agora.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("LimparExpiradas: %v", err)
	}
	if caidas != 1 {
		t.Errorf("caíram %d linhas, esperava 1 — a que ainda serve tem de ficar", caidas)
	}
	if _, houve, _ := c.Ler(t.Context(), nova, agora.Add(6*time.Minute)); !houve {
		t.Error("a limpeza levou uma entrada que ainda estava válida")
	}
}

// TestUmaValidadeNulaValeADeOmissao: uma validade a zero seria não ter cache, e
// quem constrói com zero por engano não quer dizer isso. Quem quer desligá-la
// di-lo no arranque, não aqui (ver o `web.ValidadeDeCacheDe`).
func TestUmaValidadeNulaValeADeOmissao(t *testing.T) {
	pool := subirBase(t)
	if v := cache.NovoPostgres(pool, 0).Validade(); v != cache.ValidadeOmissao {
		t.Errorf("validade %s, esperava a de omissão (%s)", v, cache.ValidadeOmissao)
	}
	if v := cache.NovoPostgres(pool, -time.Hour).Validade(); v != cache.ValidadeOmissao {
		t.Errorf("validade negativa deu %s, esperava a de omissão", v)
	}
}

// ofertaServida é uma resposta de sucesso como o `ofertaDe` a produz.
func ofertaServida(capturadoEm time.Time) api.Oferta {
	tan := 3.25
	return api.Oferta{
		BancoId: "cgd", BancoNome: "CGD", Sucesso: true,
		Tan: &tan, CapturadoEm: &capturadoEm,
	}
}

// subirBase sobe um Postgres efémero, migra-o e devolve o pool. Migra porque
// esta cache é uma TABELA — ao contrário da lotação, que são advisory locks e
// não precisa de esquema nenhum. Precisa de Docker a correr; sem ele, o teste
// falha em vez de fingir que passou.
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
