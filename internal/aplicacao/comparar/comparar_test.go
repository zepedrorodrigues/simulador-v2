package comparar_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/comparar"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A resposta local, contra um catálogo com a forma que o varrimento produz.
//
// ⚠️ **Zero I/O.** Não há base, não há rede e não há relógio a mandar no
// resultado — é o que a §3 quer dizer com «o comparar não fala com bancos», e é
// o que torna estas afirmações verificáveis sem nada montado.
//
// ⚠️ E o catálogo de prova tem, de propósito, **várias observações com a mesma
// chave de cenário**: as das famílias dos produtos e a do prazo partilham
// `variavel/0/propria`. Medido a 2026-07-28 sobre os bancos registados, essa
// chave aparece 4 a 9 vezes por banco. Um catálogo de teste com uma observação
// por chave não exercitaria o caso que a leitura tem de resolver.

const (
	bancoID   = "provabank"
	bancoNome = "Banco de Prova"

	// O segundo é varrido; o terceiro está no registo e nunca foi varrido. É a
	// assimetria que a KAN-45 mede, e a razão de os ids serem alfabéticos por
	// esta ordem — a resposta sai ordenada e os testes contam com isso.
	segundoID    = "qprovabank"
	segundoNome  = "Segundo Banco de Prova"
	terceiroID   = "rprovabank"
	terceiroNome = "Terceiro Banco de Prova"

	produtoOrdenado = "provabank:ordenado"
	produtoSeguros  = "provabank:seguros"
)

// Os encargos que o banco de prova pratica, e que o ajuste tem de reencontrar a
// partir das TAEG observadas: 0,5 % do capital à cabeça e 0,25 p.p. ao ano.
var encargosVerdadeiros = dominio.Encargos{
	Antecipado: racio(nil, "0.005"),
	Recorrente: taxa(nil, "0.25"),
}

func TestUmaOfertaSaiDaEscalaEDaEuriborMedidas(t *testing.T) {
	cat := catalogoDeProva(t)

	// LTV 80 % cai no segundo degrau, de spread 1,350. A Euribor medida é 2,450.
	ofertas, err := cat.Comparar(pedido(t, "320000", 30, dominio.TaxaVariavel), nil, requisitos(), hoje())
	if err != nil {
		t.Fatalf("Comparar: %v", err)
	}
	if len(ofertas) != 1 {
		t.Fatalf("esperava uma oferta, vieram %d", len(ofertas))
	}
	o := ofertas[0]
	if !o.Sucesso() {
		t.Fatalf("a oferta falhou: %s", o.Erro.Mensagem)
	}

	// TAN = Euribor + spread = 2,450 + 1,350.
	verTaxa(t, "TAN", o.TAN, "3.8")
	verTaxa(t, "spread", o.Spread, "1.35")
	verTaxa(t, "Euribor", o.EuriborValor, "2.45")

	// 320 000 € a 3,8 % em 360 meses, pela francesa do domínio.
	esperada, err := dominio.PrestacaoFrancesa(dinheiro(t, "320000"), taxa(t, "3.8"), 360)
	if err != nil {
		t.Fatalf("prestação de referência: %v", err)
	}
	if o.Prestacao == nil || !o.Prestacao.Equal(esperada) {
		t.Errorf("prestação %v, e a francesa sobre 320 000 € a 3,8 %% dá %s", o.Prestacao, esperada)
	}

	// ⚠️ A data é a do VARRIMENTO, e não a de agora: sem ela, um preço de ontem
	// à noite apresenta-se como cotado neste instante.
	if o.CapturadoEm.IsZero() {
		t.Error("a oferta saiu sem a hora a que o preço foi medido")
	}
}

// TestOSpreadVemDoDegrauCerto é a reversão que a KAN-31 pede: mudar o degrau tem
// de falhar a nomear o spread, e não só a prestação.
func TestOSpreadVemDoDegrauCerto(t *testing.T) {
	cat := catalogoDeProva(t)

	// LTV 60 % (primeiro degrau, spread 2,000) contra 80 % (segundo, 1,350).
	baixo := ofertaUnica(t, cat, pedido(t, "240000", 30, dominio.TaxaVariavel))
	alto := ofertaUnica(t, cat, pedido(t, "320000", 30, dominio.TaxaVariavel))

	verTaxa(t, "spread a 60 % de LTV", baixo.Spread, "2")
	verTaxa(t, "spread a 80 % de LTV", alto.Spread, "1.35")

	// ⚠️ Um LTV que caia exactamente na fronteira vai para o degrau de baixo: os
	// intervalos são (De, Ate], e está medido no Novo Banco que 50,00 % ainda
	// leva o preço de baixo e 50,25 % já leva o de cima.
	fronteira := ofertaUnica(t, cat, pedido(t, "280000", 30, dominio.TaxaVariavel)) // LTV 0,70
	verTaxa(t, "spread na fronteira", fronteira.Spread, "2")
}

