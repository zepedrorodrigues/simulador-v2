package cgd_test

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/cgd"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/prova"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Todos os testes deste ficheiro correm offline, contra as capturas de
// `capturas/` — respostas reais da CGD, gravadas a 2026-07-26. O teste ao vivo
// está em cgd_rede_test.go, por trás de `//go:build rede`, e não corre no
// portão.

// A taxa variável: os números da captura, lidos um a um.
//
// ⚠️ O MTIC é a afirmação sobre o formato: a CGD escreve `361 670,15` com
// espaço não-quebrável a separar os milhares (bytes C2 A0, medido). Passar isso
// por um parser ingénuo dá 361 ou erro.
func TestVariavelLeOsNumerosDaCaptura(t *testing.T) {
	banco, _ := montar(t, cenario{calculo: "variavel_ltv80"})

	oferta, err := banco.Simular(t.Context(), pedidoBase())
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	verTaxa(t, "TAN", oferta.TAN, "3.946")
	verTaxa(t, "TAEG", oferta.TAEG, "4.5")
	verTaxa(t, "spread", oferta.Spread, "1.35")
	verTaxa(t, "Euribor", oferta.EuriborValor, "2.596")
	verDinheiro(t, "prestação", oferta.Prestacao, "948.61")
	verDinheiro(t, "MTIC", oferta.MTIC, "361670.15")

	if oferta.Indexante != dominio.Euribor6M {
		t.Errorf("indexante: esperava 6m, veio %q", oferta.Indexante)
	}
	if len(oferta.Fases) != 1 || oferta.Fases[0].AteMes != 360 {
		t.Fatalf("uma variável tem uma fase até ao mês 360, vieram %+v", oferta.Fases)
	}
	if len(oferta.Ajustes()) != 0 {
		t.Errorf("o pedido coube tal como foi feito: não devia haver ajustes, vieram %+v", oferta.Ajustes())
	}
}

// --- as duas colunas de preço ---------------------------------------------------

// Escolher os packs dá a segunda coluna de preço da mesma resposta, e não uma
// nota sobre ela.
//
// ⚠️ Este é o teste que a KAN-33 obriga a existir. A CGD devolve as duas
// variantes no mesmo corpo — `BaseResult` e `DiscountedResult` — e o desconto
// entre elas é de 0,700 p.p., 78,64 € por mês neste cenário. Antes da KAN-33 o
// `comPacks` estava preso em falso e a segunda coluna só saía como prosa: um
// número que ninguém podia ordenar ao lado dos outros bancos.
func TestPacksEscolhidosDaoASegundaColunaDePreco(t *testing.T) {
	banco, _ := montar(t, cenario{calculo: "variavel_ltv80"})

	oferta, err := banco.Simular(t.Context(), comPacks(pedidoBase()))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	verTaxa(t, "spread", oferta.Spread, "0.65")
	verTaxa(t, "TAN", oferta.TAN, "3.246")
	verDinheiro(t, "prestação", oferta.Prestacao, "869.97")

	if len(oferta.ProdutosAplicados) != 1 || oferta.ProdutosAplicados[0] != cgd.ProdutoPacks {
		t.Errorf("a oferta tem de dizer que aplicou os packs, disse %v", oferta.ProdutosAplicados)
	}
	// Quem escolheu lê quanto lhe valem; quem não escolheu lê quanto lhe custa
	// não os ter. Nenhum dos dois é silêncio.
	if !algumaNotaContem(oferta, "já desconta") || !algumaNotaContem(oferta, "1.35") {
		t.Errorf("a nota tem de dizer o desconto e o preço sem ele: %v", oferta.Notas())
	}
}

