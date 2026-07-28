package web_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// TestTodaARotaDoSpecTemHandler é o travão que faltava (KAN-44).
//
// ⚠️ **O `/api/rate-catalog/snapshots` esteve no contrato sem handler nenhum
// desde que o contrato existe, e nada o apanhou.** A razão é estrutural: o
// portão verifica que o código **gerado** está em dia com o spec (`make
// gerado`), não que o **servido** está. Uma rota declarada e não servida
// atravessa o `make verificar` inteiro sem uma palavra — e o teste de contrato
// do `/api/rate-catalog` não a cobria, porque só afirma o formato daquele
// endpoint.
//
// Este teste fecha a classe toda, e não só o caso: qualquer caminho novo que
// alguém escreva no `openapi.yaml` e se esqueça de montar em `Rotas()` falha
// aqui, **a nomear o caminho**.
func TestTodaARotaDoSpecTemHandler(t *testing.T) {
	rotas := rotasDoSpec(t)
	if len(rotas) == 0 {
		t.Fatal("não se leu caminho nenhum do openapi.yaml — o teste estaria a afirmar o vazio")
	}
	t.Logf("%d rotas declaradas no contrato", len(rotas))

	// ⚠️ Com chave configurada: sem ela, um 401 do `exigirChave` seria
	// indistinguível de uma rota em falta, e o teste passava a medir outra coisa.
	servidor := servidorComCatalogo(t, nil, "chave-de-teste-com-32-caracteres!")

	for _, r := range rotas {
		t.Run(r.metodo+" "+r.caminho, func(t *testing.T) {
			req := httptest.NewRequest(r.metodo, r.caminho, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", "chave-de-teste-com-32-caracteres!")
			resp := httptest.NewRecorder()
			servidor.Rotas().ServeHTTP(resp, req)

			// O que se afirma é estreito de propósito: **existe handler**. Não se
			// afirma o corpo, nem o estatuto de sucesso — um pedido vazio pode e
			// deve dar 400, e isso prova que alguém o leu.
			switch resp.Code {
			case http.StatusNotFound:
				t.Errorf("o contrato declara %s %s e ninguém o serve: 404. "+
					"Falta registá-lo em Rotas().", r.metodo, r.caminho)
			case http.StatusMethodNotAllowed:
				t.Errorf("o contrato declara %s %s e a rota existe noutro método: 405. "+
					"Falta registar ESTE método em Rotas().", r.metodo, r.caminho)
			}
		})
	}
}

type rotaDoSpec struct{ metodo, caminho string }

// rotasDoSpec lê os caminhos e métodos do openapi.yaml.
//
// ⚠️ Lê o **spec**, e não o `api.gen.go`. O gerado sai do spec, portanto
// compará-lo com o spec provaria que o gerador funciona — que já é o que o
// `make gerado` afirma. O que aqui interessa é o outro lado: que o que está
// declarado tem alguém a servi-lo.
func rotasDoSpec(t *testing.T) []rotaDoSpec {
	t.Helper()

	bruto, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("ler o openapi.yaml: %v", err)
	}

	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(bruto, &spec); err != nil {
		t.Fatalf("o openapi.yaml não é YAML válido: %v", err)
	}

	metodos := map[string]string{
		"get": http.MethodGet, "post": http.MethodPost, "put": http.MethodPut,
		"patch": http.MethodPatch, "delete": http.MethodDelete,
	}

	var rotas []rotaDoSpec
	for caminho, operacoes := range spec.Paths {
		for verbo := range operacoes {
			if metodo, ehMetodo := metodos[strings.ToLower(verbo)]; ehMetodo {
				rotas = append(rotas, rotaDoSpec{metodo: metodo, caminho: caminho})
			}
		}
	}

	// Ordem estável: um teste cuja lista muda de ordem entre corridas é um teste
	// cuja falha é difícil de comparar com a anterior.
	sort.Slice(rotas, func(i, j int) bool {
		if rotas[i].caminho != rotas[j].caminho {
			return rotas[i].caminho < rotas[j].caminho
		}
		return rotas[i].metodo < rotas[j].metodo
	})
	return rotas
}

// --- o handler que faltava (KAN-44) --------------------------------------------------

