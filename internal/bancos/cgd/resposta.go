package cgd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Este ficheiro é PURO: dados para dados, sem rede, sem relógio, sem base de
// dados. É o que o torna testável contra as capturas de `capturas/`, offline.

// --- números -----------------------------------------------------------------

// espacos são os separadores de milhares que a CGD usa. Medido a 2026-07-26:
// o corpo traz `361 670,15` — espaço não-quebrável em UTF-8 limpo
// (bytes C2 A0), e não dupla codificação. O resto da lista é defesa barata
// contra a variante fina, que outros simuladores usam.
var espacos = strings.NewReplacer(
	" ", "", // não-quebrável
	" ", "", // não-quebrável fino
	" ", "", // fino
	" ", "",
)

// numeroPT lê o formato português da CGD: vírgula decimal e espaço a separar os
// milhares.
//
// ⚠️ Nunca passa por float64. O ARQUITETURA.md §4 exige decimal em dinheiro e
// taxas, e converter aqui para float e voltar seria introduzir o erro
// exactamente na fronteira que o decimal existe para proteger.
func numeroPT(s string) (decimal.Decimal, error) {
	limpo := espacos.Replace(s)
	if limpo == "" {
		return decimal.Zero, fmt.Errorf("número vazio")
	}
	// Com vírgula decimal, um ponto só pode ser separador de milhares.
	if strings.Contains(limpo, ",") {
		limpo = strings.ReplaceAll(limpo, ".", "")
		limpo = strings.ReplaceAll(limpo, ",", ".")
	}
	d, err := decimal.NewFromString(limpo)
	if err != nil {
		return decimal.Zero, fmt.Errorf("número em formato português inválido %q: %w", s, err)
	}
	return d, nil
}

// taxaPT e dinheiroPT lêem um campo que pode não vir. Nulo à entrada é nulo à
// saída, e não zero: a CGD deixa a nulo o que não se aplica — a fase variável
// de uma taxa fixa, por exemplo — e um zero aí seria um número inventado.
func taxaPT(s *string) (*dominio.Taxa, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	d, err := numeroPT(*s)
	if err != nil {
		return nil, err
	}
	t := dominio.TaxaDeDecimal(d)
	return &t, nil
}

func dinheiroPT(s *string) (*dominio.Dinheiro, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	d, err := numeroPT(*s)
	if err != nil {
		return nil, err
	}
	m := dominio.DinheiroDeDecimal(d)
	return &m, nil
}

// --- /calculate --------------------------------------------------------------

// respostaCalculate é o corpo do POST /calculate.
//
// Os números vêm como texto em formato português — por isso *string e não
// decimal: a conversão é nossa, e é o numeroPT que a faz.
type respostaCalculate struct {
	Success bool `json:"success"`
	Data    *struct {
		BaseResult       *resultado      `json:"BaseResult"`
		DiscountedResult *resultado      `json:"DiscountedResult"`
		Fees             json.RawMessage `json:"Fees"`
	} `json:"data"`
}

// resultado é uma das duas variantes de preço que vêm na mesma resposta:
// BaseResult é o preçário, DiscountedResult é o mesmo com os packs.
type resultado struct {
	TotalDuration       int `json:"TotalDuration"`
	TotalDurationMonths int `json:"TotalDurationMonths"`

	AnualNominalRate   *string `json:"AnualNominalRate"`
	Spread             *string `json:"Spread"`
	APR                *string `json:"APR"`
	Instalment         *string `json:"Instalment"`
	TotalPayableAmount *string `json:"TotalPayableAmount"`

	FixedDurationMonths       int     `json:"FixedDurationMonths"`
	FixedAnualNominalRate     *string `json:"FixedAnualNominalRate"`
	FixedInstalment           *string `json:"FixedInstalment"`
	FixedIndexRateDescription *string `json:"FixedIndexRateDescription"`

	VariableDurationMonths   int     `json:"VariableDurationMonths"`
	VariableAnualNominalRate *string `json:"VariableAnualNominalRate"`
	VariableInstalment       *string `json:"VariableInstalment"`

	// ⚠️ VariableIndexRate é a Euribor, e IndexValue não é. Medido a
	// 2026-07-26: numa mista, IndexValue traz 3,000 — que é a taxa base da
	// fase fixa — e VariableIndexRate traz 2,596, que é a Euribor 6M. Ler o
	// IndexValue como indexante daria, numa fixa, uma "Euribor" de 3,5 % que
	// não existe em lado nenhum.
	VariableIndexRate *string `json:"VariableIndexRate"`

	IndexValue *string `json:"IndexValue"`
	IndexType  *string `json:"IndexType"`
}