// TestUmProdutoEscolhidoDescontaOQueFoiMedido: o desconto deriva-se da diferença
// entre a linha com o produto e a linha sem nenhum, e é somado ao spread.
func TestUmProdutoEscolhidoDescontaOQueFoiMedido(t *testing.T) {
	cat := catalogoDeProva(t)

	sem := ofertaUnica(t, cat, pedido(t, "320000", 30, dominio.TaxaVariavel))

	comOrdenado := pedido(t, "320000", 30, dominio.TaxaVariavel)
	comOrdenado.Produtos = []string{produtoOrdenado}
	com := ofertaUnica(t, cat, comOrdenado)

	// A linha com o ordenado mede 0,850 contra 1,350 sem produtos: desconta 0,50.
	verTaxa(t, "spread sem produtos", sem.Spread, "1.35")
	verTaxa(t, "spread com o ordenado", com.Spread, "0.85")

	// E os dois produtos somam-se: -0,50 e -0,20.
	comDois := pedido(t, "320000", 30, dominio.TaxaVariavel)
	comDois.Produtos = []string{produtoOrdenado, produtoSeguros}
	verTaxa(t, "spread com os dois", ofertaUnica(t, cat, comDois).Spread, "0.65")
}

// TestSemProdutosNaoSeServeOPrecoBonificado é o defeito que a KAN-33 mediu em
// 0,45 p.p., e que a colisão de chaves torna fácil de reintroduzir.
//
// ⚠️ O catálogo tem, no mesmo cenário, a linha sem produtos E as bonificadas. Um
// índice que ficasse com «a última» respondia com a bonificada a quem não pediu
// nada — e o número sairia plausível, mais barato, e errado.
func TestSemProdutosNaoSeServeOPrecoBonificado(t *testing.T) {
	cat := catalogoDeProva(t)

	variavel := ofertaUnica(t, cat, pedido(t, "320000", 30, dominio.TaxaVariavel))
	verTaxa(t, "spread da variável", variavel.Spread, "1.35")
	if len(variavel.ProdutosAplicados) != 0 {
		t.Errorf("um pedido sem produtos trouxe %v aplicados", variavel.ProdutosAplicados)
	}

	// ⚠️ E a MISTA, que é onde a escolha da linha decide o preço: a TAN da fase
	// fixa lê-se da observação. Sem produtos tem de sair 3,100 — a de tabela — e
	// não os 2,600 da linha bonificada que está no mesmo cenário.
	mista := ofertaUnica(t, cat, pedido(t, "320000", 30, dominio.TaxaMista))
	if len(mista.Fases) == 0 {
		t.Fatal("a mista saiu sem fases")
	}
	if got := mista.Fases[0].Taxa.String(); got != "3.1" {
		t.Errorf("a TAN da fase fixa é %s, e a de tabela é 3.1 — foi servido o preço bonificado", got)
	}
}

// TestUmProdutoSemDescontoMedidoNaoDescontaEDizSe: aplicar zero em silêncio dava
// um preço mais caro do que o real; inventar um desconto dava um mais barato.
func TestUmProdutoSemDescontoMedidoNaoDescontaEDizSe(t *testing.T) {
	cat := catalogoDeProva(t)

	p := pedido(t, "320000", 30, dominio.TaxaVariavel)
	p.Produtos = []string{"provabank:nunca_medido"}
	o := ofertaUnica(t, cat, p)

	verTaxa(t, "spread", o.Spread, "1.35")
	if !contem(o.Notas(), "não mediu quanto ele desconta") {
		t.Errorf("o produto por medir passou em silêncio: %v", o.Notas())
	}
}

// TestATAEGDerivaDosEncargosAjustadosEDeclaraOsPressupostos é a condição em que
// a §4 passou a permitir servir a TAEG (2026-07-28): com as hipóteses ao lado.
func TestATAEGDerivaDosEncargosAjustadosEDeclaraOsPressupostos(t *testing.T) {
	cat := catalogoDeProva(t)
	o := ofertaUnica(t, cat, pedido(t, "320000", 30, dominio.TaxaVariavel))

	if o.TAEG == nil || o.MTIC == nil {
		t.Fatalf("a oferta saiu sem TAEG (%v) ou sem MTIC (%v)", o.TAEG, o.MTIC)
	}
	// A TAEG tem de exceder a TAN — são os encargos — e não por um disparate.
	if o.TAEG.Cmp(*o.TAN) <= 0 {
		t.Errorf("TAEG %s não excede a TAN %s", o.TAEG, o.TAN)
	}
	if o.TAEG.Decimal().Sub(o.TAN.Decimal()).GreaterThan(dinheiro(t, "1").Decimal()) {
		t.Errorf("TAEG %s excede a TAN %s em mais de 1 p.p., o que não é encargo é defeito", o.TAEG, o.TAN)
	}

	// ⚠️ Nunca um sem os outros: a TAEG preenchida obriga aos pressupostos.
	pressupostos := o.Pressupostos()
	if len(pressupostos) == 0 {
		t.Fatal("a TAEG saiu sem pressupostos — é a condição em que a §4 a permite")
	}
	if !contem(pressupostos, "não são valores que o banco tenha devolvido") {
		t.Errorf("os pressupostos não dizem que o número não veio do banco: %v", pressupostos)
	}
	if !contem(pressupostos, "ficha de informação normalizada") {
		t.Errorf("os pressupostos não remetem para a FINE: %v", pressupostos)
	}
	t.Logf("TAN %s → TAEG %s, MTIC %s", o.TAN, o.TAEG, o.MTIC)
}