// O outro lado da mesma escolha: sem os packs, é o preçário base que sai, e a
// nota diz o que eles valeriam. É o TestVariavelLeOsNumerosDaCaptura que fixa os
// números — aqui fixa-se que a segunda coluna não desaparece do texto.
func TestSemOsPacksANotaDizOQueEsseDescontoValeria(t *testing.T) {
	banco, _ := montar(t, cenario{calculo: "variavel_ltv80"})

	oferta, err := banco.Simular(t.Context(), pedidoBase())
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	verTaxa(t, "spread", oferta.Spread, "1.35")
	if len(oferta.ProdutosAplicados) != 0 {
		t.Errorf("não se escolheu produto nenhum; a oferta diz %v", oferta.ProdutosAplicados)
	}
	if !algumaNotaContem(oferta, "desceria") || !algumaNotaContem(oferta, "0.65") {
		t.Errorf("a nota tem de dizer para quanto desceria: %v", oferta.Notas())
	}
}

// A mista traz as duas fases, e é a única que traz. A validação do domínio
// exige que fechem exactamente no prazo — 60 + 300 = 360 meses.
func TestMistaTrazAsDuasFases(t *testing.T) {
	banco, _ := montar(t, cenario{calculo: "mista_5a"})

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = anos(5)

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if len(oferta.Fases) != 2 {
		t.Fatalf("esperava duas fases, vieram %d", len(oferta.Fases))
	}
	if oferta.Fases[0].AteMes != 60 {
		t.Errorf("a fase fixa acaba ao mês 60, veio %d", oferta.Fases[0].AteMes)
	}
	verTaxaValor(t, "TAN da fase fixa", oferta.Fases[0].Taxa, "4.35")
	verDinheiroValor(t, "prestação da fase fixa", oferta.Fases[0].Prestacao, "995.62")

	if oferta.Fases[1].AteMes != 360 {
		t.Errorf("a fase indexada acaba ao mês 360, veio %d", oferta.Fases[1].AteMes)
	}
	verTaxaValor(t, "TAN da fase indexada", oferta.Fases[1].Taxa, "3.946")
	verDinheiroValor(t, "prestação da fase indexada", oferta.Fases[1].Prestacao, "954.71")

	// ⚠️ A Euribor é o VariableIndexRate (2,596) e não o IndexValue, que nesta
	// mesma resposta traz 3,000 — a taxa base da fase fixa. Trocá-los dá uma
	// Euribor que não existe.
	verTaxa(t, "Euribor", oferta.EuriborValor, "2.596")
}

// A taxa fixa não tem fase indexada, e por isso não tem Euribor. Dizer "6m"
// aqui era declarar um indexante que este contrato não tem.
func TestFixaNaoTemFaseIndexadaNemEuribor(t *testing.T) {
	banco, _ := montar(t, cenario{calculo: "fixa_10a"})

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = 10

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if oferta.EuriborValor != nil {
		t.Errorf("uma taxa fixa não tem Euribor, veio %s", oferta.EuriborValor)
	}
	if oferta.Indexante != "" {
		t.Errorf("uma taxa fixa não tem indexante, veio %q", oferta.Indexante)
	}
	if len(oferta.Fases) != 1 || oferta.Fases[0].AteMes != 120 {
		t.Fatalf("esperava uma fase até ao mês 120, vieram %+v", oferta.Fases)
	}
	verTaxaValor(t, "TAN", oferta.Fases[0].Taxa, "4.85")
}

// O payload que geramos é o payload que foi capturado. É o passo 5 do
// CONTRATO-BANCO.md §3 — sem isto, o parser está certo e o pedido está errado.
func TestPayloadBateComOCapturado(t *testing.T) {
	casos := []struct {
		captura string
		pedido  dominio.Pedido
	}{
		{"variavel_ltv80", pedidoBase()},
		{"fixa_10a", comFixa(pedidoBase(), 10)},
		{"mista_5a", comMista(pedidoBase(), 5)},
		{"arrendamento_variavel", comFinalidade(pedidoBase(), dominio.FinalidadeArrendamento)},
		{"medida_jovem_ltv90", comMedidaJovem(pedidoBase())},
	}

	for _, c := range casos {
		t.Run(c.captura, func(t *testing.T) {
			limites := "limites_propria"
			switch c.captura {
			case "medida_jovem_ltv90":
				limites = "limites_medida_jovem"
			case "arrendamento_variavel":
				limites = "limites_arrendamento"
			}
			banco, falso := montar(t, cenario{calculo: c.captura, limites: limites})

			if _, err := banco.Simular(t.Context(), c.pedido); err != nil {
				t.Fatalf("Simular: %v", err)
			}

			enviado := corpoDe(t, falso, "/calculate")
			capturado := formulario(t, captura(t, c.captura+".pedido.txt"))
			if d := cmp.Diff(capturado, enviado); d != "" {
				t.Errorf("o formulário enviado não é o capturado (-capturado +enviado):\n%s", d)
			}
		})
	}
}

