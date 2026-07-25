package bancos_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/prova"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O banco de mentira, escrito só para o teste. Não é um banco a sério: é o
// mínimo que atravessa o contrato inteiro — Pedido → payload → transporte →
// resposta → Oferta — pelas duas estratégias, com o transporte falso.
//
// Serve para duas coisas: afirmar que o contrato desta issue se consegue cumprir
// sem tocar na rede, e ser o molde do primeiro banco a sério (KAN-9).

const (
	urlSimulacao = "https://exemplo.invalido/api/simular"
	urlArranque  = "https://exemplo.invalido/simulador"
)

// respostaDoBanco é o JSON que o banco de mentira devolve. Um banco a sério lê
// isto no seu resposta.go, com uma função pura contra uma captura.
type respostaDoBanco struct {
	TAN       string `json:"tan"`
	Prestacao string `json:"prestacao"`
}

func lerOferta(id, nome string, corpo []byte) (dominio.Oferta, error) {
	var r respostaDoBanco
	if err := json.Unmarshal(corpo, &r); err != nil {
		return dominio.Oferta{}, fmt.Errorf("%s: resposta ilegível: %w", id, err)
	}
	tan, err := dominio.TaxaDeTexto(r.TAN)
	if err != nil {
		return dominio.Oferta{}, fmt.Errorf("%s: tan: %w", id, err)
	}
	prestacao, err := dominio.DinheiroDeTexto(r.Prestacao)
	if err != nil {
		return dominio.Oferta{}, fmt.Errorf("%s: prestação: %w", id, err)
	}
	return dominio.Oferta{BancoID: id, BancoNome: nome, TAN: &tan, Prestacao: &prestacao}, nil
}

func requisitosDeMentira(id, nome string) dominio.Requisitos {
	return dominio.Requisitos{
		BancoID:   id,
		BancoNome: nome,
		Custo:     dominio.CustoBarato,
		Inputs: []dominio.Input{
			{Campo: dominio.CampoValorImovel, Usa: true},
			{Campo: dominio.CampoMontante, Usa: true},
			{Campo: dominio.CampoPrazoAnos, Usa: true},
		},
		PeriodosFixos:     []int{5, 10},
		PeriodosFixosModo: dominio.ModoLista,
		EuriborImposto:    dominio.Euribor12M,
		PrazoMin:          5,
		PrazoMax:          40,
		IdadeMaximaFim:    75,
	}
}

func payload(p dominio.Pedido) url.Values {
	return url.Values{
		"valorImovel": {p.ValorImovel.String()},
		"montante":    {p.Montante.String()},
		"prazo":       {strconv.Itoa(p.PrazoAnos)},
	}
}