// TestSemDoisPrazosNaoHaTAEG: com uma observação só, qualquer repartição dos
// encargos dá a mesma TAEG. Escolher uma seria assunção nossa disfarçada de
// medição — e é isso que o AjustarEncargos recusa.
func TestSemDoisPrazosNaoHaTAEG(t *testing.T) {
	// O mesmo catálogo, sem as observações da família do prazo.
	cat, err := comparar.NovoCatalogo(semFamiliaDoPrazo(observacoesDeProva(t)))
	if err != nil {
		t.Fatalf("NovoCatalogo: %v", err)
	}

	o := ofertaUnica(t, cat, pedido(t, "320000", 30, dominio.TaxaVariavel))

	if o.TAEG != nil || o.MTIC != nil {
		t.Errorf("derivou-se TAEG (%v) e MTIC (%v) de um só prazo", o.TAEG, o.MTIC)
	}
	if !contem(o.Notas(), "prazos suficientemente diferentes") {
		t.Errorf("a ausência da TAEG passou sem explicação: %v", o.Notas())
	}
	// ⚠️ E a oferta continua a sair: o preço está medido, só a TAEG é que não se
	// deriva. Recusar a oferta inteira seria deitar fora o que se sabe.
	if !o.Sucesso() || o.TAN == nil {
		t.Error("a oferta devia sair na mesma, com o preço que está medido")
	}
}

// TestUmCenarioQueOVarrimentoNaoMediuERecusadoENaoInterpolado é a decisão que a
// KAN-31 deixou em aberto.
func TestUmCenarioQueOVarrimentoNaoMediuERecusadoENaoInterpolado(t *testing.T) {
	cat := catalogoDeProva(t)

	// ⚠️ Não serve pedir um PERÍODO que o banco não pratica: esse encaixa-se no
	// mais próximo, com ajuste, e a resposta sai. O que não tem encaixe é uma
	// dimensão que a grelha não varreu — aqui, a finalidade: o banco de prova só
	// foi medido em habitação própria.
	p := pedido(t, "320000", 30, dominio.TaxaVariavel)
	p.Finalidade = dominio.FinalidadeArrendamento

	o := ofertaUnica(t, cat, p)
	if o.Sucesso() {
		t.Fatalf("respondeu-se a um cenário que não foi medido: TAN %v", o.TAN)
	}
	if !strings.Contains(o.Erro.Mensagem, "não se serve o preço de outro cenário") {
		t.Errorf("a recusa não diz porquê: %q", o.Erro.Mensagem)
	}
}

func TestUmLTVForaDaEscalaERecusado(t *testing.T) {
	cat := catalogoDeProva(t)

	// A escala do banco de prova vai de 30 % a 90 %.
	o := ofertaUnica(t, cat, pedido(t, "380000", 30, dominio.TaxaVariavel)) // LTV 0,95
	if o.Sucesso() {
		t.Fatal("respondeu-se a um LTV que o banco não preça")
	}
	if !strings.Contains(o.Erro.Mensagem, "não financia neste rácio") {
		t.Errorf("a recusa não nomeia o rácio: %q", o.Erro.Mensagem)
	}
}

// TestAFixaSaiDaBaseMaisOSpreadDoCliente: a TAN da taxa fixa muda com o LTV
// (cartesiano de 2026-07-28), por isso o que se consulta é a base.
func TestAFixaSaiDaBaseMaisOSpreadDoCliente(t *testing.T) {
	cat := catalogoDeProva(t)

	// A fixa a 10 anos foi medida a 80 % de LTV com TAN 5,550 → base 4,200.
	// A 60 % o spread é 2,000, logo a TAN tem de ser 6,200.
	oitenta := ofertaUnica(t, cat, comFixa(pedido(t, "320000", 10, dominio.TaxaFixa), 10))
	sessenta := ofertaUnica(t, cat, comFixa(pedido(t, "240000", 10, dominio.TaxaFixa), 10))

	verTaxa(t, "TAN da fixa a 80 % de LTV", oitenta.TAN, "5.55")
	verTaxa(t, "TAN da fixa a 60 % de LTV", sessenta.TAN, "6.2")

	// ⚠️ Numa fixa pura não há fase indexada: publicar spread ou Euribor era
	// inventar. O spread entrou no cálculo da TAN, que é outra coisa.
	if sessenta.Spread != nil || sessenta.EuriborValor != nil {
		t.Errorf("a fixa pura publicou spread %v e Euribor %v", sessenta.Spread, sessenta.EuriborValor)
	}
}