// ⚠️ Na CGD a taxa fixa é ao prazo todo: quem manda no prazo é o código do
// período, e o campo Years é decorativo. Um pedido de fixa a 5 anos num prazo
// de 10 simula-se aos 10 — e diz-se.
//
// Medido a 2026-07-26: com o código de 30 anos e Years=10, a resposta veio com
// TotalDuration 30. O v1 mandava os dois campos como independentes e reportava
// o prazo pedido; era uma prestação de 10 anos rotulada de 30.
func TestFixaAoPrazoTodoAjustaOPeriodoPedido(t *testing.T) {
	banco, falso := montar(t, cenario{calculo: "fixa_10a"})

	p := comFixa(pedidoBase(), 10)
	p.PeriodoFixoAnos = anos(5)

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 {
		t.Fatalf("esperava um ajuste ao período fixo, vieram %d: %+v", len(ajustes), ajustes)
	}
	if ajustes[0].Campo() != dominio.AjustadoPeriodoFixo {
		t.Errorf("esperava um ajuste a fixed_period_years, veio %q", ajustes[0].Campo())
	}
	if ajustes[0].De() != 5 || ajustes[0].Para() != 10 {
		t.Errorf("esperava 5 → 10, veio %v → %v", ajustes[0].De(), ajustes[0].Para())
	}
	if len(oferta.Notas()) == 0 {
		t.Error("um ajuste sem nota é um número diferente do pedido sem o dizer")
	}

	// E o formulário leva o código dos 10 anos, que é o do prazo.
	if codigo := corpoDe(t, falso, "/calculate").Get("IndexFixedRate"); codigo != "5" {
		t.Errorf("esperava o código de 10 anos (5), veio %q", codigo)
	}
}

// ⚠️ Uma recusa da CGD é `{"success":false}` e mais nada — sem código, sem
// mensagem. Medido a 2026-07-26 com prazo de 45 anos, montante abaixo do mínimo
// e código de período inválido: os três dão os mesmos 17 bytes. Não se inventa
// uma razão que ela não deu.
func TestRecusaSemMotivoNaoInventaRazao(t *testing.T) {
	banco, _ := montar(t, cenario{calculo: "recusa_sem_motivo"})

	_, err := banco.Simular(t.Context(), pedidoBase())

	erro := erroDeOferta(t, err)
	if erro.Codigo != dominio.ErroProdutoIndisponivel {
		t.Errorf("esperava produto_indisponivel, veio %q", erro.Codigo)
	}
	if !strings.Contains(erro.Mensagem, "não disse porquê") {
		t.Errorf("a mensagem devia assumir que a CGD não deu razão, veio %q", erro.Mensagem)
	}
}

