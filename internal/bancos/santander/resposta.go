package santander

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O Santander devolve as DUAS colunas de preço no mesmo corpo — bonificado e não
// bonificado — como a CGD e o Banco CTT. Um pedido dá as duas linhas do
// varrimento, e é por isso que o produto deste banco é «de leitura» e não «de
// pedido» (ARQUITETURA.md §4).
//
// ⚠️ E traz o plano por TROÇOS (`installments`), cada um com o seu indexante, o
// seu spread e a sua TAN. É a resposta mais rica dos cinco bancos escritos, e é
// ela que expõe a armadilha deste banco.
//
// ⚠️ **O spread promocional é TEMPORÁRIO.** Medido a 2026-07-28: no plano
// bonificado de uma taxa VARIÁVEL, o spread é 0,5 nos primeiros 36 meses e 0,8 a
// partir do mês 37 — sobre o mesmo indexante (6EM). O catálogo `/rates` anuncia
// «SPREAD PROMOCIONAL 0,5%», e publicar isso como o spread do contrato seria
// publicar um preço que dura três anos de trinta.
//
// ⚠️ **E na taxa FIXA o plano bonificado é igual ao não bonificado** — TAEG 5,1 e
// MTIC 384 541,08 nos dois, medido no mesmo dia. Escolher a coluna bonificada
// numa fixa não desconta nada, e tratar o produto como desconto universal daria
// um número que o banco não pratica.

const (
	IDBanco   = "santander"
	NomeBanco = "Santander"

	// ProdutoBonificado é o plano com domiciliação e seguros — a coluna
	// `bonifiedPlanDetails` da resposta.
	ProdutoBonificado = "santander:bonificado"
)

// indexanteEuribor6M é o código com que o Santander nomeia a Euribor a 6 meses.
//
// ⚠️ É o ÚNICO código da resposta que é uma Euribor. Os outros — `M28`, `290`,
// `DOZ`, `P10`, `P20`, `P30` — são taxas fixas do próprio banco, e lê-los como
// indexante publicaria uma Euribor que não existe.
const indexanteEuribor6M = "6EM"

// resposta é a lista que o /get_by_rates devolve: uma entrada por taxa pedida.
type resposta []entrada

type entrada struct {
	SimulationRate    string   `json:"simulationRate"`
	Indexantes        []string `json:"indexantes"`
	LoanDurationYears int      `json:"loanDurationYears"`
	IsValid           bool     `json:"isValid"`

	BonifiedPlanDetails    *plano `json:"bonifiedPlanDetails"`
	NonBonifiedPlanDetails *plano `json:"nonBonifiedPlanDetails"`
}

type plano struct {
	Installments []troco     `json:"installments"`
	TAEG         json.Number `json:"taeg"`
	MTIC         json.Number `json:"mtic"`
}

// troco é um período do plano. ⚠️ `FirstMonth` é o mês em que COMEÇA, e as fases
// do domínio guardam o mês em que ACABAM — a conversão é o `fasesDe`.
type troco struct {
	FirstMonth         int         `json:"firstMonth"`
	MonthlyPayment     json.Number `json:"monthlyPayment"`
	ReferenceRateCode  string      `json:"referenceRateCode"`
	ReferenceRateValue json.Number `json:"referenceRateValue"`
	Spread             json.Number `json:"spread"`
	TAN                json.Number `json:"tan"`
}