func postar(
	ctx context.Context, tr transporte.HTTPSimples, destino string, valores url.Values,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, destino, strings.NewReader(valores.Encode()))
	if err != nil {
		return nil, fmt.Errorf("montar o pedido: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := tr.Fazer(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("simular: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("o banco respondeu %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// bancoSimples corre pela estratégia HTTPSimples: um POST e mais nada.
type bancoSimples struct{ tr transporte.HTTPSimples }

func (b bancoSimples) ID() string                     { return "mentira" }
func (b bancoSimples) Nome() string                   { return "Banco de Mentira" }
func (b bancoSimples) Requisitos() dominio.Requisitos { return requisitosDeMentira(b.ID(), b.Nome()) }

func (b bancoSimples) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	corpo, err := postar(ctx, b.tr, urlSimulacao, payload(p))
	if err != nil {
		return dominio.Oferta{}, err
	}
	return lerOferta(b.ID(), b.Nome(), corpo)
}

// padraoHash é a leitura pura do HTML de arranque — o mesmo formato do
// HashRequest do Montepio (DOSSIE-BANCOS.md).
var padraoHash = regexp.MustCompile(`id="HashRequest"[^>]*value="([^"]*)"`)

func extrairHash(html []byte) (string, error) {
	achado := padraoHash.FindSubmatch(html)
	if achado == nil {
		return "", errors.New("o HTML de arranque não traz HashRequest")
	}
	return string(achado[1]), nil
}

// bancoComSessao corre pela estratégia HTTPComSessao: o GET de arranque fixa os
// cookies e traz o hash, e só depois vai o POST. É o desenho do Montepio, onde
// sem o arranque o gateway responde 410.
type bancoComSessao struct{ tr transporte.HTTPComSessao }

func (b bancoComSessao) ID() string                     { return "mentira-com-sessao" }
func (b bancoComSessao) Nome() string                   { return "Banco de Mentira com Sessão" }
func (b bancoComSessao) Requisitos() dominio.Requisitos { return requisitosDeMentira(b.ID(), b.Nome()) }

func (b bancoComSessao) Simular(ctx context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	sessao, err := b.tr.Arrancar(ctx, urlArranque)
	if err != nil {
		return dominio.Oferta{}, fmt.Errorf("%s: %w", b.ID(), err)
	}
	hash, err := extrairHash(sessao.HTML)
	if err != nil {
		return dominio.Oferta{}, fmt.Errorf("%s: %w", b.ID(), err)
	}

	corpo, err := postar(ctx, b.tr, urlSimulacao+"?hash="+url.QueryEscape(hash), payload(p))
	if err != nil {
		return dominio.Oferta{}, err
	}
	return lerOferta(b.ID(), b.Nome(), corpo)
}

func pedidoDeTeste(t *testing.T) dominio.Pedido {
	t.Helper()

	nascimento, err := dominio.DataDe(1990, time.March, 15)
	if err != nil {
		t.Fatalf("data de teste inválida: %v", err)
	}
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dominio.DinheiroDeInteiro(200_000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Titulares: []dominio.Titular{
			{DataNascimento: nascimento, RendimentoMensal: dominio.DinheiroDeInteiro(2_000)},
		},
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
	}
}

// respostaJSON é o que o transporte falso devolve — o JSON que um banco a sério
// teria numa captura.
const respostaJSON = `{"tan":"3.25","prestacao":"870.41"}`

const htmlDeArranque = `<html><body>` +
	`<input type="hidden" id="HashRequest" name="HashRequest" value="H4SH-D0-M0DUL0" />` +
	`</body></html>`

func confirmarOferta(t *testing.T, o dominio.Oferta, id, nome string) {
	t.Helper()

	if o.BancoID != id || o.BancoNome != nome {
		t.Errorf("oferta veio de %q/%q, esperava %q/%q", o.BancoID, o.BancoNome, id, nome)
	}
	if !o.Sucesso() {
		t.Fatalf("oferta com erro: %v", o.Erro)
	}
	if o.TAN == nil || o.TAN.String() != "3.25" {
		t.Errorf("TAN = %v, esperava 3.25", o.TAN)
	}
	if o.Prestacao == nil || o.Prestacao.String() != "870.41" {
		t.Errorf("prestação = %v, esperava 870.41", o.Prestacao)
	}
}

func TestBancoDeMentiraPelaEstrategiaHTTPSimples(t *testing.T) {
	t.Parallel()

	falso := &transporte.Falso{Corpo: respostaJSON}
	b := bancoSimples{tr: falso}

	if err := b.Requisitos().Validar(); err != nil {
		t.Fatalf("requisitos não servíveis: %v", err)
	}

	o, err := b.Simular(t.Context(), pedidoDeTeste(t))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}
	confirmarOferta(t, o, b.ID(), b.Nome())

	pedidos := falso.Pedidos()
	if len(pedidos) != 1 {
		t.Fatalf("o banco fez %d pedidos, esperava 1", len(pedidos))
	}
	if got := pedidos[0].URL.String(); got != urlSimulacao {
		t.Errorf("pediu a %q, esperava %q", got, urlSimulacao)
	}
	if !strings.Contains(string(pedidos[0].Corpo), "montante=200000") {
		t.Errorf("o payload não leva o montante: %q", pedidos[0].Corpo)
	}
}

func TestBancoDeMentiraPelaEstrategiaHTTPComSessao(t *testing.T) {
	t.Parallel()

	falso := &transporte.Falso{
		Corpo:          respostaJSON,
		HTMLDeArranque: []byte(htmlDeArranque),
	}
	b := bancoComSessao{tr: falso}

	o, err := b.Simular(t.Context(), pedidoDeTeste(t))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}
	confirmarOferta(t, o, b.ID(), b.Nome())

	arranques := falso.Arranques()
	if len(arranques) != 1 || arranques[0] != urlArranque {
		t.Fatalf("arranques = %v, esperava um só em %q", arranques, urlArranque)
	}

	pedidos := falso.Pedidos()
	if len(pedidos) != 1 {
		t.Fatalf("o banco fez %d pedidos, esperava 1", len(pedidos))
	}
	// O hash do POST tem de vir do HTML do arranque, e não de lado nenhum: é a
	// prova de que a sessão serviu para alguma coisa.
	if got := pedidos[0].URL.Query().Get("hash"); got != "H4SH-D0-M0DUL0" {
		t.Errorf("o POST levou hash=%q, esperava o do HTML de arranque", got)
	}
}