func TestOsSnapshotsSaemComAContagemEOFormatoDoV1(t *testing.T) {
	quando := time.Date(2026, 7, 28, 5, 0, 11, 0, time.UTC)
	servidor := servidorComSnapshots(t, []dominio.Snapshot{
		{VarrimentoID: "3f2a1b4c-0000-0000-0000-000000000001", CapturadoEm: quando, Linhas: 96},
	}, aChave)

	corpo := lerSnapshots(t, servidor, aChave, http.StatusOK)
	lista, _ := corpo["snapshots"].([]any)
	if len(lista) != 1 {
		t.Fatalf("esperava 1 snapshot, vieram %d", len(lista))
	}
	s, _ := lista[0].(map[string]any)

	if s["snapshot_id"] != "3f2a1b4c-0000-0000-0000-000000000001" {
		t.Errorf("snapshot_id = %v", s["snapshot_id"])
	}
	// ⚠️ O mesmo formato sem fuso do resto deste endpoint. Escrever `Z` aqui e
	// não nos pontos era servir dois formatos na mesma API.
	if s["captured_at"] != "2026-07-28T05:00:11" {
		t.Errorf("captured_at = %v, e este endpoint serializa sem fuso", s["captured_at"])
	}
	if s["rows"] != float64(96) {
		t.Errorf("rows = %v", s["rows"])
	}
}

func TestSemVarrimentosOsSnapshotsSaoListaVaziaENaoNulo(t *testing.T) {
	// ⚠️ A terceira armadilha de compatibilidade do `API.md` §2, aplicada aqui:
	// quem lê tem de poder distinguir «não há varrimentos» de «não sei».
	servidor := servidorComSnapshots(t, nil, aChave)

	corpo := lerSnapshots(t, servidor, aChave, http.StatusOK)
	if _, ok := corpo["snapshots"].([]any); !ok {
		t.Errorf("snapshots veio como %T e tem de ser lista", corpo["snapshots"])
	}
}

func TestOsSnapshotsExigemChave(t *testing.T) {
	// ⚠️ Sem a guarda, publicava-se a cadência de varrimento — a que horas
	// corremos, quantas vezes, quando falhámos — a quem não tem chave. É
	// informação sobre nós, não sobre o mercado.
	servidor := servidorComSnapshots(t, nil, aChave)

	req := httptest.NewRequest(http.MethodGet, "/api/rate-catalog/snapshots", nil)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Errorf("sem chave devolveu %d — a cadência de varrimento ficaria pública", resp.Code)
	}
}

func TestUmaFalhaALerOsVarrimentosNaoSaiComoListaVazia(t *testing.T) {
	// Uma base em baixo a responder `{"snapshots":[]}` diria ao consumidor que
	// não há varrimentos nenhuns — que é uma afirmação, e falsa.
	servidor := servidorComCatalogoAvariado(t)

	req := httptest.NewRequest(http.MethodGet, "/api/rate-catalog/snapshots", nil)
	req.Header.Set("X-API-Key", aChave)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Errorf("com a leitura avariada devolveu %d, e não 500", resp.Code)
	}
}

func servidorComSnapshots(t *testing.T, snaps []dominio.Snapshot, chaves ...string) *web.Servidor {
	t.Helper()
	s, err := web.Novo(
		fonteEmMemoria{}, catalogoEmMemoria{snapshots: snaps}, bancos.Predefinido(), chaves, relogio)
	if err != nil {
		t.Fatalf("Novo: %v", err)
	}
	return s
}

func servidorComCatalogoAvariado(t *testing.T) *web.Servidor {
	t.Helper()
	s, err := web.Novo(
		fonteEmMemoria{},
		catalogoEmMemoria{erroDeLer: errors.New("a base não responde")},
		bancos.Predefinido(), []string{aChave}, relogio)
	if err != nil {
		t.Fatalf("Novo: %v", err)
	}
	return s
}

func lerSnapshots(t *testing.T, s *web.Servidor, chave string, esperado int) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/rate-catalog/snapshots", nil)
	req.Header.Set("X-API-Key", chave)
	resp := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resp, req)

	if resp.Code != esperado {
		t.Fatalf("estado %d (esperava %d): %s", resp.Code, esperado, resp.Body.String())
	}
	var corpo map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &corpo); err != nil {
		t.Fatalf("a resposta não é JSON: %v", err)
	}
	return corpo
}
