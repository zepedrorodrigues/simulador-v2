package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

// A fronteira HTTP, contra observações gravadas e **sem base nem rede**.
//
// ⚠️ A fonte é injectada, e é isso que torna estes testes possíveis sem Postgres
// montado. O que aqui se afirma é a tradução e as guardas — os números têm os
// seus testes no `aplicacao/comparar`.

func TestUmPedidoUmaRespostaComTodosOsBancos(t *testing.T) {
	s := servidor(t, observacoesDeQuatroBancos(t))

	resposta := comparar(t, s, corpoDePedido(nil))

	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d: %s", resposta.Code, resposta.Body.String())
	}
	// ⚠️ Uma resposta e não duas: não há identificador para sondar, e é o ponto
	// inteiro da inversão da §1.
	var c api.Comparacao
	lerJSON(t, resposta, &c)

	// ⚠️ A contagem sai do REGISTO e não é um número fixo. O que este teste
	// afirma é «um pedido, uma resposta com todos os bancos» — e um banco novo
	// não é uma regressão disso. Quem apanha um banco que entrou ou saiu sem
	// ninguém dar por isso é o teste do subcomando, que conta os PONTOS de cada
	// um: aí o número tem de ser fixo, porque é o custo da corrida.
	if esperados := len(bancos.Predefinido().IDs()); len(c.Ofertas) != esperados {
		t.Fatalf("o registo tem %d bancos e a resposta trouxe %d ofertas", esperados, len(c.Ofertas))
	}
	if c.CalculadoEm.IsZero() {
		t.Error("a comparação não diz quando foi calculada")
	}

	for _, o := range c.Ofertas {
		if !o.Sucesso {
			t.Errorf("%s falhou: %+v", o.BancoId, o.Erro)
			continue
		}
		// ⚠️ E cada oferta diz de quando é o PREÇO, que é outra data: o
		// `calculado_em` é de agora, o `capturado_em` é do varrimento.
		if o.CapturadoEm == nil {
			t.Errorf("%s: a oferta não diz quando o preço foi medido", o.BancoId)
			continue
		}
		if !o.CapturadoEm.Before(c.CalculadoEm) {
			t.Errorf("%s: o preço foi medido em %s e a resposta calculada em %s",
				o.BancoId, o.CapturadoEm, c.CalculadoEm)
		}
	}
}

// TestUmaOfertaSemDataDoVarrimentoNaoEServida é a guarda que a KAN-13 pede.
//
// ⚠️ O número existe — o que falta é a data. Servi-lo era apresentá-lo como
// cotado agora, e a §4 é explícita: «não se serve um número calculado como se
// fosse cotado pelo banco». Desce a falha, e a mensagem diz porquê.
func TestUmaOfertaSemDataDoVarrimentoNaoEServida(t *testing.T) {
	obs := observacoesDeQuatroBancos(t)
	for i := range obs {
		if obs[i].Oferta.BancoID == "cgd" {
			obs[i].Oferta.CapturadoEm = time.Time{}
		}
	}

	resposta := comparar(t, servidor(t, obs), corpoDePedido([]string{"cgd"}))
	var c api.Comparacao
	lerJSON(t, resposta, &c)

	if len(c.Ofertas) != 1 {
		t.Fatalf("esperava uma oferta, vieram %d", len(c.Ofertas))
	}
	o := c.Ofertas[0]
	if o.Sucesso {
		tan := "sem TAN"
		if o.Tan != nil {
			tan = fmt.Sprintf("%.3f %%", *o.Tan)
		}
		t.Fatalf("serviu-se uma oferta sem a data do varrimento, com TAN %s — "+
			"um preço sem data apresenta-se como se fosse de agora", tan)
	}
	if !strings.Contains(o.Erro.Mensagem, "não diz de quando é o preço") {
		t.Errorf("a recusa não nomeia a falta da data: %q", o.Erro.Mensagem)
	}
}

// TestSemVarrimentoNaoSeCulpaOsBancos: o serviço não ter dados é diferente de os
// bancos estarem em baixo, e a resposta tem de o distinguir.
func TestSemVarrimentoNaoSeCulpaOsBancos(t *testing.T) {
	s, err := web.Novo(fonteVazia{}, nil, bancos.Predefinido(), nil, relogio)
	if err != nil {
		t.Fatalf("Novo: %v", err)
	}

	resposta := comparar(t, s, corpoDePedido(nil))

	if resposta.Code != http.StatusServiceUnavailable {
		t.Errorf("estado %d, esperava 503", resposta.Code)
	}
	var erro api.RespostaErro
	lerJSON(t, resposta, &erro)
	if erro.Erro.Codigo != "sem_varrimento" {
		t.Errorf("código %q, e o que falta são dados nossos e não os bancos", erro.Erro.Codigo)
	}
	// ⚠️ Em português, para uma pessoa ler. O v1 devolvia texto solto e a UI não
	// distinguia «este banco não faz isto» de «este banco está em baixo».
	if !strings.Contains(erro.Erro.Mensagem, "preços varridos") {
		t.Errorf("a mensagem não explica o que falta: %q", erro.Erro.Mensagem)
	}
}

