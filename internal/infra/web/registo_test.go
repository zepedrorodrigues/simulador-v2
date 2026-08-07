package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// O diário de pedidos (KAN-43).

// oSegredo é o que não pode aparecer no diário. Era uma chave de máquina do
// `/api/rate-catalog`; essa fronteira saiu com o varrimento (Fase 6, passo 5) e
// **a regra não saiu com ela** — o middleware continua a não poder registar a
// query string, venha nela o que vier.
const oSegredo = "segredo-que-nao-pode-ir-para-o-log"

func TestAQueryStringNuncaEntraNoDiario(t *testing.T) {
	// ⚠️ É a razão de este middleware ser escrito à mão em vez de se usar o
	// `middleware.Logger` do chi: aquele regista o URL inteiro. Um segredo que
	// entra num log é um segredo que não se apaga — fica na plataforma, nos
	// backups dela, e em quem os leia.
	//
	// ⚠️ **E hoje isto é uma guarda contra o futuro, não contra o presente:**
	// nenhuma rota actual leva segredos na query string. É precisamente por isso
	// que o teste fica — a regra tem de estar de pé **antes** de alguém acrescentar
	// a rota que os leve.
	var saida bytes.Buffer
	servidor := servidor(t).ComDiario(web.DiarioDeOmissao(&saida))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bancos?bank=cgd&api_key="+oSegredo, nil)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	escrito := saida.String()
	if escrito == "" {
		t.Fatal("o pedido não deixou linha nenhuma no diário")
	}
	if strings.Contains(escrito, oSegredo) {
		t.Errorf("o segredo foi parar ao diário:\n%s", escrito)
	}
	if strings.Contains(escrito, "bank=cgd") || strings.Contains(escrito, "api_key") {
		t.Errorf("a query string foi parar ao diário:\n%s", escrito)
	}
	if !strings.Contains(escrito, "/api/v1/bancos") {
		t.Errorf("o diário não diz sequer que caminho foi servido:\n%s", escrito)
	}
}

func TestALinhaDoDiarioCruzaComOIdentificadorQueOClienteRecebeu(t *testing.T) {
	// ⚠️ Era este o estado até 2026-07-28: o `X-Request-ID` era gerado e
	// publicado ao cliente, e nunca escrito em lado nenhum. Quem reclamasse com
	// o identificador na mão não tinha com que se cruzar do nosso lado.
	var saida bytes.Buffer
	servidor := servidor(t).ComDiario(web.DiarioDeOmissao(&saida))

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
	// Um pedido recusado é precisamente o que interessa ver num log, e se o
	// registo estivesse montado depois da guarda que o recusa era o único que não
	// aparecia.
	//
	// ⚠️ Era um 401 do `/api/rate-catalog`, que saiu com o varrimento. Passa a ser
	// o 429 do tecto por IP — que é agora a única guarda que recusa antes de
	// qualquer handler nosso correr, e está montada DEPOIS do registo por essa
	// mesma razão.
	var saida bytes.Buffer
	servidor := servidorComTecto(t, web.Tecto{Pedidos: 1, Janela: time.Minute}).
		ComDiario(web.DiarioDeOmissao(&saida))

	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		servidor.Rotas().ServeHTTP(httptest.NewRecorder(), req)
	}

	// A última linha é a do pedido recusado.
	linhas := strings.Split(strings.TrimSpace(saida.String()), "\n")
	var linha map[string]any
	if err := json.Unmarshal([]byte(linhas[len(linhas)-1]), &linha); err != nil {
		t.Fatalf("o pedido recusado não deixou linha legível: %v\n%s", err, saida.String())
	}
	if linha["estatuto"] != float64(http.StatusTooManyRequests) {
		t.Errorf("o diário diz estatuto=%v e a resposta foi 429", linha["estatuto"])
	}
}

func TestSemDiarioONadaSeEscreve(t *testing.T) {
	// Os testes que não estão a medir registo nenhum não têm de escolher um
	// destino — e um servidor sem diário não pode rebentar por isso.
	servidor := servidor(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	servidor.Rotas().ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("sem diário o servidor deixou de servir: %d", resp.Code)
	}
}