func TestUmPeriodoQueOBancoNaoPraticaSaiComAjusteENota(t *testing.T) {
	cat := catalogoDeProva(t)

	// O banco pratica 5 e 10; pede-se 12, que encaixa em 10 — e o catálogo tem
	// fixa/10/propria, portanto a resposta sai com o ajuste declarado.
	p := comFixa(pedido(t, "320000", 10, dominio.TaxaFixa), 12)
	o := ofertaUnica(t, cat, p)

	ajustes := o.Ajustes()
	if len(ajustes) == 0 {
		t.Fatal("pediu-se um período que o banco não pratica e não houve ajuste")
	}
	var doPeriodo *dominio.Ajuste
	for i := range ajustes {
		if ajustes[i].Campo() == dominio.AjustadoPeriodoFixo {
			doPeriodo = &ajustes[i]
		}
	}
	if doPeriodo == nil {
		t.Fatalf("nenhum ajuste é ao período fixo: %+v", ajustes)
	}
	// ⚠️ O dominio.Ajuste já não deixa sair um ajuste mudo, e é isso que se
	// confirma: a nota nomeia os dois números.
	if !strings.Contains(doPeriodo.Nota(), "12") {
		t.Errorf("a nota não diz o que se pediu: %q", doPeriodo.Nota())
	}
}

func TestUmPedidoInvalidoNaoChegaAOlharParaBancoNenhum(t *testing.T) {
	cat := catalogoDeProva(t)

	p := pedido(t, "320000", 30, dominio.TaxaVariavel)
	p.Montante = dinheiro(t, "500000") // acima do valor do imóvel

	if _, err := cat.Comparar(p, nil, requisitos(), hoje()); err == nil {
		t.Fatal("um pedido inválido passou")
	}
}

func TestSemObservacoesNaoHaCatalogo(t *testing.T) {
	if _, err := comparar.NovoCatalogo(nil); err == nil {
		t.Fatal("construiu-se um catálogo sem observações")
	}
}

