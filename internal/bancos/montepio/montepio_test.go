package montepio_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/montepio"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/prova"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Todos os testes deste ficheiro correm offline, contra as capturas de
// `capturas/` — respostas reais do Banco Montepio, gravadas a 2026-07-27. O
// teste ao vivo está em montepio_rede_test.go, por trás de `//go:build rede`, e
// não corre no portão.

// --- o arranque -----------------------------------------------------------------

// ⚠️ O arranque não é cerimónia: sem os cookies que ele fixa, o gateway responde
// 410. Este teste afirma a ordem — arranque primeiro, cálculo depois — e que o
// hash lido da página vai na query string do cálculo.
func TestOArranqueVemPrimeiroEOHashVaiNaQuery(t *testing.T) {
	banco, falso := montar(t, "variavel_3m")

	if _, err := banco.Simular(t.Context(), pedidoBase()); err != nil {
		t.Fatalf("Simular: %v", err)
	}

	arranques := falso.Arranques()
	if len(arranques) != 1 || arranques[0] != montepio.URLArranque {
		t.Fatalf("esperava um arranque em %s, vieram %v", montepio.URLArranque, arranques)
	}

	pedidos := falso.Pedidos()
	if len(pedidos) != 1 {
		t.Fatalf("esperava um pedido ao gateway, vieram %d", len(pedidos))
	}
	if hash := pedidos[0].URL.Query().Get("hash"); hash != hashDaCaptura {
		t.Errorf("o hash do cálculo devia ser o que a página deu (%s), foi %q", hashDaCaptura, hash)
	}
}

// O HashRequest e a tabela de prazo por idade lêem-se da página, sem rede.
//
// ⚠️ A tabela vir do banco, e não de uma constante nossa, é o que a impede de
// envelhecer em silêncio — é a lição que o Novo Banco deixou ao trazer o prazo
// máximo dentro do próprio erro.
func TestLerAPaginaDoArranque(t *testing.T) {
	html := captura(t, "arranque.html")

	hash, err := montepio.LerHashRequest(html)
	if err != nil {
		t.Fatalf("LerHashRequest: %v", err)
	}
	if hash != hashDaCaptura {
		t.Errorf("HashRequest: esperava %s, veio %s", hashDaCaptura, hash)
	}

	escaloes, doBanco := montepio.LerEscaloesDePrazo(html)
	if !doBanco {
		t.Fatal("a página traz o maxmortgageterm; devia ter sido lido dela, e não da tabela de reserva")
	}
	querido := []montepio.EscalaoDePrazo{
		{IdadeMin: 0, IdadeMax: 30, PrazoMaxAnos: 40},
		{IdadeMin: 31, IdadeMax: 35, PrazoMaxAnos: 37},
		{IdadeMin: 36, IdadeMax: 99, PrazoMaxAnos: 35},
	}
	if len(escaloes) != len(querido) {
		t.Fatalf("escalões: esperava %d, vieram %+v", len(querido), escaloes)
	}
	for i, e := range querido {
		if escaloes[i] != e {
			t.Errorf("escalão %d: esperava %+v, veio %+v", i, e, escaloes[i])
		}
	}
}

// Uma página sem a configuração cai na tabela medida, e diz que caiu.
func TestSemConfiguracaoNaPaginaValeATabelaMedida(t *testing.T) {
	escaloes, doBanco := montepio.LerEscaloesDePrazo([]byte(`<html><body>sem configuração nenhuma</body></html>`))
	if doBanco {
		t.Error("não havia maxmortgageterm nenhum na página: não se pode dizer que veio do banco")
	}
	if anos := montepio.PrazoMaximoDoEscalao(escaloes, 36); anos != 35 {
		t.Errorf("aos 36 anos a tabela medida dá 35 anos de prazo, deu %d", anos)
	}
}

func TestPaginaSemHashRequestNaoPassaPorBoa(t *testing.T) {
	if _, err := montepio.LerHashRequest([]byte(`<html><body>outra página qualquer</body></html>`)); err == nil {
		t.Fatal("sem HashRequest o cálculo responde 410: ler isto como bom era esconder a causa")
	}
}

// --- o que o banco responde ------------------------------------------------------

