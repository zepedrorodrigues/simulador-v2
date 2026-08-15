package novobanco_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/novobanco"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/prova"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Todos os testes deste ficheiro correm offline, contra as capturas de
// `capturas/` — respostas reais do Novo Banco, gravadas a 2026-07-26. O teste ao
// vivo está em novobanco_rede_test.go, por trás de `//go:build rede`, e não
// corre no portão.

// --- o que o banco responde ----------------------------------------------------

// A taxa variável: os números da captura, lidos um a um.
//
// ⚠️ É aqui que se afirma que `taxaIndexada` é a Euribor — e só aqui. A TAN
// bate com a soma: 2,798 + 0,9 = 3,698.
func TestVariavelLeOsNumerosDaCaptura(t *testing.T) {
	banco, _ := montar(t, "variavel_12m")

	oferta, err := banco.Simular(t.Context(), pedidoBase())
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	verTaxa(t, "TAN", oferta.TAN, "3.698")
	verTaxa(t, "TAEG", oferta.TAEG, "4.4")
	verTaxa(t, "spread", oferta.Spread, "0.9")
	verTaxa(t, "Euribor", oferta.EuriborValor, "2.798")
	verDinheiro(t, "prestação", oferta.Prestacao, "920.34")
	verDinheiro(t, "MTIC", oferta.MTIC, "356005.55")

	if oferta.Indexante != dominio.Euribor12M {
		t.Errorf("indexante: esperava 12m, veio %q", oferta.Indexante)
	}
	if len(oferta.Fases) != 1 || oferta.Fases[0].AteMes != 360 {
		t.Fatalf("uma variável tem uma fase até ao mês 360, vieram %+v", oferta.Fases)
	}
	if len(oferta.Ajustes()) != 0 {
		t.Errorf("o pedido coube tal como foi feito: não devia haver ajustes, vieram %+v", oferta.Ajustes())
	}
}

// O indexante é escolhível, e é o único banco de HTTP puro em que o é. A
// captura de 3M prova que o código escolhido chega ao banco e volta na resposta.
func TestVariavelComEuriborEscolhida(t *testing.T) {
	banco, falso := montar(t, "variavel_3m")

	p := pedidoBase()
	p.Indexante = dominio.Euribor3M

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.TipoTaxaIndexante != "INDEXADA_EURIBOR_3_MES" {
		t.Errorf("pediu-se a Euribor a 3 meses e foi %q para o banco", enviado.TipoTaxaIndexante)
	}
	verTaxa(t, "Euribor", oferta.EuriborValor, "2.339")
	verTaxa(t, "TAN", oferta.TAN, "3.239")
	if oferta.Indexante != dominio.Euribor3M {
		t.Errorf("indexante: esperava 3m, veio %q", oferta.Indexante)
	}
	if len(oferta.Ajustes()) != 0 {
		t.Errorf("o banco aceitou o indexante pedido: não há nada a ajustar, veio %+v", oferta.Ajustes())
	}
}

// ⚠️ A mista sai **sem fases**, e sem Euribor nenhuma. As duas coisas são
// afirmações, não omissões: o banco devolve uma TAN só — a do período fixo — e
// não diz que taxa nem que indexante aplica depois dele. Publicar 3,992 % como
// se durasse 30 anos era dizer uma coisa que o banco não disse.
func TestMistaNaoInventaOQueOBancoNaoDiz(t *testing.T) {
	banco, _ := montar(t, "mista_5a")

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = anos(5)

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	verTaxa(t, "TAN do período fixo", oferta.TAN, "3.992")
	verDinheiro(t, "prestação", oferta.Prestacao, "953.91")

	if len(oferta.Fases) != 0 {
		t.Errorf("o Novo Banco não publica o plano da mista; não se inventa um: vieram %+v", oferta.Fases)
	}
	if oferta.EuriborValor != nil {
		t.Errorf("na mista o taxaIndexada é a taxa de referência do período fixo, não uma Euribor — veio %s", oferta.EuriborValor)
	}
	if oferta.Indexante != "" {
		t.Errorf("na mista o banco não declara indexante nenhum, veio %q", oferta.Indexante)
	}
}

