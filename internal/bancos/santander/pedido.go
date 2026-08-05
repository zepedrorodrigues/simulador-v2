package santander

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O pedido do Santander, e a descoberta de configuração que o precede.
//
// ⚠️ Este banco é o primeiro dos escritos que **não sabe onde fica a sua própria
// API**: o `client_id` e o `bff_url` vêm de um `config.json` do SPA, lido em
// runtime. Os quatro anteriores tinham o endereço no código.
//
// ⚠️ Este ficheiro é puro: dados para dados, sem rede e sem relógio.

// BFFEstatico é para onde se cai quando o `config.json` remoto não serve.
//
// ⚠️ Não é redundância: é o que torna a validação do `bff_url` possível sem
// deixar o banco por responder. Ver `BFFSeguro`.
const BFFEstatico = "https://api-eeic.apis.santander.pt/santander/eeic/home_simulations"

// ClientIDEstatico é o mesmo, para o cabeçalho.
//
// ⚠️ Vazio de propósito. O `x-ibm-client-id` é uma credencial da aplicação e
// este repositório é público — quem correr isto contra o banco lê-o do
// `config.json`, que é o que o próprio SPA faz. Um valor aqui era publicá-lo.
const ClientIDEstatico = ""

// configDoSPA é o que se lê do config.json.
type configDoSPA struct {
	App struct {
		ClientID string `json:"client_id"`
		BFFURL   string `json:"bff_url"`
	} `json:"app"`
}

// lerConfig extrai o que o banco precisa do config.json do SPA.
func lerConfig(corpo []byte) (clientID, bff string, err error) {
	var c configDoSPA
	if err := json.Unmarshal(corpo, &c); err != nil {
		return "", "", ilegivel("a configuração do simulador", err)
	}

	bff = BFFSeguro(c.App.BFFURL)
	if bff == "" {
		bff = BFFEstatico
	}
	clientID = c.App.ClientID
	if clientID == "" {
		clientID = ClientIDEstatico
	}
	return clientID, strings.TrimRight(bff, "/"), nil
}

// BFFSeguro valida o `bff_url` vindo do config.json remoto, e devolve vazio se
// ele não servir.
//
// ⚠️ **Esta validação não é zelo, e a §3 manda mantê-la.** O `bff_url` é lido de
// um JSON remoto e usado para PUBLICAR os dados do pedido. Sem validação, um
// `config.json` adulterado — ou um intermediário sobre ele — apontava as nossas
// chamadas para um host arbitrário: os dados do pedido saíam para lá, e a
// resposta que voltasse era servida como se fosse do Santander.
//
// Exige-se `https` e um host do Santander. Quem falhar cai no `BFFEstatico`, que
// é conhecido.
func BFFSeguro(bruto string) string {
	if bruto == "" {
		return ""
	}
	u, err := url.Parse(bruto)
	if err != nil || u.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "santander.pt" || strings.HasSuffix(host, ".santander.pt") {
		return bruto
	}
	return ""
}

// --- o catálogo de taxas ---------------------------------------------------------

// catalogo é o que o /rates devolve.
type catalogo struct {
	AvailableRates []entradaDeCatalogo `json:"availableRates"`
}

type entradaDeCatalogo struct {
	Rate      taxaDoCatalogo `json:"rate"`
	Promotion *promocao      `json:"promotion"`
}

type taxaDoCatalogo struct {
	Type              string   `json:"type"`
	Indexantes        []string `json:"indexantes"`
	RateDurationYears *int     `json:"rateDurationYears"`
}

type promocao struct {
	Code                 string      `json:"code"`
	Description          string      `json:"description"`
	Spread               json.Number `json:"spread"`
	MinLoanDurationYears int         `json:"minLoanDurationYears"`
}

// Os tipos de taxa, no vocabulário do banco.
const (
	tipoVariavel = "VARIABLE"
	tipoMista    = "MIXED"
	tipoFixa     = "FIXED"
)