// A taxa variável: os números da captura, lidos um a um. A TAN bate com a soma —
// 2,339 + 1,500 = 3,839.
func TestVariavelLeOsNumerosDaCaptura(t *testing.T) {
	banco, _ := montar(t, "variavel_3m")

	oferta, err := banco.Simular(t.Context(), pedidoBase())
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	verTaxa(t, "TAN", oferta.TAN, "3.839")
	verTaxa(t, "TAEG", oferta.TAEG, "4.5")
	verTaxa(t, "spread", oferta.Spread, "1.5")
	verTaxa(t, "Euribor", oferta.EuriborValor, "2.339")
	verDinheiro(t, "prestação", oferta.Prestacao, "936.36")
	verDinheiro(t, "MTIC", oferta.MTIC, "359650.60")

	if oferta.Indexante != dominio.Euribor3M {
		t.Errorf("indexante: esperava 3m, veio %q", oferta.Indexante)
	}
	if len(oferta.Fases) != 1 || oferta.Fases[0].AteMes != 360 {
		t.Fatalf("uma variável tem uma fase até ao mês 360, vieram %+v", oferta.Fases)
	}
	if len(oferta.Ajustes()) != 0 {
		t.Errorf("o pedido coube tal como foi feito: não devia haver ajustes, vieram %+v", oferta.Ajustes())
	}
}

// ⚠️ **O indexante lê-se da resposta, não do que pedimos.** E não é zelo: o
// código da Euribor a 12 meses é `EH2`, não "EH12" — medido a 2026-07-27. Um mapa
// nosso a partir da família pedida acertava por acaso e nunca veria uma
// renumeração do lado do banco.
func TestOIndexanteVemDaRespostaENaoDoQueSePediu(t *testing.T) {
	banco, falso := montar(t, "variavel_12m")

	p := pedidoBase()
	p.Indexante = dominio.Euribor12M

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.ConditionCode != "21H0---H000-V" {
		t.Errorf("a Euribor a 12 meses escolhe-se pela família H0: foi %q", enviado.ConditionCode)
	}
	if oferta.Indexante != dominio.Euribor12M {
		t.Errorf("indexante: o banco respondeu EH2, que é a de 12 meses; veio %q", oferta.Indexante)
	}
	verTaxa(t, "Euribor", oferta.EuriborValor, "2.798")
	verTaxa(t, "TAN", oferta.TAN, "4.298")
}

// ⚠️ A mista sai **sem plano de fases**, e isso é uma afirmação.
//
// Na captura, a fase indexada vem com a TAN e a prestação repetidas da fase fixa
// — 4,350 e 995,62 — quando a taxa que ela própria declara é 2,339 + 1,500 =
// 3,839. Os dois números não podem estar os dois certos, e publicar a fase com
// qualquer um deles afirmava uma coisa que o banco não disse. O que ele declarou
// para essa fase vai por nota.
func TestMistaNaoPublicaUmPlanoQueSeContradiz(t *testing.T) {
	banco, falso := montar(t, "mista_5a")

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = anos(5)

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.ConditionCode != "21H9---H900-M5" {
		t.Errorf("cinco anos de taxa fixa dão o sufixo M5: foi %q", enviado.ConditionCode)
	}
	verTaxa(t, "TAN do período fixo", oferta.TAN, "4.35")
	verDinheiro(t, "prestação do período fixo", oferta.Prestacao, "995.62")

	if len(oferta.Fases) != 0 {
		t.Errorf("a fase indexada do Montepio repete a prestação da fixa; não se publica um plano assim: %+v",
			oferta.Fases)
	}
	verNotaContem(t, oferta, "EURIBOR-3 MESES")
	verNotaContem(t, oferta, "projectando a taxa do período fixo")

	// O indexante da cauda continua a ler-se, mesmo sem plano: é o que o banco
	// declarou para depois do período fixo.
	if oferta.Indexante != dominio.Euribor3M {
		t.Errorf("a cauda da mista é indexada à Euribor a 3 meses, veio %q", oferta.Indexante)
	}
}