// ⚠️ O LTV verifica-se contra o /limits, antes de calcular. Medido a
// 2026-07-26: o /calculate aceita um LTV de 96 % e devolve um preço completo,
// com todo o ar de válido, que a CGD não pratica. Uma API aceitar não é o banco
// vender.
func TestLTVAcimaDoQueACGDVendeERecusadoAntesDeCalcular(t *testing.T) {
	banco, falso := montar(t, cenario{calculo: "variavel_ltv80"})

	p := pedidoBase()
	p.Montante = dominio.DinheiroDeInteiro(240_000) // 96 % de 250 000

	_, err := banco.Simular(t.Context(), p)

	erro := erroDeOferta(t, err)
	if erro.Codigo != dominio.ErroProdutoIndisponivel {
		t.Errorf("esperava produto_indisponivel, veio %q", erro.Codigo)
	}
	// ⚠️ Em euros, e não em percentagens. Com um LTV de 80,004 % contra um
	// limite de 80 %, as duas percentagens arredondavam para o mesmo número e
	// a recusa lia-se «é de 80.0 % e a CGD vai até 80 %». Medido na corrida de
	// fidelidade de 2026-07-26, 25 vezes em 2900.
	if !strings.Contains(erro.Mensagem, "225 000") {
		t.Errorf("a mensagem devia dizer quanto a CGD financia neste imóvel, veio %q", erro.Mensagem)
	}
	if !strings.Contains(erro.Mensagem, "240 000") {
		t.Errorf("a mensagem devia dizer quanto se pediu, veio %q", erro.Mensagem)
	}
	if pedidos := caminhos(falso); contem(pedidos, "/calculate") {
		t.Errorf("o /calculate não devia ter sido chamado — ele responde a isto com um preço. Pedidos: %v", pedidos)
	}
}

// A Medida Jovem tem LTV mínimo, e não só máximo: a Garantia do Estado cobre a
// fatia dos 85 % aos 100 %. Medido — com IsMedidaJovem o /limits devolve
// ltvMinimum 0,85.
func TestMedidaJovemAbaixoDe85NaoEElegivel(t *testing.T) {
	banco, _ := montar(t, cenario{calculo: "medida_jovem_ltv90", limites: "limites_medida_jovem"})

	p := comMedidaJovem(pedidoBase())
	p.Montante = dominio.DinheiroDeInteiro(200_000) // 80 %

	_, err := banco.Simular(t.Context(), p)

	erro := erroDeOferta(t, err)
	if !strings.Contains(erro.Mensagem, "85") {
		t.Errorf("a mensagem devia nomear o mínimo de 85 %%, veio %q", erro.Mensagem)
	}
	if !strings.Contains(erro.Mensagem, "Sem a garantia") {
		t.Errorf("a mensagem devia dizer o que fazer, veio %q", erro.Mensagem)
	}
}

// O prazo encurta pela idade do titular mais velho e a oferta di-lo. A CGD não
// pergunta a idade — quem a impõe é o limite dela própria, que o /limits
// publica: o contrato tem de terminar até aos 70 anos na habitação própria.
func TestPrazoAcimaDaIdadeEncurtaEAnota(t *testing.T) {
	banco, falso := montar(t, cenario{calculo: "variavel_ltv80"})

	p := pedidoBase()
	p.PrazoAnos = 30
	p.Titulares = []dominio.Titular{{DataNascimento: nascidoHaAnos(55)}}

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 || ajustes[0].Campo() != dominio.AjustadoPrazoAnos {
		t.Fatalf("esperava um ajuste ao prazo, vieram %+v", ajustes)
	}
	// 70 − 55 = 15.
	if ajustes[0].Para() != 15 {
		t.Errorf("com 55 anos e o fim aos 70, o prazo é 15; veio %v", ajustes[0].Para())
	}
	if !strings.Contains(oferta.Notas()[0], "70") {
		t.Errorf("a nota devia nomear a idade limite, veio %q", oferta.Notas()[0])
	}
	if enviado := corpoDe(t, falso, "/calculate").Get("Years"); enviado != "15" {
		t.Errorf("o formulário devia levar o prazo ajustado (15), levou %q", enviado)
	}
}