// Na fixa o prazo é o período, e a fase cobre o prazo todo — isso é verdade
// neste banco por construção.
func TestFixaCobreOPrazoTodo(t *testing.T) {
	banco, falso := montar(t, "fixa_10a")

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = 10

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.TipoTaxaIndexante != "FIXA_10_ANOS" || enviado.Prazo != 10 {
		t.Errorf("a fixa exige prazo igual ao período; foi %q com prazo %d", enviado.TipoTaxaIndexante, enviado.Prazo)
	}
	verTaxa(t, "TAN", oferta.TAN, "5.124")
	verDinheiro(t, "prestação", oferta.Prestacao, "2133.45")

	if len(oferta.Fases) != 1 || oferta.Fases[0].AteMes != 120 {
		t.Fatalf("a fixa tem uma fase até ao mês 120, vieram %+v", oferta.Fases)
	}
	if oferta.EuriborValor != nil {
		t.Errorf("numa taxa fixa não há Euribor a declarar, veio %s", oferta.EuriborValor)
	}
}

// ⚠️ Na mista o período fixo tem de ser **estritamente** menor do que o prazo:
// 25 anos de fixo num prazo de 25 é recusado com V158, e a 26 passa — medido. O
// encaixe desce ao maior que cabe, e a diferença fica anotada em vez de ir
// bater no erro do banco.
func TestMistaEncolheOPeriodoQueNaoCabeNoPrazo(t *testing.T) {
	banco, falso := montar(t, "mista_5a")

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaMista
	p.PrazoAnos = 25
	p.PeriodoFixoAnos = anos(25)

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.TipoTaxaIndexante != "MISTA_20_ANOS" {
		t.Errorf("25 anos de fixo não cabem num prazo de 25; esperava descer a MISTA_20_ANOS, foi %q",
			enviado.TipoTaxaIndexante)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 || ajustes[0].Campo() != dominio.AjustadoPeriodoFixo {
		t.Fatalf("o período mudou: esperava um ajuste de período fixo, vieram %+v", ajustes)
	}
	if ajustes[0].De() != 25 || ajustes[0].Para() != 20 {
		t.Errorf("o ajuste tem de dizer de 25 para 20, disse de %v para %v", ajustes[0].De(), ajustes[0].Para())
	}
}

// ⚠️ Na fixa, um prazo fora dos nove valores tem de ser encaixado **antes** de
// se falar com o banco: mandá-lo tal como veio dá V157, e o que a pessoa recebia
// era uma recusa em vez de uma simulação com uma nota.
//
// 12 anos não existem; o maior que cabe abaixo é 10.
func TestFixaEncaixaOPrazoQueOBancoNaoPratica(t *testing.T) {
	banco, falso := montar(t, "fixa_10a")

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = 12

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	enviado := corpoEnviado(t, falso)
	if enviado.TipoTaxaIndexante != "FIXA_10_ANOS" || enviado.Prazo != 10 {
		t.Errorf("12 anos de fixa não existem; esperava FIXA_10_ANOS com prazo 10, foi %q com prazo %d",
			enviado.TipoTaxaIndexante, enviado.Prazo)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 || ajustes[0].Campo() != dominio.AjustadoPrazoAnos {
		t.Fatalf("o prazo mudou de 12 para 10: esperava um ajuste de prazo, vieram %+v", ajustes)
	}
	if ajustes[0].De() != 12 || ajustes[0].Para() != 10 {
		t.Errorf("o ajuste tem de dizer de 12 para 10, disse de %v para %v", ajustes[0].De(), ajustes[0].Para())
	}
	if !strings.Contains(ajustes[0].Nota(), "prazo todo") {
		t.Errorf("a nota tem de explicar que a fixa é ao prazo todo: %q", ajustes[0].Nota())
	}
}

// A finalidade não é decorativa neste banco: o arrendamento custa +0,50 p.p.
func TestArrendamentoCustaMeioPontoDeSpread(t *testing.T) {
	banco, falso := montar(t, "variavel_arrendamento")

	p := pedidoBase()
	p.Finalidade = dominio.FinalidadeArrendamento

	oferta, err := banco.Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); enviado.Imovel.TipoPropriedade != "ARRENDAMENTO" {
		t.Errorf("a finalidade não chegou ao banco: foi %q", enviado.Imovel.TipoPropriedade)
	}
	// 1,4 no arrendamento contra 0,9 na própria — medido a 2026-07-26.
	verTaxa(t, "spread", oferta.Spread, "1.4")
	verTaxa(t, "TAN", oferta.TAN, "4.198")
}

// --- as bonificações ------------------------------------------------------------

// As bonificações que a pessoa escolheu vão no pedido, e o que elas valem tem de
// sair escrito: um preço descontado apresentado ao lado de bancos sem desconto,
// sem uma palavra, é uma comparação entre coisas diferentes.
func TestBonificacoesEscolhidasVaoNoPedidoEODescontoFicaEscrito(t *testing.T) {
	banco, falso := montar(t, "variavel_12m")

	oferta, err := banco.Simular(t.Context(), comAsBonificacoes(pedidoBase()))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	enviado := corpoEnviado(t, falso)
	if len(enviado.Bonificacoes) != 2 {
		t.Errorf("escolheram-se as duas bonificações, foram %v", enviado.Bonificacoes)
	}
	if len(oferta.ProdutosAplicados) != 2 {
		t.Errorf("a oferta tem de dizer que produtos aplicou, disse %v", oferta.ProdutosAplicados)
	}

	// 1,6 sem bonificações contra 0,9 com elas: 0,7 p.p.
	if !algumaNotaContem(oferta, "0.7") {
		t.Errorf("o desconto das bonificações não aparece em lado nenhum: %v", oferta.Notas())
	}
}

// Quem não escolhe nada leva o preço sem desconto, e o pedido tem de sair com a
// lista de bonificações vazia.
//
// ⚠️ Este é o teste que a KAN-33 obriga a existir do lado do Novo Banco. Antes
// dela as duas bonificações iam sempre — logo o Novo Banco aparecia com spread
// 0,90 ao lado da CGD com 1,350, e a comparação dizia que ele era 0,45 p.p. mais
// barato. Em pé de igualdade é a CGD a mais barata por 0,25 p.p., nas duas
// colunas: 1,350 contra 1,600 sem produtos, 0,650 contra 0,900 com eles. O erro
// não somava ruído — invertia a resposta.
func TestSemEscolherProdutosOPrecoVaiSemDescontoEDizSeOEspecifica(t *testing.T) {
	banco, falso := montar(t, "variavel_sem_produtos")

	oferta, err := banco.Simular(t.Context(), pedidoBase())
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	if enviado := corpoEnviado(t, falso); len(enviado.Bonificacoes) != 0 {
		t.Errorf("não se escolheu produto nenhum e foram %v", enviado.Bonificacoes)
	}
	verTaxa(t, "spread sem bonificações", oferta.Spread, "1.6")
	if len(oferta.ProdutosAplicados) != 0 {
		t.Errorf("não se aplicou produto nenhum; a oferta diz %v", oferta.ProdutosAplicados)
	}
	if !algumaNotaContem(oferta, "não inclui as bonificações") {
		t.Errorf("um preço sem as bonificações tem de dizer que o é: %v", oferta.Notas())
	}
}

// O desconto que a nota promete é o que o banco cobra de facto a quem não tem
// as bonificações — e isso mede-se contra a captura em que elas não foram
// enviadas, não contra o mesmo número duas vezes.
//
// ⚠️ 1,6 sem elas e 0,9 com elas, no mesmo cenário e no mesmo dia. Se a nota
// dissesse 0,7 e o banco cobrasse outra coisa, era a nota que estava a mentir.
func TestODescontoPrometidoEOQueOBancoCobraSemAsBonificacoes(t *testing.T) {
	comBonificacoes, _ := montar(t, "variavel_12m")
	oferta, err := comBonificacoes.Simular(t.Context(), comAsBonificacoes(pedidoBase()))
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}
	verTaxa(t, "spread com bonificações", oferta.Spread, "0.9")

	semBonificacoes, _ := montar(t, "variavel_sem_produtos")
	outra, err := semBonificacoes.Simular(t.Context(), pedidoBase())
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}
	verTaxa(t, "spread sem bonificações", outra.Spread, "1.6")

	// A captura sem bonificações é a prova de que 1,6 é preço praticado, e não
	// um campo informativo da outra resposta.
	if !algumaNotaContem(oferta, "0.7") || !algumaNotaContem(oferta, "1.6") {
		t.Errorf("a nota tem de dizer o desconto e o preço sem ele: %v", oferta.Notas())
	}
	// ⚠️ E a nota de quem não escolheu não leva números: com `bonificacoes: []`
	// o banco devolve os dois spreads iguais, logo o preço bonificado não vem
	// nesta resposta e não se pode afirmar daqui.
	if algumaNotaContem(outra, "0.7") {
		t.Errorf("esta resposta não traz o preço bonificado; a nota não pode citá-lo: %v", outra.Notas())
	}
}