// TestUmBancoPedidoSemSerieVemNaRespostaComoRecusa é a KAN-45 à fronteira,
// reproduzindo a medição da issue: varrimento de dois bancos, pedido de todos.
//
// ⚠️ **Este teste é o que faltava para o defeito ser visível daqui.** A resposta
// filtrava-se à saída, e filtrar só sabe TIRAR: um banco que a grelha não tinha
// nunca chegava à lista para poder ser filtrado, e saía sem uma palavra. Medido
// a 2026-07-29 contra a base migrada de raiz — pediram-se 5, vieram 2.
//
// A reversão é no `Comparar` e não aqui: pô-lo a percorrer `c.Bancos()` outra
// vez. Esta afirmação falha a nomear os bancos que se evaporaram — medido, os
// três que ficaram por varrer.
//
// ⚠️ Reverter só o lado do `web` — passar `nil` ao `Comparar` e voltar a filtrar
// à saída — **não** faz este teste falhar, e isso é informação e não um defeito
// do teste: com a lista vazia a valer «todos os conhecidos», o `Comparar` já
// devolve os cinco e o filtro não tem o que tirar. Quem partir isto outra vez
// parte-o no `Comparar`, que é onde este teste aperta.
func TestUmBancoPedidoSemSerieVemNaRespostaComoRecusa(t *testing.T) {
	todos := bancos.Predefinido().IDs()
	if len(todos) < 3 {
		t.Skipf("o registo tem %d bancos e este teste precisa de pelo menos 3", len(todos))
	}
	varridos, porVarrer := todos[:2], todos[2:]

	var obs []varrimento.Observacao
	for _, id := range varridos {
		obs = append(obs, observacoesDeUmBanco(t, id)...)
	}
	s := servidor(t, obs)

	resposta := comparar(t, s, corpoDePedido(todos))
	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d, esperava 200: %s", resposta.Code, resposta.Body.String())
	}

	var comparacao api.Comparacao
	lerJSON(t, resposta, &comparacao)

	vieram := map[string]api.Oferta{}
	for _, o := range comparacao.Ofertas {
		vieram[o.BancoId] = o
	}
	for _, id := range todos {
		if _, veio := vieram[id]; !veio {
			t.Errorf("pediu-se o %q e ele não vem na resposta, nem sequer como recusa", id)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// Os varridos respondem; os que ninguém varreu recusam, e dizem de quem é a
	// falta — nossa. Culpá-los levava quem lê a concluir que estão em baixo.
	for _, id := range porVarrer {
		o := vieram[id]
		if o.Sucesso {
			t.Errorf("o %q não foi varrido e ainda assim deu oferta", id)
			continue
		}
		if o.Erro == nil || o.Erro.Codigo != "sem_serie" {
			t.Errorf("o %q veio com %+v, e a KAN-45 pede o código sem_serie", id, o.Erro)
		}
	}
}

// TestUmIdQueNaoEBancoNenhumEUm400: «ainda não temos preços deste banco» e «não
// há tal banco» são coisas diferentes, e a segunda é erro de quem pergunta.
func TestUmIdQueNaoEBancoNenhumEUm400(t *testing.T) {
	s := servidor(t, observacoesDeQuatroBancos(t))

	resposta := comparar(t, s, corpoDePedido([]string{"banco-que-nunca-existiu"}))
	if resposta.Code != http.StatusBadRequest {
		t.Fatalf("estado %d, esperava 400: %s", resposta.Code, resposta.Body.String())
	}

	var erro api.RespostaErro
	lerJSON(t, resposta, &erro)
	if erro.Erro.Campo == nil || *erro.Erro.Campo != "bancos" {
		t.Errorf("o erro não aponta ao campo bancos: %+v", erro.Erro)
	}
	if !strings.Contains(erro.Erro.Mensagem, "banco-que-nunca-existiu") {
		t.Errorf("a mensagem não diz qual o id que não existe: %q", erro.Erro.Mensagem)
	}
}

func TestUmPedidoInvalidoNomeiaOCampo(t *testing.T) {
	s := servidor(t, observacoesDeQuatroBancos(t))

	corpo := corpoDePedido(nil)
	corpo.Pedido.Montante = 900_000 // acima do valor do imóvel

	resposta := comparar(t, s, corpo)
	if resposta.Code != http.StatusBadRequest {
		t.Fatalf("estado %d, esperava 400", resposta.Code)
	}

	var erro api.RespostaErro
	lerJSON(t, resposta, &erro)
	if erro.Erro.Campo == nil || *erro.Erro.Campo != "montante" {
		t.Errorf("o erro não nomeia o campo: %+v", erro.Erro)
	}
}

// TestUmProdutoDeOutroBancoNaoPassa: o prefixo do id é estrutura e não convenção
// de leitura — é por ele que a selecção se reparte pelos bancos.
func TestUmProdutoDeOutroBancoNaoPassa(t *testing.T) {
	s := servidor(t, observacoesDeQuatroBancos(t))

	corpo := corpoDePedido(nil)
	corpo.Produtos = &map[string][]string{"cgd": {"novobanco:protecao"}}

	resposta := comparar(t, s, corpo)
	if resposta.Code != http.StatusBadRequest {
		t.Fatalf("estado %d, esperava 400", resposta.Code)
	}
}

func TestOsBancosSaemDoRegistoEDosRequisitos(t *testing.T) {
	s := servidor(t, observacoesDeQuatroBancos(t))

	pedido := httptest.NewRequest(http.MethodGet, "/api/v1/bancos", nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	if resposta.Code != http.StatusOK {
		t.Fatalf("estado %d: %s", resposta.Code, resposta.Body.String())
	}
	var r api.BancosResposta
	lerJSON(t, resposta, &r)

	if len(r.Bancos) != len(bancos.Predefinido().IDs()) {
		t.Errorf("o registo tem %d bancos e a resposta traz %d", len(bancos.Predefinido().IDs()), len(r.Bancos))
	}
	if len(r.InputsCanonicos) == 0 {
		t.Error("a resposta não traz o vocabulário do formulário")
	}

	// ⚠️ O Banco CTT impõe a Euribor e não a deixa escolher. Vazio nas opções
	// não é «não sei» — é «o banco impõe o seu», e o imposto vai ao lado.
	for _, b := range r.Bancos {
		if b.Id != "bancoctt" {
			continue
		}
		if len(b.EuriborOpcoes) != 0 {
			t.Errorf("o Banco CTT declara %d opções de Euribor e não deixa escolher", len(b.EuriborOpcoes))
		}
		if b.EuriborImposto == nil || *b.EuriborImposto != "12m" {
			t.Errorf("o Banco CTT impõe a Euribor a 12 meses, e a resposta diz %v", b.EuriborImposto)
		}
	}
}

func TestTodasAsRespostasLevamRequestID(t *testing.T) {
	s := servidor(t, observacoesDeQuatroBancos(t))

	pedido := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)

	// ⚠️ Na resposta e não só no log: sem ele, quem reporta um problema não tem
	// como dizer qual das respostas correu mal.
	if resposta.Header().Get("X-Request-ID") == "" {
		t.Error("a resposta não traz X-Request-ID")
	}
}

// --- ajudantes ---------------------------------------------------------------------

var relogio = func() time.Time { return time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC) }

