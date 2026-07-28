package santander_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/prova"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/santander"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O banco inteiro, contra as capturas e sem rede. O teste ao vivo está no
// santander_rede_test.go, por trás de `//go:build rede`.
//
// ⚠️ São QUATRO pedidos encadeados — configuração, limites, catálogo, simulação —
// e o transporte falso responde a cada caminho. É o primeiro banco escrito que
// descobre em runtime onde fica a sua própria API.

func TestOsRequisitosSaoServiveis(t *testing.T) {
	r := santander.Novo(&transporte.Falso{}).Requisitos()
	if err := r.Validar(); err != nil {
		t.Fatalf("os requisitos do Santander não são servíveis: %v", err)
	}
	if r.EuriborImposto != dominio.Euribor6M {
		t.Errorf("EuriborImposto = %q, e o Santander impõe a 6 meses", r.EuriborImposto)
	}
	// ⚠️ Os períodos declarados são os da MISTA. A fixa é ao contrato todo, e o
	// contrato publica uma lista só — a distinção vive na nota.
	if len(r.PeriodosFixos) != 3 {
		t.Errorf("períodos da mista = %v, e o catálogo tem 2, 3 e 4", r.PeriodosFixos)
	}
}

func TestSimularLeACapturaDaVariavel(t *testing.T) {
	banco := comCapturas(t, "variavel_6m")

	oferta, err := banco.Simular(t.Context(), pedidoBase(t))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}
	if oferta.BancoID != "santander" {
		t.Errorf("a oferta não se identifica: %q", oferta.BancoID)
	}
	if oferta.TAN == nil || oferta.TAEG == nil || oferta.Prestacao == nil || oferta.MTIC == nil {
		t.Fatalf("faltam campos nucleares: %+v", oferta)
	}
	// Sem produtos: o plano não bonificado, spread 1,9 constante.
	if oferta.Spread == nil || oferta.Spread.String() != "1.9" {
		t.Errorf("spread = %v, e o plano não bonificado tem 1,9", oferta.Spread)
	}
}

// TestUmBffForjadoNaoRecebeOsDadosDoPedido é a validação que a §3 manda manter.
//
// ⚠️ O `bff_url` é lido de um JSON remoto e usado para PUBLICAR os dados do
// pedido. Um `config.json` adulterado — ou um intermediário sobre ele — apontava
// as nossas chamadas para um host arbitrário: os dados saíam para lá, e a
// resposta que voltasse era servida como se fosse do Santander.
func TestUmBffForjadoNaoRecebeOsDadosDoPedido(t *testing.T) {
	forjados := []string{
		"https://atacante.example/roubar",
		"http://api-eeic.apis.santander.pt/santander", // http, não https
		"https://santander.pt.atacante.example/x",     // o sufixo tem de ser o domínio
		"",
	}

	for _, forjado := range forjados {
		t.Run(forjado, func(t *testing.T) {
			if santander.BFFSeguro(forjado) != "" {
				t.Errorf("%q foi aceite como endereço do banco", forjado)
			}
		})
	}

	// E o verdadeiro passa.
	bom := "https://api-eeic.apis.santander.pt/santander/eeic/home_simulations"
	if santander.BFFSeguro(bom) != bom {
		t.Errorf("o endereço a sério foi recusado: %q", bom)
	}
}

// TestComOConfigAdulteradoOsPedidosVaoParaOEnderecoConhecido fecha o mesmo
// buraco pelo lado do comportamento, e não da função.
func TestComOConfigAdulteradoOsPedidosVaoParaOEnderecoConhecido(t *testing.T) {
	adulterado := strings.Replace(
		string(captura(t, "config.resposta.json")),
		"https://api-eeic.apis.santander.pt/santander/eeic/home_simulations",
		"https://atacante.example/roubar", 1)

	var visitados []string
	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			visitados = append(visitados, p.URL.Host)
			return respostaPara(t, p, "variavel_6m", adulterado), nil
		},
	}

	if _, err := santander.Novo(falso).Simular(t.Context(), pedidoBase(t)); err != nil {
		t.Fatalf("Simular: %v", err)
	}
	for _, host := range visitados {
		if strings.Contains(host, "atacante") {
			t.Fatalf("os dados do pedido foram publicados em %q", host)
		}
	}
}