// TestUmBancoPedidoSemSerieVemComoRecusaENaoDesaparece é a KAN-45.
//
// ⚠️ **O catálogo tem dois bancos e o pedido nomeia três**, e é essa diferença
// que faz o teste. Todos os testes deste ficheiro construíam o catálogo e o
// pedido com o mesmo conjunto, portanto os dois coincidiam sempre e o defeito
// não tinha por onde aparecer — o que é o caso normal em produção depois de o
// varrimento estabilizar, e por isso é que isto só se via numa base nova, num
// banco novo, ou num banco cujo varrimento falhou.
//
// A reversão: pôr o `Comparar` a percorrer `c.Bancos()` outra vez. O `terceiro`
// deixa de vir, e a falha nomeia-o.
func TestUmBancoPedidoSemSerieVemComoRecusaENaoDesaparece(t *testing.T) {
	cat := catalogoDeDoisBancos(t)

	pedidos := []string{bancoID, segundoID, terceiroID}
	ofertas, err := cat.Comparar(pedido(t, "320000", 30, dominio.TaxaVariavel), pedidos, requisitosDeTres(), hoje())
	if err != nil {
		t.Fatalf("Comparar: %v", err)
	}

	// ⚠️ A falha nomeia quem falta, e não só a contagem. «esperava 3, vieram 2»
	// manda quem lê procurar qual dos três — que é a pergunta a que o teste devia
	// responder sozinho.
	vieram := map[string]dominio.Oferta{}
	for _, o := range ofertas {
		vieram[o.BancoID] = o
	}
	for _, id := range pedidos {
		if _, veio := vieram[id]; !veio {
			t.Errorf("pediu-se o %q e ele não vem na resposta, nem sequer como recusa", id)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	if len(ofertas) != 3 {
		t.Fatalf("pediram-se 3 bancos e vieram %d ofertas", len(ofertas))
	}

	// Os dois medidos respondem; o terceiro recusa, e diz porquê.
	for _, id := range []string{bancoID, segundoID} {
		if !vieram[id].Sucesso() {
			t.Errorf("o %q foi varrido e mesmo assim não deu oferta: %s", id, vieram[id].Erro.Mensagem)
		}
	}

	semSerie := vieram[terceiroID]
	if semSerie.Sucesso() {
		t.Fatalf("o %q não tem observações nenhumas e ainda assim deu oferta", terceiroID)
	}
	if semSerie.Erro.Codigo != dominio.ErroSemSerie {
		t.Errorf("o banco sem série veio como %q, e a KAN-45 pede %q",
			semSerie.Erro.Codigo, dominio.ErroSemSerie)
	}
	// A mensagem nomeia o banco por que a pessoa perguntou, e não o id.
	if !strings.Contains(semSerie.Erro.Mensagem, terceiroNome) {
		t.Errorf("a recusa do banco sem série não o nomeia: %q", semSerie.Erro.Mensagem)
	}
}

// TestNaoSeConfundeNaoTerSidoVarridoComNaoTerEsteCenario afirma a distinção que
// a KAN-45 exige: as duas recusas não podem ser a mesma frase nem o mesmo
// código.
//
// ⚠️ São decisões diferentes para quem lê. «Ainda não varremos este banco»
// resolve-se correndo o varrimento; «varreu-se e não mede mista a 5 anos»
// resolve-se mudando o pedido, ou não se resolve. Empacotar as duas na mesma
// frase mandava a pessoa esperar por uma coisa que não vai acontecer, ou mudar
// um pedido que estava bem.
func TestNaoSeConfundeNaoTerSidoVarridoComNaoTerEsteCenario(t *testing.T) {
	// O segundo banco só tem a variável: pedir-lhe uma fixa é «varrido, sem este
	// cenário». O terceiro não tem observação nenhuma: é «sem série».
	cat := catalogoDeDoisBancos(t, soVariavel)

	ofertas, err := cat.Comparar(
		pedido(t, "320000", 10, dominio.TaxaFixa),
		[]string{segundoID, terceiroID}, requisitosDeTres(), hoje())
	if err != nil {
		t.Fatalf("Comparar: %v", err)
	}
	// ⚠️ Procuram-se pelo id e não pela posição: se a correcção se perder, o que
	// se quer ler na falha é «o banco tal não veio», e não um desencontro de
	// índices que manda quem lê contar ofertas à mão.
	semCenario, temSegundo := ofertaDe(ofertas, segundoID)
	semSerie, temTerceiro := ofertaDe(ofertas, terceiroID)
	if !temSegundo {
		t.Fatalf("pediu-se o %q, que foi varrido sem este cenário, e ele não vem", segundoID)
	}
	if !temTerceiro {
		t.Fatalf("pediu-se o %q, que nunca foi varrido, e ele não vem — nem sequer como recusa", terceiroID)
	}
	if semCenario.Sucesso() || semSerie.Sucesso() {
		t.Fatal("esperava as duas recusadas")
	}

	if semCenario.Erro.Codigo == semSerie.Erro.Codigo {
		t.Errorf("as duas recusas têm o mesmo código %q, e são coisas diferentes", semCenario.Erro.Codigo)
	}
	if semCenario.Erro.Mensagem == semSerie.Erro.Mensagem {
		t.Errorf("as duas recusas dizem a mesma frase: %q", semSerie.Erro.Mensagem)
	}
	if semCenario.Erro.Codigo != dominio.ErroProdutoIndisponivel {
		t.Errorf("«varrido e sem este cenário» veio como %q, esperava %q",
			semCenario.Erro.Codigo, dominio.ErroProdutoIndisponivel)
	}
	if semSerie.Erro.Codigo != dominio.ErroSemSerie {
		t.Errorf("«nunca varrido» veio como %q, esperava %q", semSerie.Erro.Codigo, dominio.ErroSemSerie)
	}
}

// TestUmIdQueNaoEBancoNenhumERecusadoNoPedido: «ainda não temos preços deste
// banco» e «não há tal banco» são coisas diferentes, e a segunda é erro de quem
// pergunta. Dar-lhe uma linha de recusa ensinava o cliente que um id que
// escreveu mal é um banco que existe.
func TestUmIdQueNaoEBancoNenhumERecusadoNoPedido(t *testing.T) {
	cat := catalogoDeDoisBancos(t)

	_, err := cat.Comparar(
		pedido(t, "320000", 30, dominio.TaxaVariavel),
		[]string{bancoID, "banco-que-nunca-existiu"}, requisitosDeTres(), hoje())
	if err == nil {
		t.Fatal("um id que não é banco nenhum passou como se fosse")
	}

	var validacao *dominio.ErroValidacao
	if !errors.As(err, &validacao) {
		t.Fatalf("a recusa não nomeia o campo do pedido: %v", err)
	}
	if validacao.Campo != "bancos" {
		t.Errorf("a recusa aponta ao campo %q, esperava %q", validacao.Campo, "bancos")
	}
	if !strings.Contains(validacao.Mensagem, "banco-que-nunca-existiu") {
		t.Errorf("a recusa não diz qual o id que não existe: %q", validacao.Mensagem)
	}
}

// TestSemBancosNoPedidoRespondeSePelosDoRegisto: lista vazia quer dizer todos, e
// «todos» é o registo — não é «todos os que por acaso foram varridos».
func TestSemBancosNoPedidoRespondeSePelosDoRegisto(t *testing.T) {
	cat := catalogoDeDoisBancos(t)

	ofertas, err := cat.Comparar(pedido(t, "320000", 30, dominio.TaxaVariavel), nil, requisitosDeTres(), hoje())
	if err != nil {
		t.Fatalf("Comparar: %v", err)
	}
	if len(ofertas) != 3 {
		t.Fatalf("o registo tem 3 bancos e vieram %d ofertas a um pedido sem bancos nomeados", len(ofertas))
	}
}

// --- o catálogo de prova ---------------------------------------------------------

func catalogoDeProva(t *testing.T) *comparar.Catalogo {
	t.Helper()
	c, err := comparar.NovoCatalogo(observacoesDeProva(t))
	if err != nil {
		t.Fatalf("NovoCatalogo: %v", err)
	}
	return c
}

// observacoesDeProva reproduz a forma do que o varrimento grava: os degraus da
// escala, o ponto de referência, um ponto por produto, um com todos, os dois
// extremos de prazo, e os pontos de fixa e mista.
//
// ⚠️ Imóvel de 400 000 €, como a grelha de referência: 1 000 € de montante são
// 0,25 p.p. de LTV, e os degraus caem em números redondos.
func observacoesDeProva(t *testing.T) []varrimento.Observacao {
	t.Helper()

	var obs []varrimento.Observacao

	// Os dois degraus da escala, com a observação que traz o spread servido.
	for _, d := range []struct{ de, ate, spread string }{
		{"0.30", "0.70", "2.000"},
		{"0.70", "0.90", "1.350"},
	} {
		spread := taxa(t, d.spread)
		o := observacao(t, "variavel/0/propria", "320000", 30, dominio.TaxaVariavel, spread, nil)
		o.Degrau = &dominio.DegrauLTV{De: racio(t, d.de), Ate: racio(t, d.ate), Spread: spread}
		obs = append(obs, o)
	}

	// A referência: variável, 30 anos, sem produtos, LTV 80 % → spread 1,350.
	obs = append(obs, observacao(t, "variavel/0/propria", "320000", 30, dominio.TaxaVariavel, taxa(t, "1.350"), nil))

	// Um ponto por produto, e um com os dois. É daqui que os descontos saem.
	obs = append(obs,
		observacao(t, "variavel/0/propria", "320000", 30, dominio.TaxaVariavel,
			taxa(t, "0.850"), []string{produtoOrdenado}),
		observacao(t, "variavel/0/propria", "320000", 30, dominio.TaxaVariavel,
			taxa(t, "1.150"), []string{produtoSeguros}),
		observacao(t, "variavel/0/propria", "320000", 30, dominio.TaxaVariavel,
			taxa(t, "0.650"), []string{produtoOrdenado, produtoSeguros}),
	)

	// Os dois extremos de prazo, sem produtos. São estes que identificam os
	// encargos — e partilham a chave com tudo o que está acima.
	obs = append(obs,
		observacao(t, "variavel/0/propria", "320000", 10, dominio.TaxaVariavel, taxa(t, "1.350"), nil),
		observacao(t, "variavel/0/propria", "320000", 40, dominio.TaxaVariavel, taxa(t, "1.350"), nil),
	)

	// ⚠️ E uma bonificada **em último lugar**, de propósito. Sem ela, «ficar com
	// a última observação da chave» dava por acaso a resposta certa neste
	// catálogo — e uma reversão que passa não prova nada. Medido: com a lista a
	// terminar sem produtos, a reversão «fica com a última» passava nos nove
	// testes deste ficheiro.
	obs = append(obs, observacao(t, "variavel/0/propria", "320000", 30, dominio.TaxaVariavel,
		taxa(t, "0.650"), []string{produtoOrdenado, produtoSeguros}))

	// A fixa a 10 anos, medida a 80 % de LTV: TAN 5,550, logo base 4,200.
	fixa := observacao(t, "fixa/10/propria", "320000", 10, dominio.TaxaFixa, dominio.Taxa{}, nil)
	fixa.Oferta.Spread, fixa.Oferta.EuriborValor, fixa.Oferta.Indexante = nil, nil, ""
	tanFixa := taxa(t, "5.550")
	fixa.Oferta.TAN = &tanFixa
	fixa.Oferta.Fases = fases(t, 120, tanFixa)
	obs = append(obs, fixa)

	// A mista a 5 anos, sem produtos — e a seguir a MESMA mista bonificada.
	//
	// ⚠️ É aqui que a escolha da linha se paga, e não na variável: na mista, a
	// TAN da fase fixa **lê-se da observação**, enquanto na variável ela sai de
	// Euribor + spread da escala. Uma linha bonificada escolhida por engano dá,
	// na variável, exactamente o mesmo preço — e na mista dá 0,50 p.p. a menos a
	// quem não pediu produto nenhum.
	mista := observacao(t, "mista/5/propria", "320000", 30, dominio.TaxaMista, taxa(t, "1.350"), nil)
	tanMista := taxa(t, "3.100")
	mista.Oferta.TAN = &tanMista
	obs = append(obs, mista)

	mistaComProdutos := observacao(t, "mista/5/propria", "320000", 30, dominio.TaxaMista,
		taxa(t, "0.850"), []string{produtoOrdenado})
	tanMistaBonificada := taxa(t, "2.600")
	mistaComProdutos.Oferta.TAN = &tanMistaBonificada
	obs = append(obs, mistaComProdutos)

	return obs
}

// observacao constrói uma linha do catálogo, com a TAEG que os encargos
// verdadeiros produziriam — é isso que o ajuste tem de reencontrar.
func observacao(
	t *testing.T, cenario, montante string, prazoAnos int,
	tipo dominio.TipoTaxa, spread dominio.Taxa, produtos []string,
) varrimento.Observacao {
	t.Helper()

	p := pedido(t, montante, prazoAnos, tipo)
	p.Produtos = produtos

	euribor := taxa(t, "2.450")
	tan := euribor.Add(spread)
	meses := prazoAnos * 12

	taeg, mtic, err := encargosVerdadeiros.Aplicar(p.Montante, []dominio.Trecho{{Meses: meses, Anual: tan}})
	if err != nil {
		t.Fatalf("TAEG de prova: %v", err)
	}

	oferta := dominio.Oferta{
		BancoID: bancoID, BancoNome: bancoNome,
		TAN: &tan, TAEG: &taeg, MTIC: &mtic,
		Spread: &spread, EuriborValor: &euribor, Indexante: dominio.Euribor6M,
		Fases:             fases(t, meses, tan),
		ProdutosAplicados: produtos,
		CapturadoEm:       time.Date(2026, 7, 28, 5, 0, 11, 0, time.UTC),
	}
	prestacao := oferta.Fases[0].Prestacao
	oferta.Prestacao = &prestacao

	return varrimento.Observacao{
		Ponto:  varrimento.Ponto{Cenario: cenario, Pedido: p},
		Oferta: oferta,
	}
}

func fases(t *testing.T, meses int, tan dominio.Taxa) []dominio.Fase {
	t.Helper()
	plano, err := dominio.PlanoFrances(dinheiro(t, "320000"), []dominio.Trecho{{Meses: meses, Anual: tan}})
	if err != nil {
		t.Fatalf("plano de prova: %v", err)
	}
	return plano.Fases
}

// semFamiliaDoPrazo tira as observações de prazos que não sejam o de referência.
// ⚠️ Tira TODAS as observações fora do prazo de referência, e não só as da
// família do prazo. O ajuste dos encargos não distingue modalidades — os
// encargos são do banco e não do produto —, portanto a observação de taxa fixa a
// 10 anos também é um segundo prazo e também identificaria a repartição. Foi o
// que a primeira versão deste teste não viu: mediu-se «sem dois prazos» e
// deixou-se um segundo prazo lá dentro.
func semFamiliaDoPrazo(obs []varrimento.Observacao) []varrimento.Observacao {
	saida := make([]varrimento.Observacao, 0, len(obs))
	for _, o := range obs {
		if len(o.Oferta.Fases) > 0 && o.Oferta.Fases[len(o.Oferta.Fases)-1].AteMes != 360 {
			continue
		}
		saida = append(saida, o)
	}
	return saida
}

// --- ajudantes -------------------------------------------------------------------

// ofertaDe procura a oferta de um banco na resposta, pelo id.
func ofertaDe(ofertas []dominio.Oferta, id string) (dominio.Oferta, bool) {
	for _, o := range ofertas {
		if o.BancoID == id {
			return o, true
		}
	}
	return dominio.Oferta{}, false
}

// catalogoDeDoisBancos constrói o catálogo do banco de prova mais um segundo,
// deixando o terceiro do registo POR VARRER. É a assimetria que a KAN-45 mede.
//
// Os `filtros` aplicam-se só às observações do segundo banco, para se poder
// distinguir «não varrido» de «varrido e sem este cenário».
func catalogoDeDoisBancos(t *testing.T, filtros ...func([]varrimento.Observacao) []varrimento.Observacao) *comparar.Catalogo {
	t.Helper()

	obs := observacoesDeProva(t)
	doSegundo := doBanco(observacoesDeProva(t), segundoID, segundoNome)
	for _, filtro := range filtros {
		doSegundo = filtro(doSegundo)
	}

	c, err := comparar.NovoCatalogo(append(obs, doSegundo...))
	if err != nil {
		t.Fatalf("NovoCatalogo: %v", err)
	}
	return c
}

// doBanco reetiqueta observações para outro banco.
//
// ⚠️ Reetiqueta o `Ponto.Pedido.Produtos` também: os ids de produto trazem o
// banco no prefixo (`provabank:ordenado`), e deixá-los como estavam dava um
// segundo banco cujos descontos pertencem ao primeiro — uma incoerência que o
// `ProdutosDoBanco` apanharia mais tarde e mais longe daqui.
func doBanco(obs []varrimento.Observacao, id, nome string) []varrimento.Observacao {
	saida := make([]varrimento.Observacao, 0, len(obs))
	for _, o := range obs {
		o.Oferta.BancoID, o.Oferta.BancoNome = id, nome
		o.Oferta.ProdutosAplicados = renomearProdutos(o.Oferta.ProdutosAplicados, id)
		o.Ponto.Pedido.Produtos = renomearProdutos(o.Ponto.Pedido.Produtos, id)
		saida = append(saida, o)
	}
	return saida
}

func renomearProdutos(produtos []string, id string) []string {
	if produtos == nil {
		return nil
	}
	saida := make([]string, 0, len(produtos))
	for _, p := range produtos {
		saida = append(saida, id+":"+strings.TrimPrefix(p, bancoID+":"))
	}
	return saida
}

// soVariavel deixa passar só as observações de taxa variável — um banco varrido
// que não tem preço de fixa nem de mista.
func soVariavel(obs []varrimento.Observacao) []varrimento.Observacao {
	saida := make([]varrimento.Observacao, 0, len(obs))
	for _, o := range obs {
		if o.Ponto.Pedido.TipoTaxa == dominio.TaxaVariavel {
			saida = append(saida, o)
		}
	}
	return saida
}

// requisitosDeTres é o registo com três bancos — um a mais do que os que a
// grelha mediu.
func requisitosDeTres() map[string]dominio.Requisitos {
	todos := requisitos()
	base := todos[bancoID]
	for id, nome := range map[string]string{segundoID: segundoNome, terceiroID: terceiroNome} {
		r := base
		r.BancoID, r.BancoNome = id, nome
		todos[id] = r
	}
	return todos
}

func requisitos() map[string]dominio.Requisitos {
	return map[string]dominio.Requisitos{
		bancoID: {
			BancoID: bancoID, BancoNome: bancoNome, Custo: dominio.CustoBarato,
			PeriodosFixos:     []int{5, 10},
			PeriodosFixosModo: dominio.ModoLista,
			PrazoMin:          5,
			PrazoMax:          40,
			IdadeMaximaFim:    75,
		},
	}
}

func pedido(t *testing.T, montante string, prazoAnos int, tipo dominio.TipoTaxa) dominio.Pedido {
	t.Helper()
	p := dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(400_000),
		Montante:    dinheiro(t, montante),
		PrazoAnos:   prazoAnos,
		TipoTaxa:    tipo,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
		Titulares:   []dominio.Titular{{DataNascimento: nascidoHaAnos(30)}},
	}
	if tipo == dominio.TaxaMista {
		p.PeriodoFixoAnos = anos(5)
	}
	if tipo == dominio.TaxaFixa {
		p.PeriodoFixoAnos = anos(10)
	}
	return p
}

func comFixa(p dominio.Pedido, periodo int) dominio.Pedido {
	p.PeriodoFixoAnos = anos(periodo)
	return p
}

func ofertaUnica(t *testing.T, c *comparar.Catalogo, p dominio.Pedido) dominio.Oferta {
	t.Helper()
	ofertas, err := c.Comparar(p, nil, requisitos(), hoje())
	if err != nil {
		t.Fatalf("Comparar: %v", err)
	}
	if len(ofertas) != 1 {
		t.Fatalf("esperava uma oferta, vieram %d", len(ofertas))
	}
	return ofertas[0]
}

func verTaxa(t *testing.T, nome string, obtida *dominio.Taxa, esperada string) {
	t.Helper()
	if obtida == nil {
		t.Fatalf("%s: veio nulo, esperava %s", nome, esperada)
	}
	if obtida.String() != esperada {
		t.Errorf("%s = %s, esperava %s", nome, obtida, esperada)
	}
}

func contem(frases []string, pedaco string) bool {
	for _, f := range frases {
		if strings.Contains(f, pedaco) {
			return true
		}
	}
	return false
}

func hoje() dominio.Data { return dominio.DataDeInstante(time.Now()) }

func nascidoHaAnos(n int) dominio.Data {
	return dominio.DataDeInstante(time.Now().AddDate(-n, 0, 0))
}

func anos(n int) *int { return &n }

func dinheiro(t *testing.T, s string) dominio.Dinheiro {
	if t != nil {
		t.Helper()
	}
	d, err := dominio.DinheiroDeTexto(s)
	if err != nil {
		panic(err)
	}
	return d
}

func taxa(t *testing.T, s string) dominio.Taxa {
	if t != nil {
		t.Helper()
	}
	x, err := dominio.TaxaDeTexto(s)
	if err != nil {
		panic(err)
	}
	return x
}

func racio(t *testing.T, s string) dominio.Racio {
	if t != nil {
		t.Helper()
	}
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		panic(err)
	}
	return r
}