// ⚠️ Quando o widget dos períodos desaparece do HTML, recorre-se à lista
// conhecida **e avisa-se**. O v1 caía no recurso em silêncio: se a CGD mudasse
// o widget, o simulador continuava a responder com os períodos do ano passado e
// ninguém sabia.
func TestSemOWidgetRecorreMasAvisa(t *testing.T) {
	banco, _ := montar(t, cenario{calculo: "mista_5a", home: "<html>a página mudou</html>"})

	oferta, err := banco.Simular(t.Context(), comMista(pedidoBase(), 5))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	notas := strings.Join(oferta.Notas(), "\n")
	if !strings.Contains(notas, "Não se conseguiu ler") {
		t.Errorf("a oferta devia avisar que os períodos não se leram, notas: %q", notas)
	}
}

// Os Requisitos são a única fonte do formulário adaptativo, e dizem também o
// que a CGD **não** pergunta.
func TestRequisitosSaoValidosEDizemOQueACGDIgnora(t *testing.T) {
	banco, _ := montar(t, cenario{calculo: "variavel_ltv80"})
	r := banco.Requisitos()

	if err := r.Validar(); err != nil {
		t.Fatalf("Requisitos inválidos: %v", err)
	}
	if r.Custo != dominio.CustoBarato {
		t.Errorf("a CGD é HTTP puro: esperava barato, veio %q", r.Custo)
	}

	porCampo := map[dominio.CampoCanonico]dominio.Input{}
	for _, in := range r.Inputs {
		porCampo[in.Campo] = in
	}
	// ⚠️ "O simulador não pergunta" é diferente de "a API ignora", e só o
	// primeiro dispensa a nota de pressuposto. A CGD é do primeiro caso: não há
	// campo nenhum preenchido por nós.
	for _, campo := range []dominio.CampoCanonico{
		dominio.CampoDataNascimento, dominio.CampoRendimentoMensal, dominio.CampoProfissao,
	} {
		in, declarado := porCampo[campo]
		if !declarado {
			t.Errorf("%s não está declarado — a app não sabe que a CGD o ignora", campo)
			continue
		}
		if in.Usa {
			t.Errorf("%s: a CGD não o pergunta, devia estar a Usa: false", campo)
		}
		if in.Nota == "" {
			t.Errorf("%s: um campo que o banco ignora tem de dizer que o ignora", campo)
		}
	}

	// EuriborOpcoes vazio não é "não sei": é "o banco impõe o seu".
	if len(r.EuriborOpcoes) != 0 {
		t.Errorf("a CGD não deixa escolher o indexante, vieram opções: %v", r.EuriborOpcoes)
	}
	if r.EuriborImposto != dominio.Euribor6M {
		t.Errorf("esperava a Euribor a 6 meses imposta, veio %q", r.EuriborImposto)
	}
}

// A afirmação que cada banco repete: Simular volta dentro do prazo do ctx.
// Contra um transporte que demora muito mais do que o prazo — sem isso, o teste
// passava por não haver nada que demorasse.
func TestRespeitaOPrazoDoCtx(t *testing.T) {
	lento := &transporte.Falso{
		Atraso: 5 * time.Second,
		Corpo:  string(captura(t, "variavel_ltv80.resposta.json")),
	}
	banco := cgd.Novo(lento, nil)

	prova.RespeitaPrazo(t, banco, pedidoBase(), 300*time.Millisecond)
}

// --- ajudantes ---------------------------------------------------------------

type cenario struct {
	// calculo é o nome da captura que o /calculate devolve.
	calculo string
	// limites é o nome da captura do /limits; vazio vale a da habitação própria.
	limites string
	// home é o HTML a devolver na página; vazio vale a captura real.
	home string
}

// montar liga a CGD a um transporte falso que serve as capturas, encaminhando
// por caminho — é o simulador inteiro, offline.
func montar(t *testing.T, c cenario) (bancos.Banco, *transporte.Falso) {
	t.Helper()

	if c.limites == "" {
		c.limites = "limites_propria"
	}
	home := c.home
	if home == "" {
		home = string(captura(t, "home.html"))
	}
	limites := string(captura(t, c.limites+".resposta.json"))
	calculo := string(captura(t, c.calculo+".resposta.json"))

	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			switch p.URL.Path {
			case "/":
				return transporte.RespostaDeTexto(http.StatusOK, home), nil
			case "/limits":
				return transporte.RespostaDeTexto(http.StatusOK, limites), nil
			case "/calculate":
				return transporte.RespostaDeTexto(http.StatusOK, calculo), nil
			default:
				t.Errorf("pedido a um caminho que a CGD não tem: %q", p.URL.Path)
				return transporte.RespostaDeTexto(http.StatusNotFound, ""), nil
			}
		},
	}
	return cgd.Novo(falso, nil), falso
}