// --- os erros estruturados ------------------------------------------------------

// ⚠️ Este é o melhor padrão dos dez bancos: o prazo máximo vem **dentro** do
// erro, e reaplica-se. O teste mede as duas coisas — que houve segundo pedido
// com o prazo que o banco indicou, e que a diferença ficou anotada.
func TestPrazoAcimaDoMaximoReaplicaOQueOBancoIndicou(t *testing.T) {
	config := string(captura(t, "configuracoes.resposta.json"))
	respostas := []string{
		string(captura(t, "erro_v159_prazo_por_idade.resposta.json")),
		string(captura(t, "variavel_12m.resposta.json")),
	}
	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			if strings.HasSuffix(p.URL.Path, "/configuracoes") {
				return transporte.RespostaDeTexto(http.StatusOK, config), nil
			}
			corpo := respostas[0]
			estado := http.StatusBadRequest
			if len(respostas) == 1 {
				corpo, estado = respostas[0], http.StatusOK
			} else {
				respostas = respostas[1:]
			}
			return transporte.RespostaDeTexto(estado, corpo), nil
		},
	}

	p := pedidoBase()
	p.PrazoAnos = 40

	oferta, err := novobanco.Novo(falso, nil).Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	corpos := corposDoCalculo(t, falso)
	if len(corpos) != 2 {
		t.Fatalf("esperava dois pedidos ao /calculo — o recusado e o reaplicado —, houve %d", len(corpos))
	}
	if segundo := corpos[1]; segundo.Prazo != 35 {
		t.Errorf("o banco disse que o máximo era 35 anos; repetiu-se com %d", segundo.Prazo)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 {
		t.Fatalf("o prazo mudou de 40 para 35: esperava um ajuste, vieram %+v", ajustes)
	}
	if ajustes[0].Campo() != dominio.AjustadoPrazoAnos {
		t.Errorf("o ajuste é do prazo, veio de %q", ajustes[0].Campo())
	}
	if ajustes[0].De() != 40 || ajustes[0].Para() != 35 {
		t.Errorf("o ajuste tem de dizer de 40 para 35, disse de %v para %v", ajustes[0].De(), ajustes[0].Para())
	}
	if !strings.Contains(ajustes[0].Nota(), "75") {
		t.Errorf("a nota tem de dizer porquê — o contrato termina aos 75: %q", ajustes[0].Nota())
	}
}