type fonteEmMemoria struct{ obs []varrimento.Observacao }

func (f fonteEmMemoria) UltimoVarrimento(context.Context) ([]varrimento.Observacao, error) {
	return f.obs, nil
}

type fonteVazia struct{}

func (fonteVazia) UltimoVarrimento(context.Context) ([]varrimento.Observacao, error) {
	return nil, dominio.ErrSemTitulares // qualquer erro serve: o que se afirma é o 503
}

func servidor(t *testing.T, obs []varrimento.Observacao) *web.Servidor {
	t.Helper()
	s, err := web.Novo(fonteEmMemoria{obs: obs}, nil, bancos.Predefinido(), nil, relogio)
	if err != nil {
		t.Fatalf("Novo: %v", err)
	}
	return s
}

func comparar(t *testing.T, s *web.Servidor, corpo api.ComparacaoPedido) *httptest.ResponseRecorder {
	t.Helper()
	bruto, err := json.Marshal(corpo)
	if err != nil {
		t.Fatalf("serializar o pedido: %v", err)
	}
	pedido := httptest.NewRequest(http.MethodPost, "/api/v1/comparacoes", bytes.NewReader(bruto))
	pedido.Header.Set("Content-Type", "application/json")

	resposta := httptest.NewRecorder()
	s.Rotas().ServeHTTP(resposta, pedido)
	return resposta
}

