package bancoctt

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A resposta do Banco CTT traz sempre as DUAS colunas de preço — com e sem
// vendas associadas — no mesmo corpo, como a CGD. Um pedido dá as duas linhas
// do varrimento, e é por isso que os produtos deste banco são «de leitura» e
// não «de pedido» (ARQUITETURA.md §4).
//
// ⚠️ E traz duas famílias de campos, uma por fase, com a armadilha mais cara
// deste banco no meio:
//
//   - `Spread*` e `TAN*` são da PRIMEIRA fase. Na variável essa fase é a
//     indexada e o spread é o contratual. Na mista e na fixa a primeira fase é
//     FIXA — não tem indexante — e o banco preenche `Spread*` com a própria
//     TAN. Medido a 2026-07-27: na mista a 5 anos vem `SpreadWithoutBenefits`
//     4,000 e `TANWithoutBenefits` 4,000; na fixa a 30 anos vem 4,650 nos dois.
//     Publicar isso como spread punha o Banco CTT a 4,650 de spread ao lado dos
//     1,350 dos outros — não é caro, é outra coisa.
//   - `VariableSpread*`, `VariableTAN*` e `VariableIndexRate` são da fase
//     INDEXADA da mista, e é aí que está o spread contratual (1,350 medido).
//
// A mesma armadilha está registada no Santander e no Bankinter
// (DOSSIE-BANCOS.md).
//
// ⚠️ O tenor da Euribor lê-se da resposta — `IndexRateDescription` na variável,
// `VariableIndexRateDescription` na mista — e não do nosso mapa de
// identificadores. Uma renumeração do lado do banco fica visível em vez de
// produzir números errados com ar de certos (CONTRATO-BANCO.md §5).

// A identidade do banco, e os produtos que ele expõe pelos ids que os
// Requisitos publicam. ⚠️ O prefixo do id do produto é o id do banco, e é por
// ele que o Pedido.ProdutosDoBanco separa a selecção de cada um.
const (
	IDBanco   = "bancoctt"
	NomeBanco = "Banco CTT"
)

const (
	// ProdutoVendasAssociadas é o pacote de vendas associadas: é o que a
	// resposta chama «WithBenefits».
	ProdutoVendasAssociadas = "bancoctt:vendas_associadas"

	// ProdutoSustentavel exige classe energética ≥ A. ⚠️ Não tem coluna própria
	// na resposta — vai no pedido (`CampaignSustainabilityIsActive`) e o efeito
	// aparece nas colunas que já existem.
	ProdutoSustentavel = "bancoctt:sustentavel"
)

type resposta struct {
	Success bool    `json:"success"`
	Data    *dados  `json:"data"`
	Erro    *string `json:"error"`
}

// dados são os campos que este pacote lê. Os que ficam de fora — identificadores
// da simulação, seguros, comissões — não entram numa dominio.Oferta e não se
// declaram: um campo declarado e não usado promete uma leitura que não existe.
//
// ⚠️ As taxas vêm como TEXTO em formato português ("2,798") e o dinheiro vem
// como número de JSON. Nenhum dos dois passa por float64: o texto vai ao
// numeroPT e o número a json.Number, que guarda os dígitos tal como vieram
// (dominio/dinheiro.go).
type dados struct {
	IndexRate                    string `json:"IndexRate"`
	IndexRateDescription         string `json:"IndexRateDescription"`
	VariableIndexRate            string `json:"VariableIndexRate"`
	VariableIndexRateDescription string `json:"VariableIndexRateDescription"`

	AmortizationPeriodMonths int `json:"AmortizationPeriodMonths"`
	LoanFixedPeriodMonths    int `json:"LoanFixedPeriodMonths"`
	LoanVariablePeriodMonths int `json:"LoanVariablePeriodMonths"`

	SpreadWithBenefits            string `json:"SpreadWithBenefits"`
	SpreadWithoutBenefits         string `json:"SpreadWithoutBenefits"`
	VariableSpreadWithBenefits    string `json:"VariableSpreadWithBenefits"`
	VariableSpreadWithoutBenefits string `json:"VariableSpreadWithoutBenefits"`

	TANWithBenefits            string `json:"TANWithBenefits"`
	TANWithoutBenefits         string `json:"TANWithoutBenefits"`
	VariableTANWithBenefits    string `json:"VariableTANWithBenefits"`
	VariableTANWithoutBenefits string `json:"VariableTANWithoutBenefits"`

	TAEGWithBenefits    string `json:"TAEGWithBenefits"`
	TAEGWithoutBenefits string `json:"TAEGWithoutBenefits"`

	MTICWithBenefits    json.Number `json:"MTICWithBenefits"`
	MTICWithoutBenefits json.Number `json:"MTICWithoutBenefits"`

	MonthlyInstallmentWithBonification    json.Number `json:"MonthlyInstallmentWithBonification"`
	MonthlyInstallmentWithoutBonification json.Number `json:"MonthlyInstallmentWithoutBonification"`

	VariableMonthlyInstallmentAmountWithBonification    json.Number `json:"VariableMonthlyInstallmentAmountWithBonification"`
	VariableMonthlyInstallmentAmountWithoutBonification json.Number `json:"VariableMonthlyInstallmentAmountWithoutBonification"`
}