// ⚠️ Os dois encolhimentos do prazo — a idade, pelo V159, e o encaixe da fixa
// nos nove valores — encontram-se aqui, e o resultado tem de ser **um** ajuste.
//
// Pede-se uma fixa a 40 anos; o banco responde que o máximo para a idade é 35;
// a fixa não vende 35, e desce a 30. Com um ajuste por cada passo, o segundo
// dizia «Pediu 35 anos» a quem pediu 40 — que é uma frase que ninguém disse. Foi
// esta cadeia que a corrida de fidelidade da CGD apanhou lá.
func TestOsDoisEncolhimentosDoPrazoDaoUmSoAjuste(t *testing.T) {
	config := string(captura(t, "configuracoes.resposta.json"))
	respostas := []string{
		string(captura(t, "erro_v159_prazo_por_idade.resposta.json")),
		string(captura(t, "fixa_30a.resposta.json")),
	}
	estados := []int{http.StatusBadRequest, http.StatusOK}
	i := 0
	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			if strings.HasSuffix(p.URL.Path, "/configuracoes") {
				return transporte.RespostaDeTexto(http.StatusOK, config), nil
			}
			j := min(i, len(respostas)-1)
			i++
			return transporte.RespostaDeTexto(estados[j], respostas[j]), nil
		},
	}

	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = 40

	oferta, err := novobanco.Novo(falso, nil).Simular(t.Context(), p)
	if err != nil {
		t.Fatalf("Simular: %v", err)
	}

	corpos := corposDoCalculo(t, falso)
	if len(corpos) != 2 {
		t.Fatalf("esperava dois pedidos ao /calculo — o recusado e o reaplicado —, houve %d", len(corpos))
	}
	if segundo := corpos[1]; segundo.TipoTaxaIndexante != "FIXA_30_ANOS" || segundo.Prazo != 30 {
		t.Errorf("o banco disse 35 e a fixa só vende 30: esperava FIXA_30_ANOS com prazo 30, foi %q com %d",
			segundo.TipoTaxaIndexante, segundo.Prazo)
	}

	var doPrazo []dominio.Ajuste
	for _, a := range oferta.Ajustes() {
		if a.Campo() == dominio.AjustadoPrazoAnos {
			doPrazo = append(doPrazo, a)
		}
	}
	if len(doPrazo) != 1 {
		t.Fatalf("esperava um só ajuste ao prazo, vieram %d: %+v", len(doPrazo), doPrazo)
	}
	if doPrazo[0].De() != 40 || doPrazo[0].Para() != 30 {
		t.Errorf("o ajuste tem de dizer de 40 para 30 — o que se pediu e o que se simulou —, disse de %v para %v",
			doPrazo[0].De(), doPrazo[0].Para())
	}
	// A frase tem de trazer os dois motivos: a idade e a lista da fixa.
	nota := doPrazo[0].Nota()
	if !strings.Contains(nota, "75") || !strings.Contains(nota, "prazo todo") {
		t.Errorf("a nota tem de explicar os dois encolhimentos (idade e lista da fixa): %q", nota)
	}
}