// lerResposta traduz o corpo do /get_by_rates numa oferta.
//
// É pura: dados para dados, sem rede e sem relógio. Recebe o pedido porque é ele
// que diz que produtos foram escolhidos — e a escolha da coluna é de leitura.
func lerResposta(corpo []byte, p dominio.Pedido) (dominio.Oferta, error) {
	var r resposta
	if err := json.Unmarshal(corpo, &r); err != nil {
		return dominio.Oferta{}, ilegivel("o corpo", err)
	}

	valida, err := primeiraValida(r)
	if err != nil {
		return dominio.Oferta{}, err
	}

	comProdutos := len(p.ProdutosDoBanco(IDBanco)) > 0
	escolhido := valida.NonBonifiedPlanDetails
	if comProdutos {
		escolhido = valida.BonifiedPlanDetails
	}
	if escolhido == nil || len(escolhido.Installments) == 0 {
		return dominio.Oferta{}, ilegivel("o plano de prestações",
			fmt.Errorf("a entrada válida não trouxe o plano %s", nomeDoPlano(comProdutos)))
	}

	var o dominio.Oferta
	if o.TAEG, err = numeroComoTaxa(escolhido.TAEG); err != nil {
		return o, ilegivel("a TAEG", err)
	}
	if o.MTIC, err = numeroComoDinheiro(escolhido.MTIC); err != nil {
		return o, ilegivel("o MTIC", err)
	}

	fases, err := fasesDe(escolhido.Installments, valida.LoanDurationYears)
	if err != nil {
		return o, ilegivel("o plano de fases", err)
	}
	o.Fases = fases
	if err := dominio.ValidarFases(fases, valida.LoanDurationYears); err != nil {
		return o, ilegivel("o plano de fases", err)
	}

	// A TAN e a prestação de topo são as da PRIMEIRA fase — o que o cliente
	// começa a pagar.
	primeira := fases[0]
	o.TAN, o.Prestacao = &primeira.Taxa, &primeira.Prestacao

	if err := lerIndexado(&o, escolhido.Installments); err != nil {
		return o, err
	}

	o.ProdutosAplicados = p.ProdutosDoBanco(IDBanco)
	anotarPromocional(&o, escolhido.Installments)
	anotarBonificacaoInutil(&o, valida, comProdutos)
	return o, nil
}

// primeiraValida encontra a entrada que o banco marcou como simulável.
//
// ⚠️ Uma entrada com `isValid: false` não é meia resposta: é o banco a dizer que
// não faz aquela combinação. Lê-la na mesma daria números que ele não pratica.
func primeiraValida(r resposta) (entrada, error) {
	if len(r) == 0 {
		return entrada{}, recusa("o Santander não devolveu simulação nenhuma para este pedido")
	}
	for _, e := range r {
		if e.IsValid {
			return e, nil
		}
	}
	return entrada{}, recusa("o Santander marcou todas as taxas pedidas como não simuláveis para este pedido")
}

// lerIndexado preenche o spread, o indexante e o valor da Euribor — ou deixa-os
// nulos numa fixa pura.
//
// ⚠️ **O spread publicado é o da ÚLTIMA fase indexada**, e não o da primeira.
// Numa variável com promocional, a primeira traz 0,5 durante 36 meses e a última
// 0,8 durante os 324 restantes; publicar 0,5 seria anunciar o preço de um
// décimo do contrato. Numa mista, a última fase indexada é a que vem depois do
// período fixo, que é o spread contratual — a mesma regra dá a resposta certa
// nas duas, e é por isso que é uma regra e não dois casos.
func lerIndexado(o *dominio.Oferta, trocos []troco) error {
	ultimo := -1
	for i, t := range trocos {
		if t.ReferenceRateCode == indexanteEuribor6M {
			ultimo = i
		}
	}
	if ultimo < 0 {
		// Fixa pura: não há fase indexada, e publicar spread ou Euribor seria
		// inventar. É a mesma leitura que o Banco CTT obriga a fazer.
		return nil
	}

	t := trocos[ultimo]
	spread, err := numeroComoTaxa(t.Spread)
	if err != nil {
		return ilegivel("o spread", err)
	}
	euribor, err := numeroComoTaxa(t.ReferenceRateValue)
	if err != nil {
		return ilegivel("o valor da Euribor", err)
	}
	o.Spread, o.EuriborValor, o.Indexante = spread, euribor, dominio.Euribor6M
	return nil
}

// fasesDe converte os troços do banco no plano do domínio.
//
// ⚠️ O banco diz onde cada troço COMEÇA e o domínio guarda onde ACABA. O último
// acaba no fim do contrato, e é o `loanDurationYears` que o diz — não se assume
// o prazo pedido, porque o Santander encurta-o quando a idade não deixa (está no
// próprio corpo, e o v1 registou-o).
func fasesDe(trocos []troco, prazoAnos int) ([]dominio.Fase, error) {
	if prazoAnos < 1 {
		return nil, fmt.Errorf("o banco devolveu %d anos de prazo", prazoAnos)
	}
	prazoMeses := prazoAnos * 12

	fases := make([]dominio.Fase, 0, len(trocos))
	for i, t := range trocos {
		if t.FirstMonth < 1 {
			return nil, fmt.Errorf("o troço %d começa no mês %d", i+1, t.FirstMonth)
		}
		ate := prazoMeses
		if i+1 < len(trocos) {
			ate = trocos[i+1].FirstMonth - 1
		}
		taxa, err := numeroComoTaxa(t.TAN)
		if err != nil || taxa == nil {
			return nil, fmt.Errorf("a TAN do troço %d: %w", i+1, err)
		}
		prestacao, err := numeroComoDinheiro(t.MonthlyPayment)
		if err != nil || prestacao == nil {
			return nil, fmt.Errorf("a prestação do troço %d: %w", i+1, err)
		}
		fases = append(fases, dominio.Fase{AteMes: ate, Taxa: *taxa, Prestacao: *prestacao})
	}
	return fases, nil
}

