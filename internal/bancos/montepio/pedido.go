package montepio

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O payload do Montepio é JSON, e o campo que manda é o `ConditionCode` — uma
// string composta que carrega a finalidade, o tenor da Euribor e a modalidade
// de taxa.
//
// ⚠️ Este ficheiro é **puro**: dados para dados, sem rede e sem relógio. A idade
// e a tabela de escalões entram por parâmetro — a primeira porque o relógio vive
// no montepio.go, a segunda porque vem do HTML do arranque.

// Os períodos de taxa fixa que o banco pratica, em anos. Medidos um a um a
// 2026-07-27: 1, 3, 12, 20 e 35 são recusados, e a recusa vem **sem mensagem
// nenhuma** (`Message` vazia, `Code` nulo) — ver a captura
// `erro_periodo_inexistente`.
var periodosValidos = []int{2, 5, 7, 10, 15, 25, 30}

// payload é o corpo que o gateway espera.
//
// ⚠️ Os montantes vão como json.Number e não como float64, pela mesma razão da
// leitura: json.Number marshala o literal tal como está.
type payload struct {
	CCRDCalculateInput           struct{} `json:"CCRDCalculateInput"`
	MultifunctionsCalculateInput struct{} `json:"MultifunctionsCalculateInput"`

	ConditionCode         string `json:"ConditionCode"`
	CreditDestinationCode string `json:"CreditDestinationCode"`
	FamilyCode            string `json:"FamilyCode"`
	ProductCode           string `json:"ProductCode"`
	FrequencyTypeCode     string `json:"FrequencyTypeCode"`

	AcquisitionAmount json.Number `json:"AcquisitionAmount"`
	// Ammount é o montante financiado. ⚠️ Os dois «m» são literais da API do
	// banco. Não corrigir: com `Amount` o gateway não reconhece o campo.
	Ammount          json.Number `json:"Ammount"`
	Term             int         `json:"Term"` // ⚠️ em MESES, não em anos.
	EvaluationAmount json.Number `json:"EvaluationAmount"`

	Device     dispositivo  `json:"Device"`
	DistrictID int          `json:"DistrictID"`
	Proponents []proponente `json:"Proponents"`

	OptionalExpenses []despesa `json:"OptionalExpenses"`

	// Counterparts é o número de produtos detidos, de 0 a 4.
	Counterparts int `json:"Counterparts"`
}

type dispositivo struct {
	Browser        string `json:"Browser"`
	BrowserVersion string `json:"BrowserVersion"`
	Os             string `json:"Os"`
	OsVersion      string `json:"OsVersion"`
	UserAgent      string `json:"UserAgent"`
	Device         string `json:"Device"`
}

// proponente é um titular. ⚠️ Só a data de nascimento é dele: o simulador do
// Montepio não pede rendimento, profissão nem vínculo, e o payload não os leva.
type proponente struct {
	Birthday      string         `json:"Birthday"`
	Position      int            `json:"Position"`
	State         bool           `json:"State"`
	EntityType    tipoDeEntidade `json:"EntityType"`
	ExpenseCodes  []string       `json:"ExpenseCodes"`
	AccountNumber string         `json:"AccountNumber"`
}

type tipoDeEntidade struct {
	Code        string `json:"Code"`
	CompanyID   int    `json:"CompanyID"`
	Description string `json:"Description"`
	ID          int    `json:"ID"`
	State       bool   `json:"State"`
}

type despesa struct {
	Code   string `json:"Code"`
	Factor int    `json:"Factor"`
}

// Os valores que o simulador envia e que não são escolha de ninguém.
const (
	familiaMultiopcoes = "MT"
	frequenciaMensal   = "M"

	// distritoNeutro: o simulador pede o distrito e ele não mexe no preço.
	distritoNeutro = 6

	// seguroVida e seguroMultirriscos são as despesas opcionais que o simulador
	// traz ligadas. Saem nos encargos, não na prestação.
	seguroVida         = "008"
	seguroMultirriscos = "012"

	contaNeutra = "4"
)