// Cada código de erro que o banco usa diz uma coisa diferente, e a app responde
// a cada uma de maneira diferente. Traduzi-los todos para "não deu" era perder
// exactamente a informação que este banco dá melhor do que os outros.
func TestCadaCodigoDeErroDizAOQueVem(t *testing.T) {
	casos := []struct {
		captura  string
		codigo   dominio.CodigoErro
		contemNa string
	}{
		{"erro_v126_sem_conta", dominio.ErroProdutoIndisponivel, "conta no banco"},
		{"erro_v158_periodo_nao_cabe", dominio.ErroProdutoIndisponivel, "menor do que o prazo"},
		{"erro_v157_fixa_fora_do_prazo", dominio.ErroProdutoIndisponivel, "10 anos"},
		{"erro_v118_prestacao", dominio.ErroProdutoIndisponivel, "7500"},
		{"erro_generico_periodo", dominio.ErroProdutoIndisponivel, "omc.fwk.genericError"},
	}

	for _, c := range casos {
		t.Run(c.captura, func(t *testing.T) {
			config := string(captura(t, "configuracoes.resposta.json"))
			falso := &transporte.Falso{
				Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
					if strings.HasSuffix(p.URL.Path, "/configuracoes") {
						return transporte.RespostaDeTexto(http.StatusOK, config), nil
					}
					return transporte.RespostaDeTexto(
						http.StatusBadRequest, string(captura(t, c.captura+".resposta.json"))), nil
				},
			}

			_, err := novobanco.Novo(falso, nil).Simular(t.Context(), pedidoBase())

			var erroOferta *dominio.ErroOferta
			if !errors.As(err, &erroOferta) {
				t.Fatalf("esperava um ErroOferta, veio %v", err)
			}
			if erroOferta.Codigo != c.codigo {
				t.Errorf("código: esperava %q, veio %q", c.codigo, erroOferta.Codigo)
			}
			if !strings.Contains(erroOferta.Mensagem, c.contemNa) {
				t.Errorf("a mensagem tem de nomear a coisa (%q): %q", c.contemNa, erroOferta.Mensagem)
			}
		})
	}
}

// --- o que se envia --------------------------------------------------------------

// O payload gerado tem de bater com o capturado. É o passo 5 do
// CONTRATO-BANCO.md §7, e é o que apanha um campo que se deixou de enviar.
func TestOPayloadBateComOCapturado(t *testing.T) {
	casos := []struct {
		nome   string
		pedido dominio.Pedido
	}{
		{"variavel_12m", pedidoDaCaptura()},
		{"variavel_3m", comIndexante(pedidoDaCaptura(), dominio.Euribor3M)},
		{"variavel_arrendamento", comFinalidade(pedidoDaCaptura(), dominio.FinalidadeArrendamento)},
		{"mista_5a", comMista(pedidoDaCaptura(), 5)},
		{"fixa_10a", comFixa(pedidoDaCaptura(), 10)},
		// ⚠️ O par que fixa o outro lado da escolha: sem produtos escolhidos, o
		// campo `bonificacoes` sai `[]`. É a captura que prova que o banco
		// aceita a lista vazia em vez de responder com erro.
		{"variavel_sem_produtos", pedidoBase()},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			banco, falso := montar(t, c.nome)

			if _, err := banco.Simular(t.Context(), c.pedido); err != nil {
				t.Fatalf("Simular: %v", err)
			}

			gerado := corpoEnviado(t, falso)
			esperado := corpoDoPedido(t, captura(t, c.nome+".pedido.json"))

			// Compara-se campo a campo e não o texto: a ordem das chaves de um
			// JSON não é do contrato.
			if diferenca := compararPayloads(esperado, gerado); diferenca != "" {
				t.Errorf("o payload gerado não bate com o capturado:\n%s", diferenca)
			}
		})
	}
}

