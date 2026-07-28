package varrimento_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/cgd"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Os testes do resíduo da §7.4.
//
// Correm todos offline. Os dois últimos são o critério de pronto da KAN-16 — «o
// cálculo do resíduo tem teste contra observações gravadas» — e a observação
// gravada é mesmo isso: a captura real da CGD, passada pelo parser da CGD, sem
// rede nenhuma. Um resíduo afirmado sobre uma oferta escrita à mão prova a
// aritmética e não prova que a aritmética casa com o que os bancos devolvem.

// --- a aritmética -------------------------------------------------------------

func TestResiduoEZeroQuandoAPrestacaoDoBancoEAFrancesa(t *testing.T) {
	// 200 000 € a 3,00 % em 360 meses dá 843,2080674… — o banco publica 843,21,
	// e o resíduo é o cêntimo do arredondamento dele.
	o := observacao(t, oferta{
		montante: "200000", prazoAnos: 30,
		fases: []fase{{ateMes: 360, taxa: "3.00", prestacao: "843.21"}},
	})

	r, ok := varrimento.Residuo(o)
	if !ok {
		t.Fatal("não mediu resíduo numa observação que tem tudo")
	}
	if got := r.Decimal().StringFixed(2); got != "0.00" {
		t.Errorf("o resíduo é %s € e a prestação do banco é a francesa arredondada", got)
	}
}

func TestOSinalDizDeQueLadoSeDiverge(t *testing.T) {
	base := oferta{
		montante: "200000", prazoAnos: 30,
		fases: []fase{{ateMes: 360, taxa: "3.00", prestacao: "853.21"}},
	}
	acima, ok := varrimento.Residuo(observacao(t, base))
	if !ok {
		t.Fatal("não mediu")
	}
	if got := acima.Decimal().StringFixed(2); got != "10.00" {
		t.Errorf("o banco cobra 10 € acima da francesa e o resíduo é %s €", got)
	}

	base.fases[0].prestacao = "833.21"
	abaixo, ok := varrimento.Residuo(observacao(t, base))
	if !ok {
		t.Fatal("não mediu")
	}
	if got := abaixo.Decimal().StringFixed(2); got != "-10.00" {
		t.Errorf("o banco cobra 10 € abaixo da francesa e o resíduo é %s €", got)
	}
}

// TestOPrazoEODoPlanoENaoODoPedido é a reversão mais importante deste ficheiro.
//
// Os bancos encolhem prazos e dizem-no — a idade máxima ao fim do contrato é a
// razão comum. Medir contra o prazo pedido daria um resíduo enorme a acusar o
// banco de ter mudado, quando o que houve foi um ajuste que ele declarou.
func TestOPrazoEODoPlanoENaoODoPedido(t *testing.T) {
	// Pediram-se 30 anos e o banco deu 25: 200 000 € a 3,00 % em 300 meses são
	// 948,42 €, contra os 843,21 € que os 360 meses dariam.
	o := observacao(t, oferta{
		montante: "200000", prazoAnos: 30,
		fases: []fase{{ateMes: 300, taxa: "3.00", prestacao: "948.42"}},
	})

	r, ok := varrimento.Residuo(o)
	if !ok {
		t.Fatal("não mediu")
	}
	if r.Abs().Decimal().GreaterThan(centimos(t, "0.01")) {
		t.Errorf("o banco aplicou 25 anos e declarou-o no plano; o resíduo devia ser um cêntimo e é %s €",
			r.Decimal().StringFixed(2))
	}
}

// TestNaMistaMedeSeAPrimeiraFase: a fase fixa calcula-se sobre o prazo TODO, e
// é a comparação limpa. A segunda amortiza um capital que já vem do
// arredondamento de sessenta prestações do banco, e esse ruído não é sinal.
func TestNaMistaMedeSeAPrimeiraFase(t *testing.T) {
	o := observacao(t, oferta{
		montante: "200000", prazoAnos: 30,
		fases: []fase{
			{ateMes: 60, taxa: "3.00", prestacao: "843.21"},
			{ateMes: 360, taxa: "4.50", prestacao: "988.34"},
		},
	})

	r, ok := varrimento.Residuo(o)
	if !ok {
		t.Fatal("não mediu")
	}
	// Se medisse a segunda fase, a francesa sobre 200 000 € a 4,50 % em 360
	// meses daria 1 013,37 € e o resíduo saía a -25 € por razão nenhuma.
	if got := r.Decimal().StringFixed(2); got != "0.00" {
		t.Errorf("o resíduo da primeira fase é %s € — parece medido sobre a segunda", got)
	}
}