// Com o período fixo igual ao prazo o banco devolve **uma** fase, e o que sai é
// taxa fixa em todo o contrato. Não há ajuste — nenhum número do pedido mudou —
// mas há nota: o produto contratado é o de taxa mista.
func TestFixaComPrazoNaListaSaiFixaAoPrazoTodo(t *testing.T) {
	banco, falso := montar(t, "fixa_30a")

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaFixa

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.ConditionCode != "21H9---H900-M30" {
		t.Errorf("a fixa a 30 anos faz-se com o período fixo igual ao prazo (M30): foi %q", enviado.ConditionCode)
	}
	verTaxa(t, "TAN", oferta.TAN, "4.85")
	verDinheiro(t, "prestação", oferta.Prestacao, "1055.38")

	if len(oferta.Fases) != 1 || oferta.Fases[0].AteMes != 360 {
		t.Fatalf("uma fixa ao prazo todo tem uma fase até ao mês 360, vieram %+v", oferta.Fases)
	}
	if oferta.Indexante != "" {
		t.Errorf("numa fixa ao prazo todo não há Euribor a declarar, veio %q", oferta.Indexante)
	}
	if len(oferta.Ajustes()) != 0 {
		t.Errorf("o prazo pedido é um dos períodos praticados: não há nada a ajustar, veio %+v", oferta.Ajustes())
	}
	verNotaContem(t, oferta, "não tem produto de taxa fixa")
}