// lerResposta transforma o corpo do /calculate numa Oferta.
//
// comPacks escolhe a variante: o preçário base, ou o mesmo com os packs de
// vinculação. Por omissão é o base — os packs exigem deter os produtos.
func lerResposta(corpo []byte, comPacks bool) (dominio.Oferta, error) {
	var r respostaCalculate
	if err := json.Unmarshal(corpo, &r); err != nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: fmt.Sprintf("A CGD respondeu algo que não é JSON: %v", err),
		}
	}

	// ⚠️ Uma recusa da CGD é literalmente `{"success":false}` — sem código, sem
	// mensagem, sem campo nenhum que diga o que correu mal. Medido a 2026-07-26
	// com prazo de 45 anos, montante abaixo do mínimo e código de período
	// inválido: os três dão exactamente estes 17 bytes. Por isso é que os
	// limites se verificam antes, contra o /limits: depois da recusa não há
	// nada para explicar a ninguém.
	if !r.Success || r.Data == nil || r.Data.BaseResult == nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: "A CGD recusou esta simulação e não disse porquê.",
		}
	}

	escolhido := r.Data.BaseResult
	if comPacks && r.Data.DiscountedResult != nil {
		escolhido = r.Data.DiscountedResult
	}

	oferta, err := ofertaDeResultado(escolhido)
	if err != nil {
		return dominio.Oferta{}, err
	}
	if comPacks && r.Data.DiscountedResult != nil {
		oferta.ProdutosAplicados = []string{ProdutoPacks}
	}
	anotarDescontoDosPacks(&oferta, r.Data.BaseResult, r.Data.DiscountedResult, comPacks)
	return oferta, nil
}

// ofertaDeResultado lê uma das variantes.
func ofertaDeResultado(res *resultado) (dominio.Oferta, error) {
	var o dominio.Oferta
	var err error

	if o.TAN, err = taxaPT(res.AnualNominalRate); err != nil {
		return o, ilegivel("TAN", err)
	}
	if o.TAEG, err = taxaPT(res.APR); err != nil {
		return o, ilegivel("TAEG", err)
	}
	if o.Spread, err = taxaPT(res.Spread); err != nil {
		return o, ilegivel("spread", err)
	}
	if o.Prestacao, err = dinheiroPT(res.Instalment); err != nil {
		return o, ilegivel("prestação", err)
	}
	if o.MTIC, err = dinheiroPT(res.TotalPayableAmount); err != nil {
		return o, ilegivel("MTIC", err)
	}
	if o.EuriborValor, err = taxaPT(res.VariableIndexRate); err != nil {
		return o, ilegivel("valor da Euribor", err)
	}
	// A Euribor só se declara quando há fase indexada. Numa taxa fixa a CGD
	// deixa o VariableIndexRate a nulo, e dizer "6m" aí seria declarar um
	// indexante que este contrato não tem.
	if o.EuriborValor != nil {
		o.Indexante = EuriborImposta
	}

	if o.Fases, err = lerFases(res); err != nil {
		return o, err
	}
	// ⚠️ A validação é contra o prazo que a CGD diz ter aplicado, e não contra
	// o pedido: na taxa fixa ela ignora o campo Years e o prazo é o do código
	// do período (medido a 2026-07-26). Validar contra o pedido reprovaria uma
	// resposta correcta e escondia justamente o ajuste que interessa ver.
	if err := dominio.ValidarFases(o.Fases, res.TotalDuration); err != nil {
		return o, ilegivel("plano de fases", err)
	}
	return o, nil
}