// coluna escolhe entre o preço com e sem vendas associadas.
//
// ⚠️ Quem escolhe é o PEDIDO e não o banco (ARQUITETURA.md §4, KAN-33): um
// pedido sem produtos pede o preço sem produtos. Servir a coluna bonificada a
// quem não a pediu punha o Banco CTT barato ao lado de quem serve o preçário
// base — que é a comparação invertida que a KAN-33 mediu.
type coluna struct {
	spread, spreadIndexado string
	tan, tanIndexado       string
	taeg                   string
	mtic                   json.Number
	prestacao              json.Number
	prestacaoIndexada      json.Number
}

func (d *dados) coluna(comProdutos bool) coluna {
	if comProdutos {
		return coluna{
			spread: d.SpreadWithBenefits, spreadIndexado: d.VariableSpreadWithBenefits,
			tan: d.TANWithBenefits, tanIndexado: d.VariableTANWithBenefits,
			taeg: d.TAEGWithBenefits, mtic: d.MTICWithBenefits,
			prestacao:         d.MonthlyInstallmentWithBonification,
			prestacaoIndexada: d.VariableMonthlyInstallmentAmountWithBonification,
		}
	}
	return coluna{
		spread: d.SpreadWithoutBenefits, spreadIndexado: d.VariableSpreadWithoutBenefits,
		tan: d.TANWithoutBenefits, tanIndexado: d.VariableTANWithoutBenefits,
		taeg: d.TAEGWithoutBenefits, mtic: d.MTICWithoutBenefits,
		prestacao:         d.MonthlyInstallmentWithoutBonification,
		prestacaoIndexada: d.VariableMonthlyInstallmentAmountWithoutBonification,
	}
}

// lerResposta traduz o corpo do /api/simulation/simulate para uma oferta.
//
// É pura: dados para dados, sem rede e sem relógio. Recebe o pedido porque o
// significado dos campos depende do tipo de taxa — e porque é o pedido que diz
// que produtos foram escolhidos.
func lerResposta(corpo []byte, p dominio.Pedido) (dominio.Oferta, error) {
	var r resposta
	if err := json.Unmarshal(corpo, &r); err != nil {
		return dominio.Oferta{}, ilegivel("o corpo", err)
	}
	if !r.Success || r.Data == nil {
		return dominio.Oferta{}, recusa(r.Erro)
	}
	d := r.Data

	produtos := p.ProdutosDoBanco(IDBanco)
	col := d.coluna(len(produtos) > 0)

	var o dominio.Oferta
	var err error

	if o.TAEG, err = taxaPT(col.taeg); err != nil {
		return o, ilegivel("a TAEG", err)
	}
	if o.MTIC, err = dinheiroJSON(col.mtic); err != nil {
		return o, ilegivel("o MTIC", err)
	}

	// A TAN de topo é a da PRIMEIRA fase — o que o cliente começa a pagar. Na
	// mista é a da fase fixa, e é por isso que ela não bate com Euribor+spread:
	// essa identidade vale na fase indexada, não aqui.
	if o.TAN, err = taxaPT(col.tan); err != nil {
		return o, ilegivel("a TAN", err)
	}

	if err := lerIndexado(&o, d, col, p); err != nil {
		return o, err
	}
	if o.Fases, err = lerFases(d, col, p); err != nil {
		return o, err
	}
	if len(o.Fases) > 0 {
		if err := dominio.ValidarFases(o.Fases, p.PrazoAnos); err != nil {
			return o, ilegivel("o plano de fases", err)
		}
	}

	o.ProdutosAplicados = produtos
	anotarVendasAssociadas(&o, len(produtos) > 0)
	return o, nil
}