// Sem arranque não há pedido real. É a ordem que o Montepio impõe — o gateway
// responde 410 a quem chegue de fora da sessão —, e afirma-se aqui contando os
// pedidos que o transporte viu: zero.
func TestSessaoQueNaoArrancaNemChegaAPedirASimulacao(t *testing.T) {
	t.Parallel()

	falso := &transporte.Falso{
		Corpo:          respostaJSON,
		ErroDeArranque: errors.New("410 New open window with different context"),
	}
	b := bancoComSessao{tr: falso}

	_, err := b.Simular(t.Context(), pedidoDeTeste(t))
	if err == nil {
		t.Fatal("Simular devolveu nil com o arranque a falhar")
	}
	if !strings.Contains(err.Error(), "410") {
		t.Errorf("o erro não nomeia a falha do arranque: %v", err)
	}
	if n := len(falso.Pedidos()); n != 0 {
		t.Errorf("fez %d pedidos depois de o arranque falhar, esperava nenhum", n)
	}
}

// atrasoDoTransporte é maior do que o prazo e maior do que a folga de prova: é o
// que faz a diferença entre respeitar o ctx e não o respeitar ser mensurável.
const (
	atrasoDoTransporte = 2 * time.Second
	prazoCurto         = 50 * time.Millisecond
)

func TestSimularRespeitaOPrazoNasDuasEstrategias(t *testing.T) {
	t.Parallel()

	falso := &transporte.Falso{
		Corpo:          respostaJSON,
		HTMLDeArranque: []byte(htmlDeArranque),
		Atraso:         atrasoDoTransporte,
	}

	for _, b := range []bancos.Banco{bancoSimples{tr: falso}, bancoComSessao{tr: falso}} {
		t.Run(b.ID(), func(t *testing.T) {
			t.Parallel()
			prova.RespeitaPrazo(t, b, pedidoDeTeste(t), prazoCurto)
		})
	}
}

// bancoSurdo é o defeito que prova.RespeitaPrazo existe para apanhar: cumpre a
// interface, mas atira fora o ctx que lhe deram e chama o transporte com um novo.
type bancoSurdo struct{ dentro bancos.Banco }

func (b bancoSurdo) ID() string                     { return b.dentro.ID() }
func (b bancoSurdo) Nome() string                   { return b.dentro.Nome() }
func (b bancoSurdo) Requisitos() dominio.Requisitos { return b.dentro.Requisitos() }

//nolint:contextcheck // é o defeito que o teste a seguir mede: o ctx é atirado fora de propósito.
func (b bancoSurdo) Simular(_ context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	return b.dentro.Simular(context.Background(), p)
}

// O contrário do teste acima, medido em vez de argumentado: com o ctx atirado
// fora, Simular fica preso no transporte o atraso inteiro, muito para lá do
// prazo. É este o defeito que prova.RespeitaPrazo apanha — e vê-se aqui sem se
// ter de estragar código nenhum para o ver.
func TestBancoSurdoAoCtxFicaPresoNoTransporte(t *testing.T) {
	t.Parallel()

	const atraso = 300 * time.Millisecond

	falso := &transporte.Falso{Corpo: respostaJSON, Atraso: atraso}
	b := bancoSurdo{dentro: bancoSimples{tr: falso}}

	ctx, cancelar := context.WithTimeout(t.Context(), prazoCurto)
	defer cancelar()

	inicio := time.Now()
	if _, err := b.Simular(ctx, pedidoDeTeste(t)); err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if demorou := time.Since(inicio); demorou < atraso {
		t.Fatalf(
			"Simular voltou em %s com um prazo de %s: o banco surdo devia ter ficado preso os %s do transporte",
			demorou, prazoCurto, atraso,
		)
	}
}