// escolherTaxa encontra no catálogo a taxa que corresponde ao pedido.
//
// ⚠️ **A taxa fixa do Santander é ao contrato TODO**, a 10, 20 ou 30 anos — o
// `rateDurationYears` dela é o prazo, não um período dentro dele. Foi medido a
// 2026-07-28 e o DOSSIE-BANCOS.md não o registava: dizia «mista {2,3,4} anos
// apenas», sem fixa nenhuma.
func escolherTaxa(c catalogo, p dominio.Pedido, prazo int) (taxaDoCatalogo, *dominio.Ajuste, error) {
	querido := tipoDe(p.TipoTaxa)
	if querido == "" {
		return taxaDoCatalogo{}, nil, indisponivel(
			fmt.Sprintf("tipo de taxa desconhecido: %q", p.TipoTaxa))
	}

	var candidatas []taxaDoCatalogo
	for _, e := range c.AvailableRates {
		if e.Rate.Type == querido {
			candidatas = append(candidatas, e.Rate)
		}
	}
	if len(candidatas) == 0 {
		return taxaDoCatalogo{}, nil, indisponivel(
			fmt.Sprintf("o Santander não tem taxa %s no catálogo de hoje", p.TipoTaxa))
	}

	// A variável tem uma entrada só — a indexada a 6 meses.
	if p.TipoTaxa == dominio.TaxaVariavel {
		return candidatas[0], nil, nil
	}

	anos := periodosDe(candidatas)
	if len(anos) == 0 {
		return candidatas[0], nil, nil
	}

	// Na FIXA o que se encaixa é o PRAZO; na mista, o período fixo.
	pedido := p.PeriodoFixoAnos
	if p.TipoTaxa == dominio.TaxaFixa {
		pedido = &prazo
	}

	escolhido, ajuste, err := dominio.EncaixarPeriodoFixo(anos, pedido, nil)
	if err != nil {
		return taxaDoCatalogo{}, nil, indisponivel(
			fmt.Sprintf("não há taxa %s do Santander que sirva este pedido: %v", p.TipoTaxa, err))
	}
	for _, t := range candidatas {
		if t.RateDurationYears != nil && *t.RateDurationYears == escolhido {
			return t, ajuste, nil
		}
	}
	return candidatas[0], nil, nil
}

func periodosDe(taxas []taxaDoCatalogo) []int {
	var anos []int
	for _, t := range taxas {
		if t.RateDurationYears != nil {
			anos = append(anos, *t.RateDurationYears)
		}
	}
	return anos
}

func tipoDe(t dominio.TipoTaxa) string {
	switch t {
	case dominio.TaxaVariavel:
		return tipoVariavel
	case dominio.TaxaMista:
		return tipoMista
	case dominio.TaxaFixa:
		return tipoFixa
	}
	return ""
}

// --- os limites ------------------------------------------------------------------

// limites é o que o /credit_limit devolve.
type limites struct {
	Limits struct {
		MinPropertyPrice      json.Number `json:"minPropertyPrice"`
		MaxPropertyPrice      json.Number `json:"maxPropertyPrice"`
		MinAllowedLoanAmount  json.Number `json:"minAllowedLoanAmount"`
		MinLoanDurationMonths int         `json:"minLoanDurationMonths"`
		MinAge                int         `json:"minAge"`
		MaxAge                int         `json:"maxAge"`
	} `json:"limits"`
}

// verificarLimites recusa antes de ir à rede o que o banco vai recusar.
//
// ⚠️ Os limites vêm DO BANCO e não de uma tabela nossa — é o melhor padrão que os
// cinco bancos deixaram, e aqui é literal: o `/credit_limit` publica-os. Uma
// tabela nossa envelhecia em silêncio.
func verificarLimites(l limites, p dominio.Pedido, idade int) error {
	imovel := p.ValorImovel.Decimal()

	if minimo, err := decimalDe(l.Limits.MinPropertyPrice); err == nil && minimo != nil && imovel.LessThan(*minimo) {
		return indisponivel(fmt.Sprintf(
			"O Santander não simula imóveis abaixo de %s €.", dominio.DinheiroDeDecimal(*minimo).ParaPessoa()))
	}
	if maximo, err := decimalDe(l.Limits.MaxPropertyPrice); err == nil && maximo != nil && imovel.GreaterThan(*maximo) {
		return indisponivel(fmt.Sprintf(
			"O Santander não simula imóveis acima de %s €.", dominio.DinheiroDeDecimal(*maximo).ParaPessoa()))
	}
	if minimo, err := decimalDe(l.Limits.MinAllowedLoanAmount); err == nil && minimo != nil &&
		p.Montante.Decimal().LessThan(*minimo) {
		return indisponivel(fmt.Sprintf(
			"O Santander não financia menos de %s €.", dominio.DinheiroDeDecimal(*minimo).ParaPessoa()))
	}
	if l.Limits.MinAge > 0 && idade < l.Limits.MinAge {
		return indisponivel(fmt.Sprintf("O Santander exige pelo menos %d anos de idade.", l.Limits.MinAge))
	}
	if l.Limits.MaxAge > 0 && idade > l.Limits.MaxAge {
		return indisponivel(fmt.Sprintf("O Santander não simula acima dos %d anos de idade.", l.Limits.MaxAge))
	}
	return nil
}