// lerIndexado preenche spread, indexante e valor da Euribor — ou deixa-os
// nulos, que é a leitura certa numa fixa pura.
//
// ⚠️ É aqui que a armadilha do `Spread` se fecha. O campo existe nas três
// modalidades e só é um spread contratual em duas delas.
func lerIndexado(o *dominio.Oferta, d *dados, col coluna, p dominio.Pedido) error {
	var bruto, valor, descricao string

	switch p.TipoTaxa {
	case dominio.TaxaVariavel:
		// A primeira fase É a indexada: o spread e o índice são os de topo.
		bruto, valor, descricao = col.spread, d.IndexRate, d.IndexRateDescription

	case dominio.TaxaMista:
		// A fase indexada é a segunda. O `Spread` de topo é o da fase fixa e vem
		// igual à TAN — o contratual é o `VariableSpread`.
		bruto, valor, descricao = col.spreadIndexado, d.VariableIndexRate, d.VariableIndexRateDescription

	case dominio.TaxaFixa:
		// ⚠️ Numa fixa pura não há indexante nem spread sobre ele, e a §4 manda
		// as colunas nulas. O banco preenche `Spread` com a TAN (4,650 medidos a
		// 30 anos); lê-lo era publicar um spread que não existe.
		return nil

	default:
		return ilegivel("o tipo de taxa", fmt.Errorf("o pedido traz %q", p.TipoTaxa))
	}

	spread, err := taxaPT(bruto)
	if err != nil {
		return ilegivel("o spread", err)
	}
	o.Spread = spread

	euribor, err := taxaPT(valor)
	if err != nil {
		return ilegivel("o valor da Euribor", err)
	}
	o.EuriborValor = euribor

	indexante, err := lerIndexante(descricao)
	if err != nil {
		return err
	}
	o.Indexante = indexante
	return nil
}

// lerIndexante lê o tenor da etiqueta que o banco devolveu.
//
// ⚠️ Da resposta e não do nosso mapa (CONTRATO-BANCO.md §5). O Banco CTT não
// deixa escolher o indexante — variável sempre 12M, pós-fixo da mista sempre
// 3M —, e é precisamente por isso que se lê o que ele aplicou: uma mudança de
// omissão do lado dele aparece, em vez de ficar a nossa constante a mentir.
func lerIndexante(descricao string) (dominio.Indexante, error) {
	texto := strings.ToUpper(strings.TrimSpace(descricao))
	if !strings.Contains(texto, "EURIBOR") {
		return "", ilegivel("o indexante", fmt.Errorf("o banco etiquetou a fase indexada como %q", descricao))
	}
	switch {
	case strings.Contains(texto, "12M"):
		return dominio.Euribor12M, nil
	case strings.Contains(texto, "6M"):
		return dominio.Euribor6M, nil
	case strings.Contains(texto, "3M"):
		return dominio.Euribor3M, nil
	}
	return "", ilegivel("o indexante", fmt.Errorf("o banco etiquetou %q, e não é um dos tenores conhecidos", descricao))
}