// ⚠️ Fora da lista de períodos, a "taxa fixa" do Montepio **é** taxa mista: sobra
// uma cauda indexada. Isso muda o produto face ao que se pediu, e vai como
// ajuste de tipo de taxa — que é o caso que obrigou o AjusteTipoTaxa a existir.
func TestFixaForaDaListaViraMistaEDiLo(t *testing.T) {
	banco, falso := montar(t, "mista_5a")

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = 20 // 20 não é um dos períodos praticados; o maior que cabe é 15

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.ConditionCode != "21H9---H900-M15" {
		t.Errorf("num prazo de 20 anos o período fixo mais longo que cabe é 15 (M15): foi %q", enviado.ConditionCode)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 || ajustes[0].Campo() != dominio.AjustadoTipoTaxa {
		t.Fatalf("esperava um ajuste de tipo de taxa, vieram %+v", ajustes)
	}
	if ajustes[0].Para() != string(dominio.TaxaMista) {
		t.Errorf("o que sai é taxa mista, e o ajuste devia dizê-lo: veio %v", ajustes[0].Para())
	}
	if !strings.Contains(ajustes[0].Nota(), "não tem taxa fixa pura") {
		t.Errorf("a nota tem de nomear a razão, veio %q", ajustes[0].Nota())
	}
}

// O período fixo que o banco não pratica encaixa no maior abaixo, e a diferença
// fica anotada em vez de ir bater na recusa muda do banco.
func TestPeriodoFixoForaDaListaEncaixaEAnota(t *testing.T) {
	banco, falso := montar(t, "mista_5a")

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = anos(12) // 12 não existe: M12 é recusado sem mensagem

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.ConditionCode != "21H9---H900-M10" {
		t.Errorf("12 anos não existem; o maior abaixo é 10 (M10): foi %q", enviado.ConditionCode)
	}
	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 || ajustes[0].Campo() != dominio.AjustadoPeriodoFixo {
		t.Fatalf("esperava um ajuste de período fixo, vieram %+v", ajustes)
	}
	if ajustes[0].De() != 12 || ajustes[0].Para() != 10 {
		t.Errorf("o ajuste tem de nomear os dois números (12 → 10), veio %v → %v", ajustes[0].De(), ajustes[0].Para())
	}
}

// --- a finalidade e o imposto ----------------------------------------------------

// ⚠️ No arrendamento a prestação **já inclui** o Imposto do Selo sobre os juros,
// e nos outros bancos não. Medido a 2026-07-27: mesma TAN de 3,839 %, prestação
// de 953,97 contra 936,36 — 4 % dos juros. Sem a nota, o Montepio aparecia mais
// caro sem se perceber porquê, e a comparação era entre coisas diferentes.
func TestArrendamentoAvisaQueOImpostoVaiDentroDaPrestacao(t *testing.T) {
	banco, falso := montar(t, "variavel_arrendamento")

	p := pedidoBase()
	p.Finalidade = dominio.FinalidadeArrendamento

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.ProductCode != "24" || enviado.ConditionCode != "24H9---H900-V" {
		t.Errorf("o arrendamento tem produto próprio (24): foi %q / %q", enviado.ProductCode, enviado.ConditionCode)
	}
	verTaxa(t, "TAN", oferta.TAN, "3.839")
	verDinheiro(t, "prestação", oferta.Prestacao, "953.97")
	verNotaContem(t, oferta, "Imposto do Selo")
}

// A segunda habitação tem produto próprio e o preço da primeira — medido.
func TestSegundaHabitacaoTemProdutoProprioEOMesmoPreco(t *testing.T) {
	banco, falso := montar(t, "variavel_secundaria")

	p := pedidoBase()
	p.Finalidade = dominio.FinalidadeSecundaria

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.ProductCode != "23" {
		t.Errorf("a segunda habitação é o produto 23, foi %q", enviado.ProductCode)
	}
	verDinheiro(t, "prestação", oferta.Prestacao, "936.36")
	for _, n := range oferta.Notas() {
		if strings.Contains(n, "Imposto do Selo") {
			t.Errorf("na segunda habitação o imposto fica fora da prestação: a nota não devia sair — %q", n)
		}
	}
}

// --- as contrapartidas ------------------------------------------------------------

// ⚠️ Quem escolhe é o pedido (KAN-33). Sem escolha, o preço é o base — e a oferta
// diz que o é, para não parecer que o banco não tem descontos.
func TestSemContrapartidasVaiZeroEDiz(t *testing.T) {
	banco, falso := montar(t, "variavel_3m")

	oferta, err := banco.Simular(t.Context(), pedidoBase())
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.Counterparts != 0 {
		t.Errorf("sem produtos escolhidos vão zero contrapartidas, foram %d", enviado.Counterparts)
	}
	if len(oferta.ProdutosAplicados) != 0 {
		t.Errorf("não se aplicou produto nenhum, veio %v", oferta.ProdutosAplicados)
	}
	verNotaContem(t, oferta, "não inclui as contrapartidas")
}

// Com o produto escolhido vão as quatro, e o desconto medido aparece na nota com
// os dois spreads.
func TestContrapartidasEscolhidasDescontamEDizemQuanto(t *testing.T) {
	banco, falso := montar(t, "variavel_contrapartidas")

	p := pedidoBase()
	p.Produtos = []string{montepio.ProdutoContrapartidas}

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.Counterparts != montepio.ContrapartidasMaximas {
		t.Errorf("o produto escolhido vale as quatro contrapartidas, foram %d", enviado.Counterparts)
	}
	verTaxa(t, "spread com contrapartidas", oferta.Spread, "0.7")
	if len(oferta.ProdutosAplicados) != 1 || oferta.ProdutosAplicados[0] != montepio.ProdutoContrapartidas {
		t.Errorf("o produto aplicado tem de vir na oferta, veio %v", oferta.ProdutosAplicados)
	}
	verNotaContem(t, oferta, "0.8 p.p.")
}

// Um produto de outro banco não é deste, e não desconta nada aqui.
func TestProdutoDeOutroBancoNaoDescontaAqui(t *testing.T) {
	banco, falso := montar(t, "variavel_3m")

	p := pedidoBase()
	p.Produtos = []string{"novobanco:protecao"}

	if _, err := banco.Simular(t.Context(), p); err != nil {
		t.Fatalf("Simular: %v", err)
	}
	if enviado := corpoEnviado(t, falso); enviado.Counterparts != 0 {
		t.Errorf("o produto era do Novo Banco: o Montepio não tinha nada que o aplicar (foram %d)", enviado.Counterparts)
	}
}

// --- o prazo ---------------------------------------------------------------------

// ⚠️ O escalão de idade aperta o prazo **abaixo** do máximo do banco, e é o
// arranque que o diz: aos 36 anos são 35 anos e não 40, apesar de o contrato
// ainda caber nos 76. Medido ao vivo na fronteira — 35 anos passam, 36 não.
func TestPrazoEncolhePeloEscalaoQueOArranquePublica(t *testing.T) {
	banco, falso := montar(t, "variavel_3m")

	p := pedidoBase()
	p.PrazoAnos = 40
	p.Titulares = []dominio.Titular{{DataNascimento: nascidoHaAnos(36)}}

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.Term != 35*12 {
		t.Errorf("aos 36 anos o escalão dá 35 anos de prazo (420 meses), foram %d", enviado.Term)
	}
	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 || ajustes[0].Campo() != dominio.AjustadoPrazoAnos {
		t.Fatalf("esperava um ajuste de prazo, vieram %+v", ajustes)
	}
	if ajustes[0].De() != 40 || ajustes[0].Para() != 35 {
		t.Errorf("o ajuste conta-se contra o prazo pedido (40 → 35), veio %v → %v", ajustes[0].De(), ajustes[0].Para())
	}
}

// Aos 70 anos manda o outro limite: o contrato tem de terminar até aos 76.
// Medido ao vivo — 6 anos passam, 7 não.
func TestPrazoEncolhePelaIdadeDeFimDoContrato(t *testing.T) {
	banco, falso := montar(t, "variavel_3m")

	p := pedidoBase()
	p.PrazoAnos = 20
	p.Titulares = []dominio.Titular{{DataNascimento: nascidoHaAnos(70)}}

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.Term != 6*12 {
		t.Errorf("aos 70 anos o contrato só cabe em 6 anos (72 meses), foram %d", enviado.Term)
	}
	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 {
		t.Fatalf("esperava um ajuste de prazo, vieram %+v", ajustes)
	}
	if !strings.Contains(ajustes[0].Nota(), "76") {
		t.Errorf("a nota tem de nomear a idade que fecha o contrato, veio %q", ajustes[0].Nota())
	}
}

// Manda o titular **mais velho**, não o primeiro do pedido.
func TestMandaOTitularMaisVelhoEstejaOndeEstiver(t *testing.T) {
	banco, falso := montar(t, "variavel_dois_titulares")

	p := pedidoBase()
	p.PrazoAnos = 30
	p.Titulares = []dominio.Titular{
		{DataNascimento: nascidoHaAnos(30)},
		{DataNascimento: nascidoHaAnos(70)},
	}

	if _, err := banco.Simular(t.Context(), p); err != nil {
		t.Fatalf("Simular: %v", err)
	}
	if enviado := corpoEnviado(t, falso); enviado.Term != 6*12 {
		t.Errorf("quem aperta o prazo é o titular de 70 anos (72 meses), foram %d", enviado.Term)
	}
}

// Quando nem o prazo mínimo cabe na idade, não há ajuste que salve — e fingir
// que há era pior. ⚠️ Nem se chega a gastar um pedido ao banco.
func TestIdadeQueNaoDeixaCaberNemOMinimoFalhaSemIrAoBanco(t *testing.T) {
	banco, falso := montar(t, "variavel_3m")

	p := pedidoBase()
	p.Titulares = []dominio.Titular{{DataNascimento: nascidoHaAnos(73)}}

	_, err := banco.Simular(t.Context(), p)

	var erroOferta *dominio.ErroOferta
	if !errors.As(err, &erroOferta) || erroOferta.Codigo != dominio.ErroPrazoImpossivel {
		t.Fatalf("esperava prazo_impossivel, veio %v", err)
	}
	if len(falso.Pedidos()) != 0 {
		t.Errorf("não havia nada a perguntar ao banco: foram %d pedidos", len(falso.Pedidos()))
	}
}

// O prazo abaixo do mínimo sobe para os 5 anos do banco, e diz-se.
func TestPrazoAbaixoDoMinimoSobeEAnota(t *testing.T) {
	banco, falso := montar(t, "variavel_3m")

	p := pedidoBase()
	p.PrazoAnos = 3

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}
	if enviado := corpoEnviado(t, falso); enviado.Term != montepio.PrazoMinimo*12 {
		t.Errorf("o mínimo do banco são 5 anos (60 meses), foram %d", enviado.Term)
	}
	if ajustes := oferta.Ajustes(); len(ajustes) != 1 || ajustes[0].Para() != montepio.PrazoMinimo {
		t.Errorf("esperava um ajuste do prazo para 5 anos, vieram %+v", ajustes)
	}
}