func captura(t *testing.T, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("capturas", nome))
	if err != nil {
		t.Fatalf("ler a captura %s: %v", nome, err)
	}
	return b
}

// formulario lê um corpo form-urlencoded para comparação. Compara-se o
// conteúdo e não o texto: a ordem dos campos não é do contrato.
func formulario(t *testing.T, corpo []byte) url.Values {
	t.Helper()
	v, err := url.ParseQuery(strings.TrimSpace(string(corpo)))
	if err != nil {
		t.Fatalf("ler o formulário: %v", err)
	}
	return v
}

func corpoDe(t *testing.T, f *transporte.Falso, caminho string) url.Values {
	t.Helper()
	for _, p := range f.Pedidos() {
		if p.URL.Path == caminho {
			return formulario(t, p.Corpo)
		}
	}
	t.Fatalf("não houve pedido nenhum a %s", caminho)
	return nil
}

func caminhos(f *transporte.Falso) []string {
	var cs []string
	for _, p := range f.Pedidos() {
		cs = append(cs, p.URL.Path)
	}
	return cs
}

func algumaNotaContem(o dominio.Oferta, texto string) bool {
	for _, n := range o.Notas() {
		if strings.Contains(n, texto) {
			return true
		}
	}
	return false
}

func contem(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func erroDeOferta(t *testing.T, err error) *dominio.ErroOferta {
	t.Helper()
	if err == nil {
		t.Fatal("esperava uma falha e não veio nenhuma")
	}
	var e *dominio.ErroOferta
	if !errors.As(err, &e) {
		t.Fatalf("esperava um *dominio.ErroOferta, veio %T: %v", err, err)
	}
	return e
}

// pedidoBase é o cenário das capturas: 250 000 € de imóvel, 200 000 €
// financiados (LTV 80 %), 30 anos, habitação própria. O titular é fictício e a
// CGD não pergunta por ele.
func pedidoBase() dominio.Pedido {
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dominio.DinheiroDeInteiro(200_000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
		Titulares:   []dominio.Titular{{DataNascimento: nascidoHaAnos(35)}},
	}
}

func comFixa(p dominio.Pedido, prazo int) dominio.Pedido {
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = prazo
	return p
}

func comMista(p dominio.Pedido, periodo int) dominio.Pedido {
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = anos(periodo)
	return p
}

func comFinalidade(p dominio.Pedido, f dominio.Finalidade) dominio.Pedido {
	p.Finalidade = f
	return p
}

// comPacks escolhe os packs de vinculação da CGD.
func comPacks(p dominio.Pedido) dominio.Pedido {
	p.Produtos = append(p.Produtos, cgd.ProdutoPacks)
	return p
}

func comMedidaJovem(p dominio.Pedido) dominio.Pedido {
	p.GarantiaPublica = true
	p.Montante = dominio.DinheiroDeInteiro(225_000) // 90 %
	return p
}

func anos(n int) *int { return &n }

// nascidoHaAnos dá uma data de nascimento que faz a pessoa ter exactamente esta
// idade hoje — é assim que se testa a política de idade sem congelar o relógio.
func nascidoHaAnos(n int) dominio.Data {
	return dominio.DataDeInstante(time.Now().AddDate(-n, 0, 0))
}

func verTaxa(t *testing.T, nome string, veio *dominio.Taxa, esperado string) {
	t.Helper()
	if veio == nil {
		t.Errorf("%s: não veio", nome)
		return
	}
	verTaxaValor(t, nome, *veio, esperado)
}

func verTaxaValor(t *testing.T, nome string, veio dominio.Taxa, esperado string) {
	t.Helper()
	quero, err := dominio.TaxaDeTexto(esperado)
	if err != nil {
		t.Fatalf("esperado inválido %q: %v", esperado, err)
	}
	if !veio.Equal(quero) {
		t.Errorf("%s: esperava %s, veio %s", nome, esperado, veio)
	}
}

func verDinheiro(t *testing.T, nome string, veio *dominio.Dinheiro, esperado string) {
	t.Helper()
	if veio == nil {
		t.Errorf("%s: não veio", nome)
		return
	}
	verDinheiroValor(t, nome, *veio, esperado)
}

func verDinheiroValor(t *testing.T, nome string, veio dominio.Dinheiro, esperado string) {
	t.Helper()
	quero, err := dominio.DinheiroDeTexto(esperado)
	if err != nil {
		t.Fatalf("esperado inválido %q: %v", esperado, err)
	}
	if !veio.Equal(quero) {
		t.Errorf("%s: esperava %s, veio %s", nome, esperado, veio)
	}
}

// ⚠️ Reprodução do que a corrida de fidelidade de 2026-07-26 apanhou, 5 vezes
// em 2889: com o HTML ilegível, a fixa cai na lista de recurso e um prazo de 32
// anos vira 30. Isso é aceitável — o que não era aceitável era a justificação.
//
// A oferta saía com a nota «este banco não os aceita (a taxa fixa da CGD só
// existe entre 5 e 40 anos)», e 32 está entre 5 e 40: a CGD vende-os. O que
// faltou foi a lista, do nosso lado. E ao vivo, quando a idade já tinha
// encolhido o prazo antes, saíam dois ajustes em cadeia e o segundo dizia
// «Pediu 32 anos» a quem tinha pedido 35.
func TestFixaComRecursoDizAVerdadeSobrePorqueEncolheuOPrazo(t *testing.T) {
	banco, falso := montar(t, cenario{calculo: "fixa_10a", home: "<html>sem widget</html>"})

	oferta, err := banco.Simular(t.Context(), comFixa(pedidoBase(), 32))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoDe(t, falso, "/calculate").Get("Years"); enviado != "30" {
		t.Errorf("com a lista de recurso, 32 anos encaixam em 30; enviou-se %q", enviado)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 {
		t.Fatalf("esperava um ajuste ao prazo e vieram %d: %+v", len(ajustes), ajustes)
	}
	if ajustes[0].De() != 32 || ajustes[0].Para() != 30 {
		t.Errorf("esperava 32 → 30, veio %v → %v", ajustes[0].De(), ajustes[0].Para())
	}
	if strings.Contains(ajustes[0].Nota(), "entre 5 e 40") {
		t.Errorf("32 anos está entre 5 e 40 e a CGD vende-os: a razão dada é falsa — %q", ajustes[0].Nota())
	}
	if !strings.Contains(ajustes[0].Nota(), "não se conseguiu ler") {
		t.Errorf("a razão verdadeira é não termos lido a lista, e a nota devia dizê-lo — %q", ajustes[0].Nota())
	}
}

// Com a lista verdadeira, um prazo que a CGD pratica não se mexe — e é isso que
// mostra que o teste acima está a medir a ignorância, e não o banco.
func TestFixaComAListaVerdadeiraNaoMexeNoPrazo(t *testing.T) {
	banco, falso := montar(t, cenario{calculo: "fixa_10a"})

	oferta, err := banco.Simular(t.Context(), comFixa(pedidoBase(), 32))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}
	if enviado := corpoDe(t, falso, "/calculate").Get("Years"); enviado != "32" {
		t.Errorf("a CGD pratica 32 anos de taxa fixa; enviou-se %q", enviado)
	}
	if len(oferta.Ajustes()) != 0 {
		t.Errorf("não havia nada a ajustar, vieram %+v", oferta.Ajustes())
	}
}