// anotarPromocional diz que o spread dos primeiros meses não é o do contrato.
//
// ⚠️ É a nota que impede a armadilha de se tornar engano. O preço que a pessoa vê
// primeiro é o promocional, e o que ela vai pagar durante a maior parte do
// contrato é o outro — dizer os dois, com os meses, é o que a §5 chama declarar.
func anotarPromocional(o *dominio.Oferta, trocos []troco) {
	var indexados []troco
	for _, t := range trocos {
		if t.ReferenceRateCode == indexanteEuribor6M {
			indexados = append(indexados, t)
		}
	}
	if len(indexados) < 2 {
		return
	}
	primeiro, ultimo := indexados[0], indexados[len(indexados)-1]
	if primeiro.Spread.String() == ultimo.Spread.String() {
		return
	}

	meses := ultimo.FirstMonth - primeiro.FirstMonth
	o.Anotar(fmt.Sprintf(
		"O spread do Santander sobe de %s para %s p.p. ao fim de %d meses. O valor publicado é o "+
			"segundo — é o que vigora na maior parte do contrato; a prestação dos primeiros %d meses "+
			"usa o promocional e é mais baixa.",
		primeiro.Spread, ultimo.Spread, meses, meses))
}

// anotarBonificacaoInutil avisa quando o produto escolhido não desconta nada.
//
// ⚠️ Medido na taxa fixa a 2026-07-28: os dois planos vêm iguais ao cêntimo.
// Deixar passar em silêncio fazia a pessoa acreditar que domiciliar o ordenado
// lhe valeu alguma coisa naquele produto.
func anotarBonificacaoInutil(o *dominio.Oferta, e entrada, comProdutos bool) {
	if !comProdutos || e.BonifiedPlanDetails == nil || e.NonBonifiedPlanDetails == nil {
		return
	}
	if e.BonifiedPlanDetails.MTIC.String() != e.NonBonifiedPlanDetails.MTIC.String() {
		return
	}
	o.Anotar("Neste produto o plano bonificado do Santander custa exactamente o mesmo que o não " +
		"bonificado: as condições que escolheu não descontam nada aqui.")
}

func nomeDoPlano(comProdutos bool) string {
	if comProdutos {
		return "bonificado"
	}
	return "não bonificado"
}

// recusa traduz um corpo que não trouxe simulação.
func recusa(porque string) *dominio.ErroOferta {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroProdutoIndisponivel,
		Mensagem: strings.ToUpper(porque[:1]) + porque[1:] + ".",
	}
}

func ilegivel(oQue string, err error) *dominio.ErroOferta {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroRespostaIlegivel,
		Mensagem: fmt.Sprintf("Não se conseguiu ler %s da resposta do Santander: %v", oQue, err),
	}
}

// numeroComoTaxa lê um número JSON como taxa, pelos dígitos e nunca por float64.
func numeroComoTaxa(n json.Number) (*dominio.Taxa, error) {
	d, err := decimalDe(n)
	if err != nil || d == nil {
		return nil, err
	}
	t := dominio.TaxaDeDecimal(*d)
	return &t, nil
}

func numeroComoDinheiro(n json.Number) (*dominio.Dinheiro, error) {
	d, err := decimalDe(n)
	if err != nil || d == nil {
		return nil, err
	}
	m := dominio.DinheiroDeDecimal(*d)
	return &m, nil
}

func decimalDe(n json.Number) (*decimal.Decimal, error) {
	texto := strings.TrimSpace(n.String())
	if texto == "" {
		return nil, nil
	}
	d, err := decimal.NewFromString(texto)
	if err != nil {
		return nil, fmt.Errorf("número inválido %q: %w", texto, err)
	}
	return &d, nil
}