// --- as recusas -------------------------------------------------------------------

// ⚠️ Um período que o banco não pratica é recusado **sem mensagem nenhuma**:
// `Message` vazia e `Code` nulo. Não se inventa uma causa — nomeia-se o que se
// sabe, que é o que a captura mostra.
func TestRecusaMudaNaoInventaCausa(t *testing.T) {
	banco, _ := montar(t, "erro_periodo_inexistente")

	_, err := banco.Simular(t.Context(), pedidoBase())

	var erroOferta *dominio.ErroOferta
	if !errors.As(err, &erroOferta) || erroOferta.Codigo != dominio.ErroProdutoIndisponivel {
		t.Fatalf("esperava produto_indisponivel, veio %v", err)
	}
	if !strings.Contains(erroOferta.Mensagem, "sem dizer porquê") {
		t.Errorf("a mensagem tem de dizer que o banco não deu razão, veio %q", erroOferta.Mensagem)
	}
}

// ⚠️ **A mensagem do banco mente sobre a causa.** Medido a 2026-07-27: um prazo
// de 20 anos com 30 de período fixo devolve "Não existem condições disponíveis
// para a idade dos proponentes" a um titular de 36 anos. Repete-se a frase do
// banco, mas não se lhe chama diagnóstico.
func TestRecusaComMensagemNaoATomaPorDiagnostico(t *testing.T) {
	banco, _ := montar(t, "erro_periodo_maior_que_prazo")

	_, err := banco.Simular(t.Context(), pedidoBase())

	var erroOferta *dominio.ErroOferta
	if !errors.As(err, &erroOferta) {
		t.Fatalf("esperava um ErroOferta, veio %v", err)
	}
	if !strings.Contains(erroOferta.Mensagem, "idade dos proponentes") {
		t.Errorf("a mensagem do banco tem de aparecer, veio %q", erroOferta.Mensagem)
	}
	if !strings.Contains(erroOferta.Mensagem, "causas que nada têm que ver com a idade") {
		t.Errorf("a mensagem tem de avisar que o banco dá esta razão para outras causas, veio %q", erroOferta.Mensagem)
	}
}

