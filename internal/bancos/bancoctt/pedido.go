package bancoctt

import (
	"encoding/json"
	"fmt"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O payload do Banco CTT é JSON, e um só pedido resolve a simulação inteira —
// sem página a raspar, sem endpoint de limites, sem sessão.
//
// ⚠️ Este ficheiro é **puro**: dados para dados, sem rede e sem relógio.
//
// ⚠️ E é escrito contra as capturas de `capturas/*.pedido.json`, não contra a
// documentação — que não existe — nem contra o que seria razoável o banco
// querer. Os campos que este payload leva com valor constante levam-no porque
// foi assim que o simulador do banco os enviou, e cada um diz aqui porquê.

// A modalidade, no vocabulário do banco: `IndexTypeSubCategoryID`.
const (
	subcategoriaFixa     = 1
	subcategoriaVariavel = 2
	subcategoriaMista    = 3
)

// Os `IndexTypeID` da mista, por anos de período fixo. Medidos a 2026-07-27 e
// confirmados no bundle do simulador.
var indexIDMista = map[int]int{
	1: 87,
	2: 82,
	3: 89,
	5: 75,
}

// Os `IndexTypeID` da fixa, por prazo. ⚠️ Na fixa do Banco CTT o período É o
// contrato todo — 30 ou 34 anos —, e não há fixa curta: quem pede 10 anos de
// taxa fixa não tem aqui produto nenhum.
var indexIDFixa = map[int]int{
	30: 80,
	34: 86,
}

// PeriodosMistos e PrazosFixos são as listas que os Requisitos publicam. Vivem
// aqui, ao lado dos identificadores de que saem, para não poderem divergir
// deles em silêncio.
var (
	PeriodosMistos = []int{1, 2, 3, 5}
	PrazosFixos    = []int{30, 34}
)

// indexIDVariavel é a Euribor a 12 meses, e é a única da variável.
//
// ⚠️ **Não é escolha nossa.** O bundle do simulador força este identificador —
// a variável é sempre 12M e o pós-período-fixo da mista é sempre 3M. Os outros
// ids respondem por API, e isso não faz deles oferta: é preço que o banco não
// comercializa (DOSSIE-BANCOS.md). O tenor que se publica lê-se **da resposta**,
// não daqui.
const indexIDVariavel = 5

// Os limites que o banco pratica, medidos.
const (
	PrazoMinAnos = 5
	PrazoMaxAnos = 40

	// IdadeMaximaFim é imposta por NÓS e não pelo banco. ⚠️ A API **não valida**:
	// medido a 2026-07-27, aceitou um titular de 66 anos com 40 de prazo, o que
	// põe o fim do contrato aos 106. Servir isso seria publicar uma oferta que
	// não existe.
	IdadeMaximaFim = 75
)

// Os valores constantes do payload, tal como o simulador do banco os envia.
const (
	// purposeDaCaptura é o único `Purpose` que este pacote envia.
	//
	// ⚠️ Os válidos são 2, 3 e 4 — o 1 e o 5 dão um 404 interno embrulhado num
	// `500 INTERNAL_ERROR` —, e **nenhum deles muda o preço** (medido a
	// 2026-07-27). Qual dos três corresponde a que finalidade não está medido, e
	// só há captura do 2. Enviar um palpite seria escrever no pedido uma
	// finalidade que não se sabe se é aquela; enviar sempre o que se capturou é
	// dizer a verdade sobre o que se mediu. A consequência — o banco não
	// distingue preço por finalidade — fica declarada nos Requisitos.
	purposeDaCaptura = 2

	// loanPurpose 1 é «aquisição», o único que as capturas exercitam.
	loanPurpose = 1

	// propertyType 1 é o tipo de imóvel da captura.
	propertyType = 1

	// propertyDistrict 24 é o distrito da captura. ⚠️ O Banco CTT não preça por
	// região — não há Açores nem Madeira no preço —, e por isso o distrito vai
	// constante em vez de traduzir a Localizacao do pedido. Declarado nos
	// Requisitos.
	propertyDistrict = 24

	// productScheme, referralCodeTypeID e simulationOriginID identificam a
	// origem da simulação no lado do banco. Vão como a captura os traz.
	productScheme      = 1
	referralCodeTypeID = "1"
	simulationOriginID = 1

	// monthlyInstallment a zero é como o simulador pergunta «quanto fica a
	// prestação» em vez de «quanto posso pedir com esta prestação».
	monthlyInstallment = 0
)

// payload é o corpo do POST /api/simulation/simulate.
//
// ⚠️ Os campos vão pela ordem em que o simulador do banco os envia, e os
// montantes como json.Number — o literal tal como está, nunca por float64.
type payload struct {
	LoanAmount         json.Number `json:"LoanAmount"`
	AmortizationPeriod int         `json:"AmortizationPeriod"`
	MonthlyInstallment int         `json:"MonthlyInstallment"`

	IndexTypeSubCategoryID int `json:"IndexTypeSubCategoryID"`
	IndexTypeID            int `json:"IndexTypeID"`

	ProductScheme int `json:"ProductScheme"`
	Purpose       int `json:"Purpose"`
	LoanPurpose   int `json:"LoanPurpose"`

	PropertyType     int         `json:"PropertyType"`
	PropertyValue    json.Number `json:"PropertyValue"`
	PropertyDistrict int         `json:"PropertyDistrict"`

	BorrowerOneBirthDate string  `json:"BorrowerOneBirthDate"`
	BorrowerTwoBirthDate *string `json:"BorrowerTwoBirthDate"`

	// HasCrossSelling vai SEMPRE a true, e não é a escolha de produtos.
	//
	// ⚠️ A resposta traz as duas colunas de preço — `*WithBenefits` e
	// `*WithoutBenefits` — no mesmo corpo, como a CGD. Quem escolhe a coluna é o
	// pedido, na leitura (ver resposta.go). Pô-lo a false devolveria só uma
	// coluna e gastava um segundo pedido para obter a outra.
	HasCrossSelling bool `json:"HasCrossSelling"`

	CampaignSustainabilityIsActive bool `json:"CampaignSustainabilityIsActive"`
	YouthMeasuresIsActive          bool `json:"YouthMeasuresIsActive"`

	ReferralCodeTypeID string `json:"ReferralCodeTypeID"`
	SimulationOriginID int    `json:"SimulationOriginID"`
}

// construirPayload traduz o Pedido no corpo que o banco espera.
//
// `prazoMaximo` é o tecto que a idade e os limites do banco já impuseram, e não
// é decorativo: é ele que impede a taxa fixa de **esticar** o prazo para lá do
// que o titular pode contratar.
//
// Devolve os ajustes que a tradução obrigou a fazer — hoje só ao período fixo —
// e o motivo por que o prazo mudou, quando mudou. ⚠️ O ajuste ao **prazo** não
// sai daqui: quem o escreve é quem simula, uma vez só e sempre contra o prazo
// que a pessoa pediu de facto. É a lição que a corrida de fidelidade da CGD
// deixou, e que o Novo Banco já regista.
func construirPayload(p dominio.Pedido, prazo, prazoMaximo int) (payload, []*dominio.Ajuste, string, error) {
	subcategoria, indexID, ajustes, prazoFinal, motivoDoPrazo, err := modalidade(p, prazo, prazoMaximo)
	if err != nil {
		return payload{}, nil, "", err
	}

	if len(p.Titulares) == 0 {
		return payload{}, nil, "", &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: "O Banco CTT precisa da data de nascimento de pelo menos um titular.",
		}
	}

	corpo := payload{
		LoanAmount:         numero(p.Montante),
		AmortizationPeriod: prazoFinal * 12,
		MonthlyInstallment: monthlyInstallment,

		IndexTypeSubCategoryID: subcategoria,
		IndexTypeID:            indexID,

		ProductScheme: productScheme,
		Purpose:       purposeDaCaptura,
		LoanPurpose:   loanPurpose,

		PropertyType:     propertyType,
		PropertyValue:    numero(p.ValorImovel),
		PropertyDistrict: propertyDistrict,

		BorrowerOneBirthDate: instanteDe(p.Titulares[0].DataNascimento),
		BorrowerTwoBirthDate: segundoTitular(p.Titulares),

		HasCrossSelling: true,

		// A campanha de sustentabilidade é um produto que vai NO PEDIDO — ao
		// contrário das vendas associadas, que vêm nas duas colunas da resposta.
		CampaignSustainabilityIsActive: p.TemProduto(ProdutoSustentavel),

		// A medida jovem é a garantia pública do DL 44/2024.
		YouthMeasuresIsActive: p.GarantiaPublica,

		ReferralCodeTypeID: referralCodeTypeID,
		SimulationOriginID: simulationOriginID,
	}
	return corpo, ajustes, motivoDoPrazo, nil
}