// produtoDe é o ProductCode por finalidade — os três nativos do simulador.
//
// ⚠️ Medido a 2026-07-27: a primeira e a segunda habitação dão o mesmo preço ao
// cêntimo; o arrendamento dá a mesma TAN e o mesmo spread, e uma prestação mais
// alta por levar o Imposto do Selo lá dentro (ver anotarImpostoDoSelo).
func produtoDe(f dominio.Finalidade) string {
	switch f {
	case dominio.FinalidadeSecundaria:
		return "23"
	case dominio.FinalidadeArrendamento:
		return "24"
	default:
		return "21"
	}
}

// familiaDe é o par operação+tenor que escolhe o indexante: H0 = 12M, H5 = 6M,
// H9 = 3M.
func familiaDe(i dominio.Indexante) string {
	switch i {
	case dominio.Euribor6M:
		return "H5"
	case dominio.Euribor12M:
		return "H0"
	default:
		return "H9"
	}
}

// EuriborOmissao é o tenor que o próprio simulador traz escolhido. Usa-se quando
// o pedido não exprime preferência, e não se anota — não há escolha nenhuma a
// contrariar.
const EuriborOmissao = dominio.Euribor3M

// ContrapartidasMaximas é o número de produtos detidos que o simulador aceita.
const ContrapartidasMaximas = 4

// construirPayload traduz o Pedido no corpo que o banco espera.
//
// Devolve também os ajustes que a tradução obrigou a fazer, cada um com a sua
// nota, e as notas livres — pela ordem em que a pessoa as deve ler: primeiro o
// prazo, que muda o produto todo, depois a modalidade, depois o período.
//
// ⚠️ **Impõe-se tudo aqui, antes de ir à rede, e não é excesso de zelo.** Medido
// a 2026-07-27: o Montepio responde "Não existem condições disponíveis para a
// idade dos proponentes." a um prazo de 20 anos com 30 de período fixo, a um
// prazo de 3 anos e a um prazo acima do máximo — três causas, uma frase, e duas
// delas nada têm que ver com a idade. Uma leitura que classificasse a recusa pela
// mensagem do banco estaria a adivinhar.
func construirPayload(
	p dominio.Pedido,
	escaloes []EscalaoDePrazo,
	idadeMaisVelho int,
) (payload, []*dominio.Ajuste, []string, error) {
	var ajustes []*dominio.Ajuste
	var notas []string

	prazo, ajustePrazo, err := encaixarPrazo(p.PrazoAnos, idadeMaisVelho, escaloes)
	if err != nil {
		return payload{}, nil, nil, err
	}
	if ajustePrazo != nil {
		ajustes = append(ajustes, ajustePrazo)
	}

	sufixo, ajustesDaTaxa, notasDaTaxa, err := modalidade(p, prazo)
	if err != nil {
		return payload{}, nil, nil, err
	}
	ajustes = append(ajustes, ajustesDaTaxa...)
	notas = append(notas, notasDaTaxa...)

	indexante := p.Indexante
	if indexante == "" {
		indexante = EuriborOmissao
	}
	familia := familiaDe(indexante)
	produto := produtoDe(p.Finalidade)

	proponentes := make([]proponente, 0, len(p.Titulares))
	for i, t := range p.Titulares {
		proponentes = append(proponentes, proponenteDe(t, i+1))
	}

	corpo := payload{
		ConditionCode: condicao(produto, familia, sufixo),
		// ⚠️ Sai da mesma fonte que o ConditionCode e **não** de o partir por
		// "--": o "---" do filtro vazio faz um split devolver "-H900-M5", o
		// destino sai vazio, e o banco responde "Código Tipo finalidade não
		// existe" — um erro que não aponta para aqui.
		CreditDestinationCode: familia + "00",
		FamilyCode:            familiaMultiopcoes,
		ProductCode:           produto,
		FrequencyTypeCode:     frequenciaMensal,

		AcquisitionAmount: numero(p.ValorImovel),
		Ammount:           numero(p.Montante),
		Term:              prazo * 12,
		EvaluationAmount:  numero(p.ValorImovel),

		Device:     dispositivoNeutro(),
		DistrictID: distritoNeutro,
		Proponents: proponentes,

		OptionalExpenses: []despesa{{Code: seguroVida, Factor: 1}},

		// ⚠️ Vão as que a pessoa escolheu, e não as que o simulador traz. Aqui a
		// selecção **vai no pedido** (KAN-33): é este campo que decide, e não uma
		// segunda coluna da resposta como na CGD.
		Counterparts: contrapartidasDe(p),
	}
	return corpo, ajustes, notas, nil
}