// O 410 do gateway é o que sai quando o pedido vai sem os cookies do arranque.
// ⚠️ Nomeia-se a causa: a mensagem do banco ("New open window with different
// context") não diz nada a quem a leia sem este contexto.
func TestGatewayResponde410SemOsCookiesDoArranque(t *testing.T) {
	falso := &transporte.Falso{
		HTMLDeArranque: captura(t, "arranque.html"),
		Responder: func(transporte.PedidoGravado) (*http.Response, error) {
			return transporte.RespostaDeTexto(http.StatusGone, string(captura(t, "erro_sem_sessao.resposta.json"))), nil
		},
	}

	_, err := montepio.Novo(falso).Simular(t.Context(), pedidoBase())

	var erroOferta *dominio.ErroOferta
	if !errors.As(err, &erroOferta) || erroOferta.Codigo != dominio.ErroBancoIndisponivel {
		t.Fatalf("esperava banco_indisponivel, veio %v", err)
	}
	if !strings.Contains(erroOferta.Mensagem, "410") || !strings.Contains(erroOferta.Mensagem, "cookies") {
		t.Errorf("a mensagem tem de nomear o 410 e a razão dele, veio %q", erroOferta.Mensagem)
	}
}

// Um arranque que falha é falha de arranque, e não uma recusa do produto.
func TestArranqueQueFalhaNaoSeConfundeComRecusa(t *testing.T) {
	falso := &transporte.Falso{ErroDeArranque: errors.New("ligação recusada")}

	_, err := montepio.Novo(falso).Simular(t.Context(), pedidoBase())

	var erroOferta *dominio.ErroOferta
	if !errors.As(err, &erroOferta) || erroOferta.Codigo != dominio.ErroBancoIndisponivel {
		t.Fatalf("esperava banco_indisponivel, veio %v", err)
	}
	if !strings.Contains(erroOferta.Mensagem, "arranque") {
		t.Errorf("a mensagem tem de dizer que foi o arranque, veio %q", erroOferta.Mensagem)
	}
}

// --- o contrato -------------------------------------------------------------------

func TestRequisitosSaoServiveis(t *testing.T) {
	banco, _ := montar(t, "variavel_3m")
	if err := banco.Requisitos().Validar(); err != nil {
		t.Fatalf("Requisitos: %v", err)
	}
}

// ⚠️ Um banco que ignore o ctx segura a comparação inteira. A afirmação é de cada
// banco, não do contrato em geral — e este tem **dois** pedidos, o arranque e o
// cálculo, o que dá dois sítios por onde a esquecer.
func TestSimularRespeitaOPrazoDoContexto(t *testing.T) {
	falso := &transporte.Falso{
		HTMLDeArranque: captura(t, "arranque.html"),
		Corpo:          string(captura(t, "variavel_3m.resposta.json")),
		Atraso:         10 * time.Second,
	}
	prova.RespeitaPrazo(t, montepio.Novo(falso), pedidoBase(), 200*time.Millisecond)
}

// --- ajudantes --------------------------------------------------------------------