// modalidade decide a subcategoria, o identificador do índice e o prazo.
//
// É onde vivem as duas regras que o banco impõe:
//   - na **mista**, o período fixo é um de {1, 2, 3, 5} anos e tem de caber no
//     prazo;
//   - na **fixa**, o período é o contrato TODO e só existe a 30 e 34 anos — logo
//     é o prazo que se encaixa, e não o período.
func modalidade(p dominio.Pedido, prazo, prazoMaximo int) (
	subcategoria, indexID int, ajustes []*dominio.Ajuste, prazoFinal int, motivoDoPrazo string, err error,
) {
	switch p.TipoTaxa {
	case dominio.TaxaVariavel:
		return subcategoriaVariavel, indexIDVariavel, nil, prazo, "", nil

	case dominio.TaxaMista:
		// ⚠️ O tecto é prazo-1: um período fixo igual ao prazo não é uma mista,
		// é uma fixa — e a fixa do Banco CTT tem identificadores próprios.
		tecto := prazo - 1
		periodo, ajuste, err := dominio.EncaixarPeriodoFixo(PeriodosMistos, p.PeriodoFixoAnos, &tecto)
		if err != nil {
			return 0, 0, nil, 0, "", &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf("Não há período de taxa fixa do Banco CTT que caiba neste prazo: %v", err),
			}
		}
		id, ok := indexIDMista[periodo]
		if !ok {
			return 0, 0, nil, 0, "", &dominio.ErroOferta{
				Codigo: dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf(
					"A taxa mista do Banco CTT existe a 1, 2, 3 e 5 anos de período fixo, e não a %d.", periodo),
			}
		}
		var lista []*dominio.Ajuste
		if ajuste != nil {
			lista = append(lista, ajuste)
		}
		return subcategoriaMista, id, lista, prazo, "", nil

	case dominio.TaxaFixa:
		// ⚠️ Na fixa o prazo É o período. Encaixa-se o prazo em {30, 34} e o
		// motivo sobe a quem simula — não se escreve aqui um ajuste ao prazo,
		// para não haver dois sítios a encolhê-lo.
		pedido := prazo
		periodo, _, err := dominio.EncaixarPeriodoFixo(PrazosFixos, &pedido, &prazoMaximo)
		if err != nil {
			return 0, 0, nil, 0, "", &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf("O Banco CTT não tem taxa fixa para este prazo: %v", err),
			}
		}

		// ⚠️ A fixa é o único sítio deste banco onde encaixar o prazo o pode
		// FAZER CRESCER — quem pede 10 anos leva 30. Se o tecto da idade não dá
		// para 30, não há aqui produto nenhum, e dizê-lo é melhor do que servir
		// um contrato que termina depois dos 75. O EncaixarPeriodoFixo volta à
		// lista inteira quando o tecto não deixa nenhum de pé, e é isso que esta
		// guarda apanha.
		if prazoMaximo > 0 && periodo > prazoMaximo {
			return 0, 0, nil, 0, "", &dominio.ErroOferta{
				Codigo: dominio.ErroPrazoImpossivel,
				Mensagem: fmt.Sprintf(
					"A taxa fixa do Banco CTT é ao contrato todo e só existe a 30 e 34 anos, "+
						"e neste pedido o prazo não pode passar de %d anos.", prazoMaximo),
			}
		}

		motivo := ""
		if periodo != prazo {
			motivo = "a taxa fixa do Banco CTT é ao prazo todo e só existe a 30 e 34 anos"
		}
		return subcategoriaFixa, indexIDFixa[periodo], nil, periodo, motivo, nil

	default:
		return 0, 0, nil, 0, "", &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf("tipo de taxa desconhecido: %q", p.TipoTaxa),
		}
	}
}

// segundoTitular devolve a data do segundo titular, ou nulo.
//
// ⚠️ Nulo e não string vazia: a captura traz `"BorrowerTwoBirthDate": null` e
// uma string vazia é outro valor — que este banco não foi visto a aceitar.
func segundoTitular(ts []dominio.Titular) *string {
	if len(ts) < 2 {
		return nil
	}
	d := instanteDe(ts[1].DataNascimento)
	return &d
}

// instanteDe escreve uma data no formato que o simulador envia: ISO 8601 com
// hora zero e sufixo Z.
//
// ⚠️ A meia-noite é UTC e não local, tal como na captura. Escrever a hora local
// mudava o DIA para quem estivesse a leste de Greenwich, e o dia da data de
// nascimento é o que decide o prazo máximo.
func instanteDe(d dominio.Data) string {
	return fmt.Sprintf("%04d-%02d-%02dT00:00:00.000Z", d.Ano, int(d.Mes), d.Dia)
}

// numero converte um montante no literal que vai para o JSON, sem passar por
// float64.
func numero(d dominio.Dinheiro) json.Number { return json.Number(d.String()) }