func corpoDePedido(ids []string) api.ComparacaoPedido {
	return api.ComparacaoPedido{
		Bancos: ids,
		Pedido: api.Pedido{
			ValorImovel: 400_000,
			Montante:    320_000,
			PrazoAnos:   30,
			RateType:    api.PedidoRateType(dominio.TaxaVariavel),
			Finalidade:  api.PedidoFinalidade(dominio.FinalidadePropria),
			Localizacao: string(dominio.LocalizacaoContinente),
			Titulares: []api.Titular{{
				DataNascimento:   openapi_types.Date{Time: time.Date(1996, 1, 15, 0, 0, 0, 0, time.UTC)},
				RendimentoMensal: 2000,
			}},
		},
	}
}

func lerJSON(t *testing.T, r *httptest.ResponseRecorder, alvo any) {
	t.Helper()
	if err := json.Unmarshal(r.Body.Bytes(), alvo); err != nil {
		t.Fatalf("ler a resposta (%s): %v", r.Body.String(), err)
	}
}

// observacoesDeQuatroBancos monta um varrimento com a forma que o real tem: os
// degraus da escala, a referência, os produtos e os dois extremos de prazo, para
// cada banco registado.
func observacoesDeQuatroBancos(t *testing.T) []varrimento.Observacao {
	t.Helper()

	var obs []varrimento.Observacao
	for _, id := range bancos.Predefinido().IDs() {
		obs = append(obs, observacoesDeUmBanco(t, id)...)
	}
	return obs
}

func observacoesDeUmBanco(t *testing.T, id string) []varrimento.Observacao {
	t.Helper()

	var obs []varrimento.Observacao
	for _, d := range []struct{ de, ate, spread string }{
		{"0.30", "0.70", "2.000"},
		{"0.70", "0.90", "1.350"},
	} {
		spread := taxa(t, d.spread)
		o := observacao(t, id, "variavel/0/propria", 30, spread)
		o.Degrau = &dominio.DegrauLTV{De: racio(t, d.de), Ate: racio(t, d.ate), Spread: spread}
		obs = append(obs, o)
	}

	obs = append(obs,
		observacao(t, id, "variavel/0/propria", 30, taxa(t, "1.350")),
		observacao(t, id, "variavel/0/propria", 10, taxa(t, "1.350")),
		observacao(t, id, "variavel/0/propria", 40, taxa(t, "1.350")),
	)
	return obs
}

func observacao(t *testing.T, bancoID, cenario string, prazoAnos int, spread dominio.Taxa) varrimento.Observacao {
	t.Helper()

	euribor := taxa(t, "2.450")
	tan := euribor.Add(spread)
	meses := prazoAnos * 12

	encargos := dominio.Encargos{Antecipado: racio(t, "0.005"), Recorrente: taxa(t, "0.25")}
	taeg, mtic, err := encargos.Aplicar(dominio.DinheiroDeInteiro(320_000),
		[]dominio.Trecho{{Meses: meses, Anual: tan}})
	if err != nil {
		t.Fatalf("TAEG de prova: %v", err)
	}
	plano, err := dominio.PlanoFrances(dominio.DinheiroDeInteiro(320_000),
		[]dominio.Trecho{{Meses: meses, Anual: tan}})
	if err != nil {
		t.Fatalf("plano de prova: %v", err)
	}
	prestacao := plano.Fases[0].Prestacao

	return varrimento.Observacao{
		Ponto: varrimento.Ponto{
			Cenario: cenario,
			Pedido: dominio.Pedido{
				ValorImovel: dominio.DinheiroDeInteiro(400_000),
				Montante:    dominio.DinheiroDeInteiro(320_000),
				PrazoAnos:   prazoAnos,
				TipoTaxa:    dominio.TaxaVariavel,
				Finalidade:  dominio.FinalidadePropria,
				Localizacao: dominio.LocalizacaoContinente,
			},
		},
		Oferta: dominio.Oferta{
			BancoID: bancoID, BancoNome: bancoID,
			TAN: &tan, TAEG: &taeg, MTIC: &mtic, Spread: &spread,
			EuriborValor: &euribor, Indexante: dominio.Euribor6M,
			Prestacao:   &prestacao,
			Fases:       plano.Fases,
			CapturadoEm: relogio().Add(-6 * time.Hour),
		},
	}
}

func taxa(t *testing.T, s string) dominio.Taxa {
	t.Helper()
	x, err := dominio.TaxaDeTexto(s)
	if err != nil {
		t.Fatalf("taxa inválida %q: %v", s, err)
	}
	return x
}

func racio(t *testing.T, s string) dominio.Racio {
	t.Helper()
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		t.Fatalf("rácio inválido %q: %v", s, err)
	}
	return r
}