// --- o que não se mede ----------------------------------------------------------

func TestNaoHaResiduoOndeNaoHaOQueComparar(t *testing.T) {
	falha := varrimento.Observacao{
		Ponto: varrimento.Ponto{Cenario: "variavel/0/propria", Pedido: pedidoDeReferencia(t, "200000", 30)},
		Oferta: dominio.Falhar("cgd", "CGD", &dominio.ErroOferta{
			Codigo: dominio.ErroBancoIndisponivel, Mensagem: "em baixo",
		}),
	}
	if _, ok := varrimento.Residuo(falha); ok {
		t.Error("mediu resíduo numa observação de falha — não houve prestação nenhuma para comparar")
	}

	semFases := observacao(t, oferta{montante: "200000", prazoAnos: 30})
	if _, ok := varrimento.Residuo(semFases); ok {
		t.Error("mediu resíduo numa oferta sem plano de fases")
	}

	// ⚠️ Um plano que não descreve um plano. O zero silencioso aqui seria pior do
	// que não medir: leria-se como «bateu ao cêntimo».
	semMeses := observacao(t, oferta{
		montante: "200000", prazoAnos: 30,
		fases: []fase{{ateMes: 0, taxa: "3.00", prestacao: "843.21"}},
	})
	if _, ok := varrimento.Residuo(semMeses); ok {
		t.Error("mediu resíduo sobre um plano de zero meses")
	}
}

// --- contra observações gravadas -------------------------------------------------

// TestResiduoDaCapturaRealDaCGD é o critério de pronto: a resposta real da CGD,
// gravada a 2026-07-26, passada pelo parser real da CGD, sem rede.
//
// Medido a 2026-07-28: variável **-0,0048 €** e mista a 5 anos **-0,0036 €**.
// Meio cêntimo é o arredondamento da prestação que a CGD publica, e é a ordem de
// grandeza que interessa reter: o resíduo desta série é ruído de arredondamento,
// portanto qualquer coisa aos euros é sinal e não é ruído. Medida a última fase
// em vez da primeira, o mesmo cenário dá **6,10 €** — mil vezes mais.
//
// ⚠️ Lê as capturas do pacote do banco por caminho relativo, e é deliberado.
// Copiar os números para uma fixture deste pacote afirmaria a aritmética contra
// uma cópia que ninguém volta a olhar; o que interessa medir é se o resíduo
// fecha sobre o que o parser produz. As capturas são versionadas e obrigatórias
// (CONTRATO-BANCO.md §3), portanto o caminho é tão estável como o pacote.
func TestResiduoDaCapturaRealDaCGD(t *testing.T) {
	casos := []struct {
		nome     string
		calculo  string
		pedido   dominio.Pedido
		tolera   string
		esperado string
	}{
		{
			// Variável, 200 000 € em 30 anos: TAN 3,946 % e prestação 948,61 €.
			nome:    "variável",
			calculo: "variavel_ltv80",
			pedido:  pedidoDeReferencia(t, "200000", 30),
			tolera:  "0.05",
		},
		{
			// Mista a 5 anos: duas fases, e mede-se a primeira.
			nome:    "mista a 5 anos",
			calculo: "mista_5a",
			pedido:  comMista(pedidoDeReferencia(t, "200000", 30), 5),
			tolera:  "0.05",
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			banco := cgdDaCaptura(t, c.calculo)

			oferta, err := banco.Simular(t.Context(), c.pedido)
			if err != nil {
				t.Fatalf("simular contra a captura: %v", err)
			}
			oferta.CapturadoEm = time.Now()

			o := varrimento.Observacao{
				Ponto:  varrimento.Ponto{Cenario: "cenario-de-ensaio", Pedido: c.pedido},
				Oferta: oferta,
			}

			r, ok := varrimento.Residuo(o)
			if !ok {
				t.Fatal("a captura real não produziu resíduo nenhum")
			}
			if r.Abs().Decimal().GreaterThan(centimos(t, c.tolera)) {
				t.Errorf("o resíduo da captura é %s € e a tolerância da primeira fase é %s €"+
					" — ou o parser leu outro campo, ou a CGD mudou de forma de calcular",
					r.Decimal().StringFixed(2), c.tolera)
			}
			t.Logf("resíduo medido: %s €", r.Decimal().StringFixed(4))
		})
	}
}