// TestOAjusteDosEncargosNaoDependeDaOrdemDeIteracao é o teste de um defeito que
// entrou e quase passou despercebido.
//
// ⚠️ O ajuste ancora na observação de prazo mais curto e na de mais longo. As
// observações vinham de um MAPA, e a iteração de um mapa em Go é aleatória a
// cada corrida — logo, quando duas partilhavam o prazo com TAN diferentes, qual
// delas era âncora mudava entre execuções. Medido a 2026-07-28: **uma falha em
// seis corridas**, com a TAEG a saltar de 4,4 % para 5,476 %.
//
// Um teste que falha uma vez em seis é pior do que um que falha sempre: passa no
// portão, entra, e reaparece em produção como «às vezes o número está mal».
func TestOAjusteDosEncargosNaoDependeDaOrdemDeIteracao(t *testing.T) {
	// Vinte catálogos construídos do zero: cada `NovoCatalogo` percorre os seus
	// próprios mapas, e a semente da aleatoriedade do Go muda a cada um.
	var primeira string
	for i := range 20 {
		o := ofertaUnica(t, catalogoDeProva(t), pedido(t, "320000", 30, dominio.TaxaVariavel))
		if o.TAEG == nil {
			t.Fatalf("corrida %d: a oferta saiu sem TAEG", i+1)
		}
		if i == 0 {
			primeira = o.TAEG.String()
			continue
		}
		if got := o.TAEG.String(); got != primeira {
			t.Fatalf("corrida %d: TAEG %s, e a primeira deu %s — o ajuste depende da ordem de iteração",
				i+1, got, primeira)
		}
	}
	t.Logf("TAEG estável em 20 catálogos: %s %%", primeira)
}