// ⚠️ Um campo do perfil enviado com valor neutro é um pressuposto, e um
// pressuposto que ninguém vê não existe. Este teste é a guarda do que a §5
// manda: o que se preenche por nós tem de estar declarado nos Requisitos.
func TestOsCamposNeutrosEstaoDeclaradosComNota(t *testing.T) {
	r := novobanco.Novo(nil, nil).Requisitos()
	if err := r.Validar(); err != nil {
		t.Fatalf("requisitos inválidos: %v", err)
	}

	porCampo := make(map[dominio.CampoCanonico]dominio.Input, len(r.Inputs))
	for _, in := range r.Inputs {
		porCampo[in.Campo] = in
	}

	// O v1 declarava requires_occupation=False e enviava profissão fixa. São
	// duas afirmações diferentes, e só uma delas é verdade aqui.
	for _, campo := range []dominio.CampoCanonico{
		dominio.CampoProfissao, dominio.CampoTipologia, dominio.CampoRendimentoMensal,
	} {
		in, existe := porCampo[campo]
		if !existe {
			t.Errorf("%s: o banco envia-o e não o declara", campo)
			continue
		}
		if !in.Usa {
			t.Errorf("%s: é enviado no payload, por isso não pode ir a Usa:false", campo)
		}
		if in.Nota == "" {
			t.Errorf("%s: preenchido por nós com valor neutro e sem nota que o diga", campo)
		}
	}
}

// O indexante é escolhível, e por isso EuriborImposto tem de ficar vazio —
// vazio ali não é "não sei", é "o banco impõe o seu", e dizê-lo aqui seria
// falso.
func TestDeclaraQueDeixaEscolherOIndexante(t *testing.T) {
	r := novobanco.Novo(nil, nil).Requisitos()

	if len(r.EuriborOpcoes) != 3 {
		t.Errorf("o Novo Banco deixa escolher os três tenores, declarou %v", r.EuriborOpcoes)
	}
	if r.EuriborImposto != "" {
		t.Errorf("não impõe nenhum; declarou %q", r.EuriborImposto)
	}
}

// --- o ctx ----------------------------------------------------------------------

// A afirmação que é de cada banco e não do contrato: Simular volta dentro do
// prazo do ctx. Sem o Atraso do transporte falso, o teste passava por não haver
// nada que demorasse.
func TestRespeitaOPrazoDoCtx(t *testing.T) {
	falso := &transporte.Falso{
		Atraso: 5 * time.Second,
		Responder: func(transporte.PedidoGravado) (*http.Response, error) {
			return transporte.RespostaDeTexto(http.StatusOK, string(captura(t, "variavel_12m.resposta.json"))), nil
		},
	}
	prova.RespeitaPrazo(t, novobanco.Novo(falso, nil), pedidoBase(), 300*time.Millisecond)
}

// --- andaimes --------------------------------------------------------------------

func montar(t *testing.T, nomeDaCaptura string) (bancos.Banco, *transporte.Falso) {
	t.Helper()

	config := string(captura(t, "configuracoes.resposta.json"))
	corpo := string(captura(t, nomeDaCaptura+".resposta.json"))
	falso := &transporte.Falso{
		Responder: func(p transporte.PedidoGravado) (*http.Response, error) {
			if strings.HasSuffix(p.URL.Path, "/configuracoes") {
				if canal := p.Cabecalhos.Get("x-nb-oc-channel"); canal == "" {
					t.Error("o pedido ao /configuracoes foi sem o x-nb-oc-channel, e sem ele o banco não responde")
				}
				return transporte.RespostaDeTexto(http.StatusOK, config), nil
			}
			if !strings.Contains(p.URL.Path, "/simulacao/calculo") {
				t.Errorf("pedido a um caminho que o Novo Banco não tem: %q", p.URL.Path)
			}
			if canal := p.Cabecalhos.Get("x-nb-oc-channel"); canal == "" {
				t.Error("o pedido foi sem o x-nb-oc-channel, e sem ele o banco não responde")
			}
			return transporte.RespostaDeTexto(http.StatusOK, corpo), nil
		},
	}
	return novobanco.Novo(falso, nil), falso
}

