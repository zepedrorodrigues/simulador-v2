package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// O teste de contrato do `/api/rate-catalog` — o portão 2 dos cinco da §8, que o
// `ARQUITETURA.md` e o `CLAUDE.md` diziam existir e **não existia** (KAN-42).
//
// ⚠️ A amostra em `api/amostras/rate-catalog-v1.json` **saiu do v1 a correr**, a
// 2026-07-28: subiu-se um Postgres, gravaram-se duas entradas pelo
// `RateCatalogRepository`, e chamou-se o endpoint pelo `TestClient` do FastAPI.
// Não foi transcrita do spec — uma amostra escrita à mão afirmaria que o gerado
// bate com o spec, que é o que o `make gerado` já faz, e não que bate com o que
// o `viabilidade-imobiliaria` recebe.
//
// ⚠️ E o que ela apanhou **antes de este teste existir**: o v1 envia
// `"fixed_period_years": null` no cenário variável, e o contrato do v2
// declarava-o `integer` não-nulo — em Go isso serializa `0`, e o consumidor
// receberia zero anos de período fixo onde espera «não se aplica».

func TestOContratoDoRateCatalogBateComOV1(t *testing.T) {
	amostra := amostraDoV1(t)

	// Os pontos da amostra, traduzidos para o que a nossa leitura devolveria.
	s := servidorComCatalogo(t, pontosDaAmostra(t, amostra))

	pedido := httptest.NewRequest(http.MethodGet, "/api/rate-catalog", nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d: %s", resposta.Code, resposta.Body.String())
	}

	var nosso map[string]any
	if err := json.Unmarshal(resposta.Body.Bytes(), &nosso); err != nil {
		t.Fatalf("a nossa resposta não é JSON: %v", err)
	}

	// ⚠️ Compara-se o objecto INTEIRO, e não campo a campo escolhido: um campo a
	// mais é tão incompatível como um a menos, e é o que o consumidor recebe.
	if d := cmp.Diff(amostra, nosso); d != "" {
		t.Errorf("a nossa resposta não é a do v1 (-v1 +nós):\n%s", d)
	}
}

// TestOsTiposDoRateCatalogSaoOsDoV1 afirma as três armadilhas da §4, uma a uma e
// com nome — porque um `cmp.Diff` verde não diz qual delas está a ser respeitada.
func TestOsTiposDoRateCatalogSaoOsDoV1(t *testing.T) {
	amostra := amostraDoV1(t)
	s := servidorComCatalogo(t, pontosDaAmostra(t, amostra))

	pedido := httptest.NewRequest(http.MethodGet, "/api/rate-catalog", nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	var nosso map[string]any
	if err := json.Unmarshal(resposta.Body.Bytes(), &nosso); err != nil {
		t.Fatalf("a nossa resposta não é JSON: %v", err)
	}
	pontos, _ := nosso["points"].([]any)
	if len(pontos) == 0 {
		t.Fatal("a resposta não trouxe pontos")
	}
	primeiro, _ := pontos[0].(map[string]any)

	t.Run("números e não strings", func(t *testing.T) {
		// ⚠️ O shopspring/decimal serializa com aspas por omissão, e o v1 envia
		// números. Uma string aqui parte o consumidor em silêncio.
		for _, campo := range []string{"tan", "taeg", "spread", "prestacao_mensal", "mtic", "euribor_valor"} {
			if _, ok := primeiro[campo].(float64); !ok {
				t.Errorf("%s veio como %T e o v1 envia número", campo, primeiro[campo])
			}
		}
	})

	t.Run("captured_at sem fuso", func(t *testing.T) {
		// O v1 guardava instantes ingénuos e serializava sem `Z`. Escrever o `Z`
		// parece mais correcto e parte o outro repositório.
		capturado, ok := primeiro["captured_at"].(string)
		if !ok {
			t.Fatalf("captured_at veio como %T", primeiro["captured_at"])
		}
		if len(capturado) != len("2026-07-22T05:00:11") {
			t.Errorf("captured_at = %q, e o v1 envia 2026-07-22T05:00:11", capturado)
		}
		if capturado[len(capturado)-1] == 'Z' {
			t.Errorf("captured_at = %q e leva Z — o v1 serializa sem fuso", capturado)
		}
	})

	t.Run("products é sempre lista, nunca null", func(t *testing.T) {
		// Vazio quer dizer «correu sem bonificações», que é informação. Um null
		// não se distingue de um erro.
		for i, bruto := range pontos {
			p, _ := bruto.(map[string]any)
			if _, ok := p["products"].([]any); !ok {
				t.Errorf("ponto %d: products veio como %T", i+1, p["products"])
			}
		}
	})

	t.Run("fixed_period_years é nulo na variável", func(t *testing.T) {
		// É o que o v1 envia, e o que o contrato do v2 declarava mal.
		cenarios, _ := nosso["scenarios"].([]any)
		for _, bruto := range cenarios {
			c, _ := bruto.(map[string]any)
			if c["rate_type"] != "variavel" {
				continue
			}
			if c["fixed_period_years"] != nil {
				t.Errorf("o cenário variável traz fixed_period_years = %v, e o v1 envia null",
					c["fixed_period_years"])
			}
		}
	})
}

// --- ajudantes ---------------------------------------------------------------------

type catalogoEmMemoria struct {
	pontos    []dominio.PontoDeMercado
	snapshots []dominio.Snapshot
	erroDeLer error
}

func (c catalogoEmMemoria) PontosDoCatalogo(
	context.Context, dominio.FiltroDoCatalogo,
) ([]dominio.PontoDeMercado, error) {
	return c.pontos, nil
}

func (c catalogoEmMemoria) SnapshotsDoCatalogo(context.Context) ([]dominio.Snapshot, error) {
	return c.snapshots, c.erroDeLer
}

func servidorComCatalogo(t *testing.T, pontos []dominio.PontoDeMercado, chaves ...string) *web.Servidor {
	t.Helper()
	s, err := web.Novo(fonteEmMemoria{}, catalogoEmMemoria{pontos: pontos}, bancos.Predefinido(), chaves, relogio)
	if err != nil {
		t.Fatalf("Novo: %v", err)
	}
	return s
}

func amostraDoV1(t *testing.T) map[string]any {
	t.Helper()
	bruto, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "amostras", "rate-catalog-v1.json"))
	if err != nil {
		t.Fatalf("ler a amostra do v1: %v", err)
	}
	var amostra map[string]any
	if err := json.Unmarshal(bruto, &amostra); err != nil {
		t.Fatalf("a amostra do v1 não é JSON: %v", err)
	}
	return amostra
}