func TestAFixaEncaixaOPrazoNosQueOBancoPratica(t *testing.T) {
	banco := comCapturas(t, "fixa_30a")

	p := pedidoBase(t)
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = 28 // o banco tem 10, 20 e 30

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) == 0 {
		t.Fatal("pediram-se 28 anos de fixa e o banco só tem 10, 20 e 30: não houve ajuste")
	}
	if !strings.Contains(ajustes[0].Nota(), "10, 20 e 30") {
		t.Errorf("a nota não diz que prazos existem: %q", ajustes[0].Nota())
	}
}

func TestUmaIdadeForaDosLimitesNaoVaiARede(t *testing.T) {
	// O /credit_limit publica maxAge 80. ⚠️ Verificar antes de simular: um
	// pedido que o banco vai recusar gasta uma chamada para receber um erro que
	// os limites já diziam.
	banco := comCapturas(t, "variavel_6m")

	p := pedidoBase(t)
	p.Titulares = []dominio.Titular{{DataNascimento: nascidoHaAnos(t, 85)}}

	_, err := banco.Simular(t.Context(), p)
	if err == nil {
		t.Fatal("simulou-se para um titular de 85 anos, e o banco publica um máximo de 80")
	}
	if !strings.Contains(err.Error(), "80") {
		t.Errorf("o erro não nomeia o limite do banco: %v", err)
	}
}

func TestRespeitaOPrazoDoCtx(t *testing.T) {
	lento := &transporte.Falso{
		Estado: http.StatusOK,
		Corpo:  string(captura(t, "config.resposta.json")),
		Atraso: 5 * time.Second,
	}
	var banco bancos.Banco = santander.Novo(lento)
	prova.RespeitaPrazo(t, banco, pedidoBase(t), 300*time.Millisecond)
}

// --- ajudantes ------------------------------------------------------------------

// comCapturas monta o banco sobre um transporte que responde as quatro capturas
// pelo caminho pedido.
func comCapturas(t *testing.T, simulacao string) *santander.Banco {
	t.Helper()
	config := string(captura(t, "config.resposta.json"))
	return santander.Novo(&transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			return respostaPara(t, p, simulacao, config), nil
		},
	})
}

func respostaPara(t *testing.T, p transporte.PedidoGravado, simulacao, config string) *http.Response {
	t.Helper()
	switch {
	case strings.HasSuffix(p.URL.Path, "config.json"):
		return transporte.RespostaDeTexto(http.StatusOK, config)
	case strings.HasSuffix(p.URL.Path, "/credit_limit"):
		return transporte.RespostaDeTexto(http.StatusOK, string(captura(t, "credit_limit.resposta.json")))
	case strings.HasSuffix(p.URL.Path, "/rates"):
		return transporte.RespostaDeTexto(http.StatusOK, string(captura(t, "rates.resposta.json")))
	case strings.HasSuffix(p.URL.Path, "/get_by_rates"):
		return transporte.RespostaDeTexto(http.StatusOK, string(captura(t, simulacao+".resposta.json")))
	default:
		t.Errorf("pedido a um caminho que o Santander não tem: %q", p.URL.Path)
		return transporte.RespostaDeTexto(http.StatusNotFound, "")
	}
}

func captura(t *testing.T, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("capturas", nome))
	if err != nil {
		t.Fatalf("ler a captura %s: %v", nome, err)
	}
	return b
}

func pedidoBase(t *testing.T) dominio.Pedido {
	t.Helper()
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dominio.DinheiroDeInteiro(200_000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
		Titulares:   []dominio.Titular{{DataNascimento: nascidoHaAnos(t, 30)}},
	}
}

func nascidoHaAnos(t *testing.T, n int) dominio.Data {
	t.Helper()
	return dominio.DataDeInstante(time.Now().AddDate(-n, 0, 0))
}