// lerFases monta o plano de prestações.
//
// A mista tem duas fases e o banco publica-as inteiras — ao contrário do Novo
// Banco, que não diz o que vem depois do período fixo. Aqui não é preciso
// assumir nada: `LoanFixedPeriodMonths` e `VariableMonthlyInstallmentAmount*`
// vêm na resposta.
func lerFases(d *dados, col coluna, p dominio.Pedido) ([]dominio.Fase, error) {
	prazoMeses := d.AmortizationPeriodMonths
	if prazoMeses < 1 {
		return nil, ilegivel("o prazo", fmt.Errorf("o banco devolveu %d meses", prazoMeses))
	}

	tan, err := taxaPT(col.tan)
	if err != nil || tan == nil {
		return nil, ilegivel("a TAN da fase", err)
	}
	prestacao, err := dinheiroJSON(col.prestacao)
	if err != nil || prestacao == nil {
		return nil, ilegivel("a prestação da fase", err)
	}

	if p.TipoTaxa != dominio.TaxaMista {
		return dominio.FasesDeDuracoes([]dominio.FaseDuracao{
			{Meses: prazoMeses, Taxa: *tan, Prestacao: *prestacao},
		})
	}

	fixos := d.LoanFixedPeriodMonths
	if fixos < 1 || fixos >= prazoMeses {
		return nil, ilegivel("o período fixo", fmt.Errorf(
			"o banco devolveu %d meses de fixo num prazo de %d", fixos, prazoMeses))
	}
	tanIndexada, err := taxaPT(col.tanIndexado)
	if err != nil || tanIndexada == nil {
		return nil, ilegivel("a TAN da fase indexada", err)
	}
	prestacaoIndexada, err := dinheiroJSON(col.prestacaoIndexada)
	if err != nil || prestacaoIndexada == nil {
		return nil, ilegivel("a prestação da fase indexada", err)
	}

	return dominio.FasesDeDuracoes([]dominio.FaseDuracao{
		{Meses: fixos, Taxa: *tan, Prestacao: *prestacao},
		{Meses: prazoMeses - fixos, Taxa: *tanIndexada, Prestacao: *prestacaoIndexada},
	})
}

// anotarVendasAssociadas diz o que o preço pressupõe.
//
// Um preço com desconto ao lado do preçário base de outro banco compara coisas
// diferentes sem o dizer (CONTRATO-BANCO.md §6), e a KAN-33 mediu que isso
// chega a inverter a ordem dos bancos.
func anotarVendasAssociadas(o *dominio.Oferta, escolhidas bool) {
	if escolhidas {
		o.Anotar("Este preço pressupõe as vendas associadas do Banco CTT — " +
			"domiciliação de ordenado e os seguros contratados com o banco.")
		return
	}
	o.Anotar("Este preço não inclui as vendas associadas do Banco CTT. " +
		"Escolhê-las desconta spread, e exigem domiciliar o ordenado e contratar os seguros com o banco.")
}

// recusa traduz um corpo que não trouxe simulação.
func recusa(erro *string) *dominio.ErroOferta {
	mensagem := "O Banco CTT não devolveu simulação para este pedido."
	if erro != nil && strings.TrimSpace(*erro) != "" {
		mensagem = fmt.Sprintf("O Banco CTT recusou: %s", strings.TrimSpace(*erro))
	}
	return &dominio.ErroOferta{Codigo: dominio.ErroProdutoIndisponivel, Mensagem: mensagem}
}

func ilegivel(oQue string, err error) *dominio.ErroOferta {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroRespostaIlegivel,
		Mensagem: fmt.Sprintf("Não se conseguiu ler %s da resposta do Banco CTT: %v", oQue, err),
	}
}

// numeroPT lê um número em formato português. ⚠️ Com vírgula decimal, um ponto
// só pode ser separador de milhares.
func numeroPT(s string) (decimal.Decimal, error) {
	limpo := strings.NewReplacer(" ", "", " ", "", "%", "", "€", "").Replace(s)
	if limpo == "" {
		return decimal.Zero, fmt.Errorf("número vazio")
	}
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

// taxaPT lê uma taxa em texto português. Vazio à entrada é nulo à saída — e não
// zero: o Banco CTT deixa em branco a `VariableIndexRateDescription` da
// variável, e um zero ali seria uma Euribor de 0 % afirmada.
func taxaPT(s string) (*dominio.Taxa, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	d, err := numeroPT(s)
	if err != nil {
		return nil, err
	}
	t := dominio.TaxaDeDecimal(d)
	return &t, nil
}

// dinheiroJSON lê dinheiro que veio como número de JSON, pelos dígitos e nunca
// por float64.
func dinheiroJSON(n json.Number) (*dominio.Dinheiro, error) {
	texto := strings.TrimSpace(n.String())
	if texto == "" {
		return nil, nil
	}
	d, err := decimal.NewFromString(texto)
	if err != nil {
		return nil, fmt.Errorf("número inválido %q: %w", texto, err)
	}
	m := dominio.DinheiroDeDecimal(d)
	return &m, nil
}
