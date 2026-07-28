package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// O diário de pedidos (KAN-43).

// aChave é uma chave a sério, do tamanho que o `apikey.go` exige.
const aChave = "chave-secreta-de-32-caracteres!!"

func TestAQueryStringNuncaEntraNoDiario(t *testing.T) {
	// ⚠️ É a razão de este middleware ser escrito à mão em vez de se usar o
	// `middleware.Logger` do chi: aquele regista o URL inteiro. Uma chave que
	// entra num log é uma chave que não se apaga — fica na plataforma, nos
	// backups dela, e em quem os leia.
	var saida bytes.Buffer
	servidor := servidorComCatalogo(t, nil, aChave).ComDiario(web.DiarioDeOmissao(&saida))

	req := httptest.NewRequest(http.MethodGet, "/api/rate-catalog?bank=cgd&api_key="+aChave, nil)
	req.Header.Set("X-API-Key", aChave)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	escrito := saida.String()
	if escrito == "" {
		t.Fatal("o pedido não deixou linha nenhuma no diário")
	}
	if strings.Contains(escrito, aChave) {
		t.Errorf("a chave foi parar ao diário:\n%s", escrito)
	}
	if strings.Contains(escrito, "bank=cgd") || strings.Contains(escrito, "api_key") {
		t.Errorf("a query string foi parar ao diário:\n%s", escrito)
	}
	if !strings.Contains(escrito, "/api/rate-catalog") {
		t.Errorf("o diário não diz sequer que caminho foi servido:\n%s", escrito)
	}
}

func TestALinhaDoDiarioCruzaComOIdentificadorQueOClienteRecebeu(t *testing.T) {
	// ⚠️ Era este o estado até 2026-07-28: o `X-Request-ID` era gerado e
	// publicado ao cliente, e nunca escrito em lado nenhum. Quem reclamasse com
	// o identificador na mão não tinha com que se cruzar do nosso lado.
	var saida bytes.Buffer
	servidor := servidorComCatalogo(t, nil).ComDiario(web.DiarioDeOmissao(&saida))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	doCliente := resp.Header().Get("X-Request-ID")
	if doCliente == "" {
		t.Fatal("a resposta saiu sem X-Request-ID")
	}

	var linha map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(saida.String())), &linha); err != nil {
		t.Fatalf("a linha do diário não é JSON: %v\n%s", err, saida.String())
	}
	if linha["request_id"] != doCliente {
		t.Errorf("o diário diz request_id=%v e o cliente recebeu %q", linha["request_id"], doCliente)
	}
	if linha["estatuto"] != float64(http.StatusOK) {
		t.Errorf("o diário diz estatuto=%v e a resposta foi %d", linha["estatuto"], resp.Code)
	}
	if linha["metodo"] != http.MethodGet || linha["caminho"] != "/healthz" {
		t.Errorf("o diário não descreve o pedido: %v", linha)
	}
}

func TestUmPedidoRecusadoTambemDeixaRasto(t *testing.T) {
	// Um 401 é precisamente o que interessa ver num log — e se o registo
	// estivesse depois da guarda de chave, era o único que não aparecia.
	var saida bytes.Buffer
	servidor := servidorComCatalogo(t, nil, aChave).ComDiario(web.DiarioDeOmissao(&saida))

	req := httptest.NewRequest(http.MethodGet, "/api/rate-catalog", nil)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("esperava 401 sem chave, veio %d", resp.Code)
	}
	var linha map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(saida.String())), &linha); err != nil {
		t.Fatalf("o pedido recusado não deixou linha legível: %v\n%s", err, saida.String())
	}
	if linha["estatuto"] != float64(http.StatusUnauthorized) {
		t.Errorf("o diário diz estatuto=%v e a resposta foi 401", linha["estatuto"])
	}
}

func TestSemDiarioONadaSeEscreve(t *testing.T) {
	// Os testes que não estão a medir registo nenhum não têm de escolher um
	// destino — e um servidor sem diário não pode rebentar por isso.
	servidor := servidorComCatalogo(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("sem diário o servidor deixou de servir: %d", resp.Code)
	}
}