func captura(t *testing.T, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("capturas", nome))
	if err != nil {
		t.Fatalf("ler a captura %s: %v", nome, err)
	}
	return b
}

// corpoLido é a forma do payload do lado do teste. É deliberadamente uma struct
// à parte da do pacote: assim o teste afirma sobre o JSON que sai, e não sobre
// os tipos internos de quem o produz.
type corpoLido struct {
	Idade             int         `json:"idade"`
	EntradaInicial    json.Number `json:"entradaInicial"`
	ContaNB           bool        `json:"contaNB"`
	Prazo             int         `json:"prazo"`
	TipoTaxa          string      `json:"tipoTaxa"`
	TipoTaxaIndexante string      `json:"tipoTaxaIndexante"`
	RegimeCredito     string      `json:"regimeCredito"`
	ValorAvaliacao    json.Number `json:"valorAvaliacaoTotal"`
	Missao            string      `json:"missao"`
	Bonificacoes      []string    `json:"bonificacoes"`
	Imovel            struct {
		Localizacao     string `json:"localizacao"`
		TipoPropriedade string `json:"tipoPropriedade"`
		Tipologia       string `json:"tipologia"`
	} `json:"imovel"`
	Emprestimos []struct {
		FinalidadeCredito string      `json:"finalidadeCredito"`
		ValorAquisicao    json.Number `json:"valorAquisicao"`
		ValorEmprestimo   json.Number `json:"valorEmprestimo"`
	} `json:"emprestimos"`
	Proponentes []struct {
		NIF              string      `json:"nif"`
		Nacionalidade    string      `json:"nacionalidade"`
		Profissao        string      `json:"profissao"`
		Habilitacoes     string      `json:"habilitacoes"`
		DataNascimento   string      `json:"dataNascimento"`
		RendimentoMensal json.Number `json:"rendimentoMensalLiquido"`
	} `json:"proponentes"`
}

func corpoDoPedido(t *testing.T, bruto []byte) corpoLido {
	t.Helper()
	var c corpoLido
	if err := json.Unmarshal(bruto, &c); err != nil {
		t.Fatalf("ler o payload: %v", err)
	}
	return c
}

// corpoEnviado devolve o corpo do pedido que calcula. Desde o KAN-37 o primeiro
// pedido de cada simulação é o GET /configuracoes, que não tem corpo — procurar
// o /simulacao/calculo é o que diz o que se enviou de facto.
func corpoEnviado(t *testing.T, f *transporte.Falso) corpoLido {
	t.Helper()
	corpos := corposDoCalculo(t, f)
	if len(corpos) == 0 {
		t.Fatal("não houve pedido ao /simulacao/calculo")
	}
	return corpos[0]
}

// corposDoCalculo devolve os corpos dos pedidos ao /simulacao/calculo, pela
// ordem em que foram enviados — é o que sobra depois de o GET /configuracoes
// não ter corpo nem interesse.
func corposDoCalculo(t *testing.T, f *transporte.Falso) []corpoLido {
	t.Helper()
	var corpos []corpoLido
	for _, p := range f.Pedidos() {
		if strings.HasSuffix(p.URL.Path, "/simulacao/calculo") {
			corpos = append(corpos, corpoDoPedido(t, p.Corpo))
		}
	}
	return corpos
}

