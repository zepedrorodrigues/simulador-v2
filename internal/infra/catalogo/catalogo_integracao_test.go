package catalogo_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/catalogo"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
)

// O critério de pronto da KAN-16, palavra por palavra: «dois varrimentos
// seguidos com a guarda ligada produzem UM lote».
//
// ⚠️ Mede-se aqui e não com um catálogo falso porque o que se conta são
// varrimento_id distintos numa base a sério — e porque foi assim que o defeito
// apareceu no v1: cinco corridas em 14 minutos, cinco lotes, e a série a
// mostrar movimento onde não houve nenhum.
func TestDoisVarrimentosSeguidosComAGuardaDaoUmLote(t *testing.T) {
	pool := subirBase(t)
	cat := catalogo.NovoPostgres(pool)
	v := varredorDeProva(t)

	const guarda = 6 * time.Hour
	pontos := varrimento.MesmosPontos([]varrimento.Ponto{pontoDeProva("variavel/0/propria")})

	primeiro, err := v.VarrerEGravar(t.Context(), cat, pontos, guarda)
	if err != nil {
		t.Fatalf("primeiro varrimento: %v", err)
	}
	if primeiro.ID == "" {
		t.Fatal("o primeiro varrimento não abriu lote")
	}

	// O segundo, logo a seguir, tem de ser travado.
	//
	// ⚠️ Aqui é t.Error e não t.Fatal de propósito: o que o critério de pronto
	// manda ver a falhar é a CONTAGEM DOS LOTES, e um Fatal aqui parava o teste
	// antes de lá chegar. Revertida a guarda, a frase que aparece tem de ser «2
	// lotes» e não «passou a guarda» — a segunda descreve o mecanismo, a
	// primeira descreve o estrago.
	segundo, err := v.VarrerEGravar(t.Context(), cat, pontos, guarda)
	if err == nil {
		t.Error("o segundo varrimento passou a guarda")
	}
	if !segundo.Saltado {
		t.Error("o segundo não ficou marcado como saltado")
	}

	if lotes := contarLotes(t, pool); lotes != 1 {
		t.Errorf(
			"saíram %d lotes de dois varrimentos seguidos, e a guarda existe para sair 1 — "+
				"cinco corridas em 14 minutos não são cinco dias de dados",
			lotes)
	}
}

func TestOLoteGravaOsProdutosQueAssumiu(t *testing.T) {
	pool := subirBase(t)
	cat := catalogo.NovoPostgres(pool)

	// ⚠️ Sem isto a série é incomparável entre bancos e não se nota: cada banco
	// tem defaults diferentes, e a CGD e o BPI apareciam caros por lhes faltar o
	// desconto, não por cobrarem mais. O v1 demorou a descobri-lo.
	obs := observacaoDeProva()
	obs.Oferta.ProdutosAplicados = []string{"cgd:packs"}

	if _, err := cat.GravarLote(t.Context(), []varrimento.Observacao{obs}); err != nil {
		t.Fatalf("GravarLote: %v", err)
	}

	var produtos string
	linha := pool.QueryRow(t.Context(), `SELECT produtos::text FROM catalogo_taxas LIMIT 1`)
	if err := linha.Scan(&produtos); err != nil {
		t.Fatalf("ler produtos: %v", err)
	}
	if produtos != `["cgd:packs"]` {
		t.Errorf("produtos = %s, esperava [\"cgd:packs\"]", produtos)
	}
}

func TestUmaObservacaoSemProdutosGravaListaVaziaENaoNulo(t *testing.T) {
	pool := subirBase(t)
	cat := catalogo.NovoPostgres(pool)

	// Vazio é informação — «nada aplicado» — e nulo seria «não sei». O
	// /api/rate-catalog exige `products` sempre lista (API.md §2).
	if _, err := cat.GravarLote(t.Context(), []varrimento.Observacao{observacaoDeProva()}); err != nil {
		t.Fatalf("GravarLote: %v", err)
	}

	var produtos string
	if err := pool.QueryRow(t.Context(), `SELECT produtos::text FROM catalogo_taxas LIMIT 1`).Scan(&produtos); err != nil {
		t.Fatalf("ler produtos: %v", err)
	}
	if produtos != "[]" {
		t.Errorf("produtos = %s, esperava []", produtos)
	}
}

func TestUmaRespostaIncompletaDesceAFalhaENomeiaOCampo(t *testing.T) {
	pool := subirBase(t)
	cat := catalogo.NovoPostgres(pool)

	// A §4 é explícita: «um nulo aqui não é meio-dado, é resposta mal lida, e
	// isso é uma falha (sucesso = false)». O que este teste afirma a mais é que
	// o erro NOMEIA o campo — sem isso, a linha ficava a dizer que o banco
	// esteve em baixo quando o que houve foi uma resposta incompleta, e isso é
	// a KAN-30 outra vez.
	obs := observacaoDeProva()
	obs.Oferta.TAEG = nil

	if _, err := cat.GravarLote(t.Context(), []varrimento.Observacao{obs}); err != nil {
		t.Fatalf("GravarLote: %v", err)
	}

	var sucesso bool
	var erro string
	linha := pool.QueryRow(t.Context(), `SELECT sucesso, coalesce(erro, '') FROM catalogo_taxas LIMIT 1`)
	if err := linha.Scan(&sucesso, &erro); err != nil {
		t.Fatalf("ler a linha: %v", err)
	}
	if sucesso {
		t.Error("uma resposta sem TAEG ficou gravada como sucesso")
	}
	if !strings.Contains(erro, "TAEG") {
		t.Errorf("o erro não nomeia o campo em falta: %q", erro)
	}
}