// hashDaCaptura é o HashRequest que a página gravada traz. É do módulo, não da
// sessão: estável, e por isso afirmável num teste.
const hashDaCaptura = "e45a4619da78c3b9dc6b789bdae336e1"

func montar(t *testing.T, nomeDaCaptura string) (bancos.Banco, *transporte.Falso) {
	t.Helper()

	corpo := string(captura(t, nomeDaCaptura+".resposta.json"))
	falso := &transporte.Falso{
		HTMLDeArranque: captura(t, "arranque.html"),
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			if !strings.Contains(p.URL.Path, "/Calculator/Calculate") {
				t.Errorf("pedido a um caminho que o Montepio não tem: %q", p.URL.Path)
			}
			if p.Cabecalhos.Get("X-Requested-With") != "XMLHttpRequest" {
				t.Error("o gateway espera o X-Requested-With de um pedido de browser")
			}
			return transporte.RespostaDeTexto(http.StatusOK, corpo), nil
		},
	}
	return montepio.Novo(falso), falso
}

func captura(t *testing.T, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("capturas", nome))
	if err != nil {
		t.Fatalf("ler a captura %s: %v", nome, err)
	}
	return b
}

// corpoLido é o payload visto do lado de quem o recebe.
type corpoLido struct {
	ConditionCode         string      `json:"ConditionCode"`
	CreditDestinationCode string      `json:"CreditDestinationCode"`
	ProductCode           string      `json:"ProductCode"`
	FamilyCode            string      `json:"FamilyCode"`
	AcquisitionAmount     json.Number `json:"AcquisitionAmount"`
	Ammount               json.Number `json:"Ammount"`
	Term                  int         `json:"Term"`
	Counterparts          int         `json:"Counterparts"`
	Proponents            []struct {
		Birthday string `json:"Birthday"`
		Position int    `json:"Position"`
	} `json:"Proponents"`
}

func corpoEnviado(t *testing.T, f *transporte.Falso) corpoLido {
	t.Helper()
	pedidos := f.Pedidos()
	if len(pedidos) == 0 {
		t.Fatal("não houve pedido nenhum ao banco")
	}
	var c corpoLido
	if err := json.Unmarshal(pedidos[0].Corpo, &c); err != nil {
		t.Fatalf("ler o payload: %v", err)
	}
	return c
}

func pedidoBase() dominio.Pedido {
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dominio.DinheiroDeInteiro(200_000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
		Titulares:   []dominio.Titular{{DataNascimento: nascidoHaAnos(36)}},
	}
}

func anos(n int) *int { return &n }

// nascidoHaAnos devolve a data de nascimento de quem faz n anos hoje.
//
// ⚠️ Conta-se a partir de hoje de propósito: a idade entra no Simular por um
// time.Now(), e uma data fixa fazia estes testes mudar de significado com o
// passar dos anos — o titular de 1990 que hoje tem 36 teria 40 daqui a quatro.
func nascidoHaAnos(n int) dominio.Data {
	hoje := time.Now()
	return dominio.DataDeInstante(hoje.AddDate(-n, 0, -1))
}

func verNotaContem(t *testing.T, o dominio.Oferta, pedaco string) {
	t.Helper()
	for _, n := range o.Notas() {
		if strings.Contains(n, pedaco) {
			return
		}
	}
	t.Errorf("nenhuma nota diz %q; as notas são %v", pedaco, o.Notas())
}

func verTaxa(t *testing.T, nome string, veio *dominio.Taxa, esperado string) {
	t.Helper()
	if veio == nil {
		t.Errorf("%s: não veio", nome)
		return
	}
	querido, err := dominio.TaxaDeTexto(esperado)
	if err != nil {
		t.Fatalf("esperado inválido %q: %v", esperado, err)
	}
	if !veio.Equal(querido) {
		t.Errorf("%s: esperava %s, veio %s", nome, esperado, veio)
	}
}

func verDinheiro(t *testing.T, nome string, veio *dominio.Dinheiro, esperado string) {
	t.Helper()
	if veio == nil {
		t.Errorf("%s: não veio", nome)
		return
	}
	querido, err := dominio.DinheiroDeTexto(esperado)
	if err != nil {
		t.Fatalf("esperado inválido %q: %v", esperado, err)
	}
	if !veio.Equal(querido) {
		t.Errorf("%s: esperava %s, veio %s", nome, esperado, veio)
	}
}