// compararPayloads devolve as diferenças que interessam, em português.
func compararPayloads(esperado, veio corpoLido) string {
	var falhas []string
	dizer := func(campo string, a, b any) {
		if a != b {
			falhas = append(falhas, "  "+campo+": esperava "+fmt.Sprint(a)+", veio "+fmt.Sprint(b))
		}
	}
	dizer("contaNB", esperado.ContaNB, veio.ContaNB)
	dizer("prazo", esperado.Prazo, veio.Prazo)
	dizer("tipoTaxa", esperado.TipoTaxa, veio.TipoTaxa)
	dizer("tipoTaxaIndexante", esperado.TipoTaxaIndexante, veio.TipoTaxaIndexante)
	dizer("regimeCredito", esperado.RegimeCredito, veio.RegimeCredito)
	dizer("missao", esperado.Missao, veio.Missao)
	dizer("entradaInicial", esperado.EntradaInicial, veio.EntradaInicial)
	dizer("valorAvaliacaoTotal", esperado.ValorAvaliacao, veio.ValorAvaliacao)
	dizer("imovel.localizacao", esperado.Imovel.Localizacao, veio.Imovel.Localizacao)
	dizer("imovel.tipoPropriedade", esperado.Imovel.TipoPropriedade, veio.Imovel.TipoPropriedade)
	dizer("imovel.tipologia", esperado.Imovel.Tipologia, veio.Imovel.Tipologia)
	dizer("nº de bonificações", len(esperado.Bonificacoes), len(veio.Bonificacoes))
	dizer("nº de empréstimos", len(esperado.Emprestimos), len(veio.Emprestimos))
	if len(esperado.Emprestimos) == 1 && len(veio.Emprestimos) == 1 {
		dizer("valorAquisicao", esperado.Emprestimos[0].ValorAquisicao, veio.Emprestimos[0].ValorAquisicao)
		dizer("valorEmprestimo", esperado.Emprestimos[0].ValorEmprestimo, veio.Emprestimos[0].ValorEmprestimo)
		dizer("finalidadeCredito", esperado.Emprestimos[0].FinalidadeCredito, veio.Emprestimos[0].FinalidadeCredito)
	}
	dizer("nº de proponentes", len(esperado.Proponentes), len(veio.Proponentes))
	if len(esperado.Proponentes) >= 1 && len(veio.Proponentes) >= 1 {
		dizer("proponente.dataNascimento", esperado.Proponentes[0].DataNascimento, veio.Proponentes[0].DataNascimento)
		dizer("proponente.profissao", esperado.Proponentes[0].Profissao, veio.Proponentes[0].Profissao)
		dizer("proponente.habilitacoes", esperado.Proponentes[0].Habilitacoes, veio.Proponentes[0].Habilitacoes)
		dizer("proponente.nacionalidade", esperado.Proponentes[0].Nacionalidade, veio.Proponentes[0].Nacionalidade)
		dizer("proponente.rendimentoMensalLiquido", esperado.Proponentes[0].RendimentoMensal, veio.Proponentes[0].RendimentoMensal)
	}
	return strings.Join(falhas, "\n")
}

func algumaNotaContem(o dominio.Oferta, texto string) bool {
	for _, n := range o.Notas() {
		if strings.Contains(n, texto) {
			return true
		}
	}
	return false
}

// pedidoBase é o cenário de referência: 250 000 € de imóvel, 200 000 €
// financiados, 30 anos, habitação própria.
func pedidoBase() dominio.Pedido {
	return dominio.Pedido{
		ValorImovel: dominio.DinheiroDeInteiro(250_000),
		Montante:    dominio.DinheiroDeInteiro(200_000),
		PrazoAnos:   30,
		TipoTaxa:    dominio.TaxaVariavel,
		Finalidade:  dominio.FinalidadePropria,
		Localizacao: dominio.LocalizacaoContinente,
		Titulares:   []dominio.Titular{{DataNascimento: nascidoEm(1990, 1, 15), RendimentoMensal: dominio.DinheiroDeInteiro(2000)}},
	}
}

// pedidoDaCaptura é o pedidoBase com a data de nascimento exacta com que as
// capturas foram gravadas — é o que permite comparar o payload gerado com o
// gravado sem que o relógio o desfaça.
//
// ⚠️ Leva as duas bonificações porque as capturas foram gravadas com elas
// ligadas (a única excepção é a `variavel_sem_produtos`). Isso é um facto sobre
// as capturas e não a omissão do banco: quem não escolhe nada leva `[]`.
func pedidoDaCaptura() dominio.Pedido { return comAsBonificacoes(pedidoBase()) }

// comAsBonificacoes escolhe as duas bonificações do Novo Banco.
func comAsBonificacoes(p dominio.Pedido) dominio.Pedido {
	p.Produtos = append(p.Produtos, novobanco.ProdutoPrimeiroBanco, novobanco.ProdutoProtecao)
	return p
}

func comIndexante(p dominio.Pedido, i dominio.Indexante) dominio.Pedido {
	p.Indexante = i
	return p
}

func comFinalidade(p dominio.Pedido, f dominio.Finalidade) dominio.Pedido {
	p.Finalidade = f
	return p
}

func comMista(p dominio.Pedido, periodo int) dominio.Pedido {
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = anos(periodo)
	return p
}

func comFixa(p dominio.Pedido, prazo int) dominio.Pedido {
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = prazo
	return p
}

func anos(n int) *int { return &n }

func nascidoEm(ano int, mes time.Month, dia int) dominio.Data {
	d, err := dominio.DataDe(ano, mes, dia)
	if err != nil {
		panic(err)
	}
	return d
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