func TestUmLoteOuEntraTodoOuNaoEntraNenhum(t *testing.T) {
	pool := subirBase(t)
	cat := catalogo.NovoPostgres(pool)

	// ⚠️ Um lote meio gravado deixaria na série uma corrida com bancos a faltar
	// e nada a dizer que faltavam — e quem a lesse concluía que esses bancos não
	// tinham respondido.
	boa := observacaoDeProva()
	impossivel := observacaoDeProva()
	impossivel.Oferta.CapturadoEm = time.Time{} // sem instante de captura: não se grava

	if _, err := cat.GravarLote(t.Context(), []varrimento.Observacao{boa, impossivel}); err == nil {
		t.Fatal("o lote passou com uma observação impossível lá dentro")
	}

	var linhas int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM catalogo_taxas`).Scan(&linhas); err != nil {
		t.Fatalf("contar: %v", err)
	}
	if linhas != 0 {
		t.Errorf("ficaram %d linhas de um lote que falhou a meio", linhas)
	}
}

func TestNuncaTerVarridoNaoEUmVarrimentoAntigo(t *testing.T) {
	pool := subirBase(t)
	cat := catalogo.NovoPostgres(pool)

	quando, houve, err := cat.UltimoVarrimentoEm(t.Context())
	if err != nil {
		t.Fatalf("UltimoVarrimentoEm: %v", err)
	}
	if houve {
		t.Errorf("uma base vazia disse que já se varreu, em %s", quando)
	}
}

// ------------------------------------------------------------------ ajudantes

func varredorDeProva(t *testing.T) *varrimento.Varredor {
	t.Helper()
	v, err := varrimento.Novo(varrimento.Config{
		Bancos: []bancos.Banco{bancoQueResponde{}},
		Travao: travaoLivre{},
	})
	if err != nil {
		t.Fatalf("varrimento.Novo: %v", err)
	}
	return v
}

func pontoDeProva(cenario string) varrimento.Ponto {
	return varrimento.Ponto{
		Cenario: cenario,
		Pedido: dominio.Pedido{
			ValorImovel: dominio.DinheiroDeInteiro(400_000),
			Montante:    dominio.DinheiroDeInteiro(320_000),
			PrazoAnos:   30,
			TipoTaxa:    dominio.TaxaVariavel,
			Finalidade:  dominio.FinalidadePropria,
			Localizacao: dominio.LocalizacaoContinente,
		},
	}
}

func observacaoDeProva() varrimento.Observacao {
	return varrimento.Observacao{
		Ponto:  pontoDeProva("variavel/0/propria"),
		Oferta: ofertaCompleta(),
	}
}

// ofertaCompleta é uma resposta com tudo o que os CHECK da tabela exigem numa
// linha de sucesso. Os testes tiram-lhe campos para afirmar o que acontece.
func ofertaCompleta() dominio.Oferta {
	tan, taeg := taxaDe("4.500"), taxaDe("4.700")
	spread, euribor := taxaDe("1.350"), taxaDe("2.450")
	prestacao, mtic := dinheiroDe(1_600), dinheiroDe(600_000)
	return dominio.Oferta{
		BancoID:      "cgd",
		BancoNome:    "Caixa Geral de Depósitos",
		TAN:          &tan,
		TAEG:         &taeg,
		Spread:       &spread,
		Indexante:    dominio.Euribor6M,
		EuriborValor: &euribor,
		Prestacao:    &prestacao,
		MTIC:         &mtic,
		CapturadoEm:  time.Now(),
	}
}

// bancoQueResponde devolve sempre a oferta completa. O varrimento carimba-lhe a
// hora e o banco.
type bancoQueResponde struct{}

func (bancoQueResponde) ID() string   { return "cgd" }
func (bancoQueResponde) Nome() string { return "Caixa Geral de Depósitos" }

func (bancoQueResponde) Requisitos() dominio.Requisitos {
	return dominio.Requisitos{BancoID: "cgd", BancoNome: "Caixa Geral de Depósitos", Custo: dominio.CustoBarato}
}

func (bancoQueResponde) Simular(context.Context, dominio.Pedido) (dominio.Oferta, error) {
	return ofertaCompleta(), nil
}

// travaoLivre deixa passar. O travão a sério tem os seus testes em
// internal/infra/travao; aqui o que está a ser medido é a guarda de tempo.
type travaoLivre struct{}

func (travaoLivre) Tomar(context.Context, string) (varrimento.Largar, error) {
	return func() {}, nil
}

func contarLotes(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(DISTINCT varrimento_id) FROM catalogo_taxas`).Scan(&n); err != nil {
		t.Fatalf("contar lotes: %v", err)
	}
	return n
}

func taxaDe(s string) dominio.Taxa {
	t, err := dominio.TaxaDeTexto(s)
	if err != nil {
		panic(err)
	}
	return t
}

func dinheiroDe(n int64) dominio.Dinheiro { return dominio.DinheiroDeInteiro(n) }

// subirBase sobe um Postgres efémero já migrado e devolve um pool. Um por teste
// — isolamento total, sem estado partilhado a limpar.
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