// pontosDaAmostra converte os pontos da amostra em pontos do domínio — que é o
// que a nossa leitura devolveria da base.
//
// ⚠️ A volta é deliberada: o teste não compara a amostra consigo própria, compara
// o que o NOSSO serializador produz a partir dos MESMOS dados. Uma diferença de
// tipo, de nome ou de formato aparece; uma diferença de valores não, e não é
// isso que este portão existe para medir.
func pontosDaAmostra(t *testing.T, amostra map[string]any) []dominio.PontoDeMercado {
	t.Helper()

	brutos, _ := amostra["points"].([]any)
	pontos := make([]dominio.PontoDeMercado, 0, len(brutos))
	for _, b := range brutos {
		p, _ := b.(map[string]any)

		capturado, err := time.Parse("2006-01-02T15:04:05", texto(p["captured_at"]))
		if err != nil {
			t.Fatalf("captured_at da amostra: %v", err)
		}

		ponto := dominio.PontoDeMercado{
			VarrimentoID: texto(p["snapshot_id"]),
			CapturadoEm:  capturado,
			Cenario:      texto(p["scenario_key"]),
			BancoID:      texto(p["bank_id"]),
			BancoNome:    texto(p["bank_name"]),
			TipoTaxa:     texto(p["rate_type"]),
			ValorImovel:  dinheiroDaAmostra(t, p["valor_imovel"]),
			Montante:     dinheiroDaAmostra(t, p["montante"]),
			PrazoAnos:    int(numero(p["prazo_anos"])),
			Indexante:    texto(p["euribor_indexante"]),
			TAN:          taxaDaAmostra(t, p["tan"]),
			TAEG:         taxaDaAmostra(t, p["taeg"]),
			Spread:       taxaDaAmostra(t, p["spread"]),
			EuriborValor: taxaDaAmostra(t, p["euribor_valor"]),
			Prestacao:    dinheiroOuNil(t, p["prestacao_mensal"]),
			MTIC:         dinheiroOuNil(t, p["mtic"]),
			Produtos:     listaDaAmostra(p["products"]),
		}
		if p["fixed_period_years"] != nil {
			anos := int(numero(p["fixed_period_years"]))
			ponto.PeriodoFixoAnos = &anos
		}
		pontos = append(pontos, ponto)
	}
	return pontos
}

func texto(v any) string {
	s, _ := v.(string)
	return s
}

func numero(v any) float64 {
	n, _ := v.(float64)
	return n
}

// listaDaAmostra devolve NIL quando a lista está vazia.
//
// ⚠️ E é assim de propósito: é o que a leitura real faz — o `listaDeJSON` do
// `infra/catalogo` devolve nil para uma lista vazia. A primeira versão deste
// helper devolvia `[]string{}`, e com ela a reversão «tirar a guarda do
// products» PASSAVA: o nil nunca chegava ao serializador, e o teste afirmava
// sobre um caso que não existe. Um ajudante de teste que não reproduz a
// fronteira mede outra coisa.
func listaDaAmostra(v any) []string {
	brutos, _ := v.([]any)
	if len(brutos) == 0 {
		return nil
	}
	saida := make([]string, 0, len(brutos))
	for _, b := range brutos {
		saida = append(saida, texto(b))
	}
	return saida
}

func taxaDaAmostra(t *testing.T, v any) *dominio.Taxa {
	t.Helper()
	if v == nil {
		return nil
	}
	x := dominio.TaxaDeDecimal(decimalDaAmostra(t, v))
	return &x
}

func dinheiroOuNil(t *testing.T, v any) *dominio.Dinheiro {
	t.Helper()
	if v == nil {
		return nil
	}
	d := dominio.DinheiroDeDecimal(decimalDaAmostra(t, v))
	return &d
}

func dinheiroDaAmostra(t *testing.T, v any) dominio.Dinheiro {
	t.Helper()
	return dominio.DinheiroDeDecimal(decimalDaAmostra(t, v))
}

// decimalDaAmostra lê um número da amostra PELOS DÍGITOS e não pelo float.
//
// ⚠️ O JSON traz `4.5` como float64, e `decimal.NewFromFloat` desse valor daria
// os dígitos que o float tem. Passar pelo texto mantém o que o v1 escreveu — que
// é o que este teste compara.
func decimalDaAmostra(t *testing.T, v any) decimal.Decimal {
	t.Helper()
	bruto, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("número da amostra: %v", err)
	}
	d, err := decimal.NewFromString(string(bruto))
	if err != nil {
		t.Fatalf("número da amostra %q: %v", bruto, err)
	}
	return d
}
