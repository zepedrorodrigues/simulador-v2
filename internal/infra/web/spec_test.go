package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestTodaARotaDoSpecTemHandler é o travão que faltava (KAN-44).
//
// ⚠️ **O `/api/rate-catalog/snapshots` esteve no contrato sem handler nenhum
// desde que o contrato existe, e nada o apanhou.** A razão é estrutural: o
// portão verifica que o código **gerado** está em dia com o spec (`make
// gerado`), não que o **servido** está. Uma rota declarada e não servida
// atravessa o `make verificar` inteiro sem uma palavra.
//
// ⚠️ Essa rota já não existe — saiu com o varrimento na Fase 6 —, e o teste
// **fica**: o que ele trava não era aquela rota, era a classe. E agora trava
// também o contrário, que é o risco desta fase: uma rota **apagada do servidor**
// e esquecida no `openapi.yaml`.
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

	servidor := servidor(t)

	for _, r := range rotas {
		t.Run(r.metodo+" "+r.caminho, func(t *testing.T) {
			req := httptest.NewRequest(r.metodo, comParametrosPreenchidos(r.caminho), strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			servidor.Rotas().ServeHTTP(resp, req)

			// O que se afirma é estreito de propósito: **existe handler**. Não se
			// afirma o corpo, nem o estatuto de sucesso — um pedido vazio pode e
			// deve dar 400, e isso prova que alguém o leu.
			switch {
			case resp.Code == http.StatusNotFound && !respondeuUmHandler(resp):
				t.Errorf("o contrato declara %s %s e ninguém o serve: 404. "+
					"Falta registá-lo em Rotas().", r.metodo, r.caminho)
			case resp.Code == http.StatusMethodNotAllowed:
				t.Errorf("o contrato declara %s %s e a rota existe noutro método: 405. "+
					"Falta registar ESTE método em Rotas().", r.metodo, r.caminho)
			}
		})
	}
}

// oParametroDeProva é o que se põe onde o caminho traz `{algo}`.
//
// ⚠️ Um valor que **não** é nada de verdade, e de propósito: com um id de banco
// a sério, este teste ia à rede a cinco bancos de cada vez que o portão
// corresse. O que aqui se mede é registo de rota, não resposta.
const oParametroDeProva = "-parametro-de-prova-"

func comParametrosPreenchidos(caminho string) string {
	var partes []string
	for _, p := range strings.Split(caminho, "/") {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			p = oParametroDeProva
		}
		partes = append(partes, p)
	}
	return strings.Join(partes, "/")
}

// respondeuUmHandler distingue um 404 NOSSO do 404 do chi.
//
// ⚠️ **Sem isto o teste mentia nos dois sentidos**, e passou a mentir assim que
// o contrato ganhou o primeiro caminho com parâmetro: um handler que responde
// «não há banco nenhum com esse id» — que é a resposta certa ao parâmetro de
// prova — era lido como rota em falta. E a correcção preguiçosa (aceitar
// qualquer 404) apagava exactamente o defeito que a KAN-44 existe para apanhar.
//
// O que os separa é a forma: o nosso 404 é o envelope de erro em JSON, com
// código; o do chi é `text/plain` com «404 page not found». Um envelope só
// aparece se alguém leu o pedido.
func respondeuUmHandler(resp *httptest.ResponseRecorder) bool {
	if !strings.Contains(resp.Header().Get("Content-Type"), "application/json") {
		return false
	}
	var corpo struct {
		Erro struct {
			Codigo string `json:"codigo"`
		} `json:"erro"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &corpo); err != nil {
		return false
	}
	return corpo.Erro.Codigo != ""
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