// lerFases monta o plano a partir das duas fases que a CGD publica.
//
// Uma fase com zero meses não existe: a variável traz só a indexada, a fixa só
// a fixa, e a mista traz as duas — sempre a fixa primeiro.
func lerFases(res *resultado) ([]dominio.Fase, error) {
	duracoes := make([]dominio.FaseDuracao, 0, 2)

	if res.FixedDurationMonths > 0 {
		taxa, err := taxaPT(res.FixedAnualNominalRate)
		if err != nil {
			return nil, ilegivel("TAN da fase fixa", err)
		}
		prestacao, err := dinheiroPT(res.FixedInstalment)
		if err != nil {
			return nil, ilegivel("prestação da fase fixa", err)
		}
		if taxa == nil || prestacao == nil {
			return nil, ilegivel("fase fixa", fmt.Errorf("%d meses sem taxa ou sem prestação", res.FixedDurationMonths))
		}
		duracoes = append(duracoes, dominio.FaseDuracao{
			Meses: res.FixedDurationMonths, Taxa: *taxa, Prestacao: *prestacao,
		})
	}

	if res.VariableDurationMonths > 0 {
		taxa, err := taxaPT(res.VariableAnualNominalRate)
		if err != nil {
			return nil, ilegivel("TAN da fase indexada", err)
		}
		prestacao, err := dinheiroPT(res.VariableInstalment)
		if err != nil {
			return nil, ilegivel("prestação da fase indexada", err)
		}
		if taxa == nil || prestacao == nil {
			return nil, ilegivel("fase indexada", fmt.Errorf("%d meses sem taxa ou sem prestação", res.VariableDurationMonths))
		}
		duracoes = append(duracoes, dominio.FaseDuracao{
			Meses: res.VariableDurationMonths, Taxa: *taxa, Prestacao: *prestacao,
		})
	}

	fases, err := dominio.FasesDeDuracoes(duracoes)
	if err != nil {
		return nil, ilegivel("plano de fases", err)
	}
	return fases, nil
}

// anotarDescontoDosPacks diz, em números, o que os packs valem — ou o que
// custou não os ter.
//
// ⚠️ As duas variantes vêm sempre na mesma resposta, e por isso a nota sai nos
// dois sentidos: quem escolheu os packs lê quanto lhe valem, quem não os
// escolheu lê quanto lhe custa não os ter. Nenhum dos dois é silêncio —
// esconder a coluna do lado era esconder que existe.
func anotarDescontoDosPacks(o *dominio.Oferta, base, comDesconto *resultado, aplicados bool) {
	if base == nil || comDesconto == nil {
		return
	}
	spreadBase, err := taxaPT(base.Spread)
	if err != nil || spreadBase == nil {
		return
	}
	spreadDesconto, err := taxaPT(comDesconto.Spread)
	if err != nil || spreadDesconto == nil {
		return
	}
	diferenca := spreadBase.Sub(*spreadDesconto)
	if diferenca.Decimal().IsZero() {
		return
	}
	if aplicados {
		o.Anotar(fmt.Sprintf(
			"Este spread já desconta %s p.p. dos packs CGD (Vinculação, Ligação e Proteção). Sem eles, o spread é de %s p.p.",
			diferenca, spreadBase))
		return
	}
	o.Anotar(fmt.Sprintf(
		"Com os packs CGD (Vinculação, Ligação e Proteção) o spread desceria %s p.p., para %s p.p. Exigem deter esses produtos.",
		diferenca, spreadDesconto))
}

func ilegivel(oQue string, err error) error {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroRespostaIlegivel,
		Mensagem: fmt.Sprintf("Não se conseguiu ler %s da resposta da CGD: %v", oQue, err),
	}
}

// --- /limits -----------------------------------------------------------------

// limites é o que o POST /limits devolve sobre elegibilidade. Só os campos que
// se usam: o resto da resposta é a lista de códigos de indexante, que não se lê
// daqui (ver periodosDoHTML).
type limites struct {
	LTVMinimo        float64 `json:"ltvMinimum"`
	LTVMaximo        float64 `json:"ltvMaximum"`
	MontanteMinimo   float64 `json:"loanAmountMinimum"`
	MontanteMaximo   float64 `json:"loanAmountMaximum"`
	ValorImovelMin   float64 `json:"propertyValueMinimum"`
	PrazoMinimo      int     `json:"loanDurationMinimum"`
	PrazoMaximo      int     `json:"loanDurationMaximum"`
	IdadeMaximaAoFim int     `json:"ageMaximumAtLoanMaturity"`
}