// condicao monta o ConditionCode: {produto}{familia}-{filtro}--{familia}00-{sufixo}.
//
// ⚠️ O filtro vai **vazio** de propósito, e é isso que dá o `---` do meio. Com
// `C029` vinham as condições de campanha, que não são o preçário base e por isso
// não são o comparável — é a mesma regra por que o varrimento não usa campanhas.
func condicao(produto, familia, sufixo string) string {
	return fmt.Sprintf("%s%s---%s00-%s", produto, familia, familia, sufixo)
}

// modalidade decide o sufixo do ConditionCode: "V" para a variável, "M{n}" para
// os n anos de taxa fixa inicial.
//
// ⚠️ **O Montepio não tem taxa fixa pura**, e é este o banco que obriga o
// mecanismo "ajusta e anota" a existir. A fixa faz-se com o período fixo igual ao
// prazo, e aí o banco devolve uma fase só — taxa fixa a todo o contrato, de facto.
// Quando o prazo não é um dos períodos praticados, o mais longo que cabe deixa uma
// cauda indexada, e então o produto **é** misto: isso muda o tipo de taxa face ao
// que se pediu, e vai como ajuste.
func modalidade(p dominio.Pedido, prazo int) (string, []*dominio.Ajuste, []string, error) {
	switch p.TipoTaxa {
	case dominio.TaxaVariavel:
		return "V", nil, nil, nil

	case dominio.TaxaMista:
		// ⚠️ O tecto é prazo-1: com o período igual ao prazo não sobra cauda
		// indexada, e o que sai é taxa fixa — não é o que se pediu.
		tecto := prazo - 1
		periodo, ajuste, err := dominio.EncaixarPeriodoFixo(periodosValidos, p.PeriodoFixoAnos, &tecto)
		if err != nil {
			return "", nil, nil, &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf("Não há período de taxa fixa do Banco Montepio que caiba neste prazo: %v", err),
			}
		}
		if periodo >= prazo {
			return "", nil, nil, &dominio.ErroOferta{
				Codigo: dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf(
					"A taxa mista do Banco Montepio começa nos %d anos de período fixo, e esse período tem de ser "+
						"menor do que o prazo de %d anos.", periodo, prazo),
			}
		}
		var lista []*dominio.Ajuste
		if ajuste != nil {
			lista = append(lista, ajuste)
		}
		return fmt.Sprintf("M%d", periodo), lista, nil, nil

	case dominio.TaxaFixa:
		// Na fixa procura-se o período que iguale o prazo; o tecto é o prazo,
		// porque um período igual ao prazo é exactamente o que se quer.
		pedido := prazo
		periodo, _, err := dominio.EncaixarPeriodoFixo(periodosValidos, &pedido, &prazo)
		if err != nil {
			return "", nil, nil, &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf("O Banco Montepio não tem período de taxa fixa para este prazo: %v", err),
			}
		}

		if periodo == prazo {
			// Taxa fixa a todo o contrato — mas contratada com o produto de taxa
			// mista, e isso diz-se. Não é ajuste: nenhum número do pedido mudou.
			return fmt.Sprintf("M%d", periodo), nil, []string{
				"O Banco Montepio não tem produto de taxa fixa: esta simulação foi feita com o produto de taxa " +
					"mista e o período de taxa fixa igual ao prazo, o que dá taxa fixa em todo o contrato.",
			}, nil
		}

		ajuste := dominio.AjusteTipoTaxa(dominio.TaxaFixa, dominio.TaxaMista, fmt.Sprintf(
			"o Banco Montepio não tem taxa fixa pura, e o período de taxa fixa mais longo que cabe em %d anos "+
				"é de %d", prazo, periodo))
		return fmt.Sprintf("M%d", periodo), []*dominio.Ajuste{ajuste}, []string{
			fmt.Sprintf(
				"A taxa é fixa nos primeiros %d anos e indexada à Euribor nos %d restantes: o Banco Montepio só "+
					"pratica períodos de taxa fixa de %s anos.",
				periodo, prazo-periodo, listaDePeriodos()),
		}, nil

	default:
		return "", nil, nil, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf("tipo de taxa desconhecido: %q", p.TipoTaxa),
		}
	}
}