// --- ajudantes ------------------------------------------------------------------

type fase struct {
	ateMes    int
	taxa      string
	prestacao string
}

type oferta struct {
	montante  string
	prazoAnos int
	fases     []fase
}

// observacao monta uma observação de sucesso com o plano descrito.
func observacao(t *testing.T, o oferta) varrimento.Observacao {
	t.Helper()

	fases := make([]dominio.Fase, 0, len(o.fases))
	for _, f := range o.fases {
		fases = append(fases, dominio.Fase{
			AteMes:    f.ateMes,
			Taxa:      taxaDeTeste(t, f.taxa),
			Prestacao: dinheiroDeTeste(t, f.prestacao),
		})
	}

	tan := taxaDeTeste(t, "3.00")
	prestacao := dinheiroDeTeste(t, "843.21")
	return varrimento.Observacao{
		Ponto: varrimento.Ponto{
			Cenario: "variavel/0/propria",
			Pedido:  pedidoDeReferencia(t, o.montante, o.prazoAnos),
		},
		Oferta: dominio.Oferta{
			BancoID: "ensaio", BancoNome: "Banco de ensaio",
			TAN: &tan, Prestacao: &prestacao,
			Fases:       fases,
			CapturadoEm: time.Now(),
		},
	}
}

func pedidoDeReferencia(t *testing.T, montante string, prazoAnos int) dominio.Pedido {
	t.Helper()
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dinheiroDeTeste(t, montante),
		PrazoAnos:   prazoAnos,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
		Titulares:   []dominio.Titular{{DataNascimento: dominio.DataDeInstante(time.Now().AddDate(-35, 0, 0))}},
	}
}

func comMista(p dominio.Pedido, periodo int) dominio.Pedido {
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = &periodo
	return p
}

// cgdDaCaptura monta a CGD sobre um transporte que responde as capturas
// gravadas — os três caminhos que ela percorre numa simulação.
func cgdDaCaptura(t *testing.T, calculo string) *cgd.Banco {
	t.Helper()

	home := capturaDaCGD(t, "home.html")
	limites := capturaDaCGD(t, "limites_propria.resposta.json")
	resposta := capturaDaCGD(t, calculo+".resposta.json")

	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			switch p.URL.Path {
			case "/":
				return transporte.RespostaDeTexto(http.StatusOK, home), nil
			case "/limits":
				return transporte.RespostaDeTexto(http.StatusOK, limites), nil
			case "/calculate":
				return transporte.RespostaDeTexto(http.StatusOK, resposta), nil
			default:
				t.Errorf("pedido a um caminho que a CGD não tem: %q", p.URL.Path)
				return transporte.RespostaDeTexto(http.StatusNotFound, ""), nil
			}
		},
	}
	return cgd.Novo(falso)
}

func capturaDaCGD(t *testing.T, nome string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "bancos", "cgd", "capturas", nome))
	if err != nil {
		t.Fatalf("ler a captura %s da CGD: %v", nome, err)
	}
	return string(b)
}

func taxaDeTeste(t *testing.T, s string) dominio.Taxa {
	t.Helper()
	x, err := dominio.TaxaDeTexto(s)
	if err != nil {
		t.Fatalf("taxa de teste inválida %q: %v", s, err)
	}
	return x
}

func dinheiroDeTeste(t *testing.T, s string) dominio.Dinheiro {
	t.Helper()
	d, err := dominio.DinheiroDeTexto(s)
	if err != nil {
		t.Fatalf("montante de teste inválido %q: %v", s, err)
	}
	return d
}

// centimos é uma tolerância em euros, para comparar contra um resíduo.
func centimos(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	return dinheiroDeTeste(t, s).Decimal()
}