// lerLimites lê a resposta do /limits.
//
// ⚠️ Estes limites mudam com a finalidade e com a Medida Jovem, e é por isso
// que se pedem a cada simulação em vez de virem de constantes nossas. Medido a
// 2026-07-26: própria dá LTV ≤ 90 %, prazo ≤ 40 e idade ao fim 70; secundária e
// arrendamento dão LTV ≤ 80 %, prazo ≤ 30 e idade ao fim 75; e a Medida Jovem
// sobrepõe-se às duas — LTV entre 85 % e 100 %, prazo ≤ 40, montante ≤ 450 000 €
// — mesmo em arrendamento. Escrever isto como constantes era ficar com números
// que envelhecem em silêncio.
func lerLimites(corpo []byte) (limites, error) {
	var l limites
	if err := json.Unmarshal(corpo, &l); err != nil {
		return limites{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: fmt.Sprintf("Os limites da CGD não vieram em JSON: %v", err),
		}
	}
	if l.PrazoMaximo <= 0 || l.LTVMaximo <= 0 {
		return limites{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: "Os limites da CGD vieram sem prazo máximo nem LTV máximo.",
		}
	}
	return l, nil
}

// --- os períodos, do HTML ----------------------------------------------------

// A CGD publica os períodos válidos em dois widgets Kendo do HTML da página, e
// não numa API. São duas listas diferentes, e essa é a descoberta que obriga a
// ler as duas:
//
//   - #IndexFixedRateView  — taxa fixa: TODOS os anos de 5 a 40, um a um
//   - #IndexRateMixedFixed — taxa mista: só {5,10,15,20,25,30,35}
//
// ⚠️ Medido a 2026-07-26. O v1 lia só a da mista e usava-a também na fixa, o que
// encaixava uma fixa de 12 anos em 10 — quando a CGD vende os 12.
//
// ⚠️ E não se lêem do /limits, apesar de ele trazer uma lista de indexTypes bem
// maior. É a lição do Banco CTT: uma API aceitar não é o banco vender. O que o
// simulador mostra é o que a CGD comercializa.
var (
	dropdown = regexp.MustCompile(`jQuery\("#(\w+)"\)\.kendoDropDownList\((\{[\s\S]*?\})\);`)
	opcaoAno = regexp.MustCompile(`\{"Text":"Taxa base (\d+) anos?","Value":"(\d+)"`)
)

const (
	widgetFixa  = "IndexFixedRateView"
	widgetMista = "IndexRateMixedFixed"
)

// codigos mapeia anos → código que o formulário espera.
type codigos map[int]string

// anos devolve os anos disponíveis, para o EncaixarPeriodoFixo do domínio.
func (c codigos) anos() []int {
	as := make([]int, 0, len(c))
	for a := range c {
		as = append(as, a)
	}
	return as
}

// periodosDoHTML extrai os dois catálogos de períodos do HTML da homepage.
func periodosDoHTML(html []byte) (fixa, mista codigos, err error) {
	fixa, mista = codigos{}, codigos{}
	for _, m := range dropdown.FindAllSubmatch(html, -1) {
		nome := string(m[1])
		if nome != widgetFixa && nome != widgetMista {
			continue
		}
		destino := fixa
		if nome == widgetMista {
			destino = mista
		}
		for _, o := range opcaoAno.FindAllSubmatch(m[2], -1) {
			ano, convErr := strconv.Atoi(string(o[1]))
			if convErr != nil {
				continue
			}
			destino[ano] = string(o[2])
		}
	}
	if len(fixa) == 0 || len(mista) == 0 {
		return nil, nil, fmt.Errorf(
			"os períodos não estão no HTML da CGD (fixa: %d, mista: %d) — o widget mudou",
			len(fixa), len(mista))
	}
	return fixa, mista, nil
}