// encaixarPrazo limita o prazo ao que o banco aceita para esta idade.
//
// São três limites e vale o menor: o tecto do banco (PrazoMaximo), o escalão de
// idade que o arranque publica, e o contrato terminar até aos IdadeMaximaFim.
//
// ⚠️ **Um** ajuste ao prazo, com os motivos juntos numa frase, e sempre contra o
// prazo que a pessoa pediu. É a correcção que a corrida de fidelidade da CGD e
// do Novo Banco obrigou a fazer nos dois: dois sítios a encolher o prazo davam
// dois ajustes em cadeia, e o segundo dizia "Pediu 35 anos" a quem pediu 40.
func encaixarPrazo(pedido, idade int, escaloes []EscalaoDePrazo) (int, *dominio.Ajuste, error) {
	porEscalao := PrazoMaximoDoEscalao(escaloes, idade)
	porIdade := IdadeMaximaFimContrato - idade

	maximo := PrazoMaximo
	motivo := fmt.Sprintf("o máximo deste banco é de %d anos", PrazoMaximo)
	if porEscalao > 0 && porEscalao < maximo {
		maximo = porEscalao
		motivo = fmt.Sprintf("aos %d anos de idade o Banco Montepio financia no máximo %d", idade, porEscalao)
	}
	if porIdade < maximo {
		maximo = porIdade
		motivo = fmt.Sprintf("o contrato tem de terminar até aos %d anos", IdadeMaximaFimContrato)
	}

	if maximo < PrazoMinimo {
		return 0, nil, dominio.ErroDePrazo(BancoNome, idade+PrazoMinimo, IdadeMaximaFimContrato)
	}

	switch {
	case pedido > maximo:
		return maximo, dominio.AjustePrazo(pedido, maximo, motivo), nil
	case pedido < PrazoMinimo:
		return PrazoMinimo, dominio.AjustePrazo(pedido, PrazoMinimo,
			fmt.Sprintf("o mínimo deste banco é de %d anos", PrazoMinimo)), nil
	}
	return pedido, nil, nil
}

// contrapartidasDe traduz a escolha no número que o banco conta.
//
// ⚠️ O produto é um só e vale o máximo: o simulador aceita 0, 2, 3 ou 4 produtos
// detidos, e é o número deles que desconta. A curva medida está nos Requisitos —
// declarar aqui um grau intermédio seria oferecer um desconto que ninguém pediu.
func contrapartidasDe(p dominio.Pedido) int {
	if p.TemProduto(ProdutoContrapartidas) {
		return ContrapartidasMaximas
	}
	return 0
}

func proponenteDe(t dominio.Titular, posicao int) proponente {
	return proponente{
		Birthday: t.DataNascimento.String() + "T00:00:00.000Z",
		Position: posicao,
		State:    true,
		EntityType: tipoDeEntidade{
			Code: "P", CompanyID: 1, Description: "Proponente", ID: 1, State: true,
		},
		ExpenseCodes:  []string{seguroVida, seguroMultirriscos},
		AccountNumber: contaNeutra,
	}
}

// dispositivoNeutro é o que o simulador envia sobre o browser. Não é disfarce: é
// um cliente de browser a falar com um endpoint de browser, e o campo é
// obrigatório no payload.
func dispositivoNeutro() dispositivo {
	return dispositivo{
		Browser:        "chrome",
		BrowserVersion: "126.0.0.0",
		Os:             "windows",
		OsVersion:      "windows-10",
		UserAgent:      agente,
		Device:         "Desktop",
	}
}

func listaDePeriodos() string {
	texto := make([]string, 0, len(periodosValidos))
	for _, n := range periodosValidos {
		texto = append(texto, fmt.Sprint(n))
	}
	return strings.Join(texto[:len(texto)-1], ", ") + " e " + texto[len(texto)-1]
}

// numero converte um montante no literal que vai para o JSON, sem passar por
// float64.
func numero(d dominio.Dinheiro) json.Number { return json.Number(d.String()) }

// produtosAplicados são as contrapartidas que foram enviadas.
func (p payload) produtosAplicados() []string {
	if p.Counterparts <= 0 {
		return nil
	}
	return []string{ProdutoContrapartidas}
}