// --- o payload -------------------------------------------------------------------

// payload é o corpo do POST /get_by_rates.
type payload struct {
	HolderBirthDates  []string       `json:"holderBirthDates"`
	OccupationType    string         `json:"occupationType"`
	SimulationPurpose string         `json:"simulationPurpose"`
	PropertyPrice     json.Number    `json:"propertyPrice"`
	PropertyLocation  string         `json:"propertyLocation"`
	LoanAmount        json.Number    `json:"loanAmount"`
	DownPayment       json.Number    `json:"downPayment"`
	LoanDurationYears int            `json:"loanDurationYears"`
	Rates             []taxaDoPedido `json:"rates"`
	YouthPlan         bool           `json:"youthPlan"`
}

// taxaDoPedido é a taxa escolhida, na forma que o banco espera de volta.
//
// ⚠️ `rateDurationYears` é omitido na variável — o catálogo devolve-o a `null`, e
// mandá-lo como zero era pedir uma taxa de zero anos.
type taxaDoPedido struct {
	Type              string   `json:"type"`
	Indexantes        []string `json:"indexantes"`
	RateDurationYears *int     `json:"rateDurationYears,omitempty"`
}

// Os valores fixos do payload, tal como o simulador do banco os envia.
const (
	// ocupacaoNeutra: o Santander não pergunta pela profissão nem pelo
	// rendimento, e este campo é o único que descreve a situação — vai no valor
	// que o simulador traz de origem.
	ocupacaoNeutra = "PERMANENT"

	// finalidadeUnica: ⚠️ o simulador só aceita HOUSE_PURCHASE. Não distingue
	// arrendamento, e por isso a finalidade do pedido não viaja — está declarado
	// nos Requisitos.
	finalidadeUnica = "HOUSE_PURCHASE"
)

func localizacaoDe(l dominio.Localizacao) string {
	switch l {
	case dominio.LocalizacaoAcores:
		return "AZORES"
	case dominio.LocalizacaoMadeira:
		return "MADEIRA"
	default:
		return "CONTINENT"
	}
}

// construirPayload traduz o pedido no corpo do /get_by_rates.
func construirPayload(p dominio.Pedido, taxa taxaDoCatalogo, prazo int) (payload, error) {
	if len(p.Titulares) == 0 {
		return payload{}, indisponivel("O Santander precisa da data de nascimento de pelo menos um titular.")
	}

	nascimentos := make([]string, 0, len(p.Titulares))
	for _, t := range p.Titulares {
		nascimentos = append(nascimentos, t.DataNascimento.String())
	}

	// ⚠️ Nunca negativa: um montante acima do valor do imóvel é pedido inválido,
	// e o domínio recusa-o antes de chegar aqui — mas uma entrada negativa no
	// payload seria um número que o banco não sabe ler.
	entrada := p.ValorImovel.Decimal().Sub(p.Montante.Decimal())
	if entrada.IsNegative() {
		entrada = decimal.Zero
	}

	return payload{
		HolderBirthDates:  nascimentos,
		OccupationType:    ocupacaoNeutra,
		SimulationPurpose: finalidadeUnica,
		PropertyPrice:     json.Number(p.ValorImovel.String()),
		PropertyLocation:  localizacaoDe(p.Localizacao),
		LoanAmount:        json.Number(p.Montante.String()),
		DownPayment:       json.Number(entrada.String()),
		LoanDurationYears: prazo,
		Rates: []taxaDoPedido{{
			Type:              taxa.Type,
			Indexantes:        taxa.Indexantes,
			RateDurationYears: taxa.RateDurationYears,
		}},
		YouthPlan: p.GarantiaPublica,
	}, nil
}

func indisponivel(mensagem string) *dominio.ErroOferta {
	return &dominio.ErroOferta{Codigo: dominio.ErroProdutoIndisponivel, Mensagem: mensagem}
}
