package novobanco

import (
	"encoding/json"
	"fmt"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O payload do Novo Banco é JSON, e um só pedido resolve a simulação inteira —
// não há página a raspar nem lead a preencher. Os limites é que vêm antes, do
// GET /configuracoes (KAN-37), mas isso é trabalho do novobanco.go, não deste
// ficheiro.
//
// ⚠️ Este ficheiro é **puro**: dados para dados, sem rede, sem relógio. A idade
// entra por parâmetro, nunca de um time.Now() aqui dentro.

// Os períodos que o banco pratica, medidos um a um a 2026-07-26. São os mesmos
// nove para a mista e para a fixa; um código fora desta lista dá um erro que o
// banco não nomeia ("omc.fwk.genericError").
var periodosValidos = []int{2, 3, 4, 5, 10, 15, 20, 25, 30}

// payload é o corpo que o simulador espera.
//
// Guarda também o que se decidiu ao construí-lo — o tipo de taxa e o indexante
// escolhido — porque a leitura da resposta precisa disso: o campo `taxaIndexada`
// só é uma Euribor quando o pedido foi de taxa variável.
// ⚠️ Os montantes vão como json.Number e não como float64. É a mesma regra da
// leitura, do lado do envio: json.Number marshala o literal tal como está, por
// isso "250000" sai `250000` e não `250000.00000000001`.
type payload struct {
	Idade           int          `json:"idade"`
	EntradaInicial  json.Number  `json:"entradaInicial"`
	AgregadoFamilia agregado     `json:"agregadoFamiliar"`
	Proponentes     []proponente `json:"proponentes"`
	ContaNB         bool         `json:"contaNB"`
	Imovel          imovel       `json:"imovel"`
	Emprestimos     []emprestimo `json:"emprestimos"`
	Prazo           int          `json:"prazo"`
	TipoTaxa        string       `json:"tipoTaxa"`
	TipoTaxaIndex   string       `json:"tipoTaxaIndexante"`
	RegimeCredito   string       `json:"regimeCredito"`
	ValorAvaliacao  json.Number  `json:"valorAvaliacaoTotal"`
	Missao          string       `json:"missao"`
	Bonificacoes    []string     `json:"bonificacoes"`
	Campanhas       []string     `json:"campanhas"`

	// indexante é o tenor que se pediu, quando o pedido é de taxa variável.
	// Vazio nos outros dois casos — e é isso que impede publicar como Euribor
	// uma taxa que não o é.
	indexante dominio.Indexante
}

type agregado struct {
	NumeroMembros            int `json:"numeroMembros"`
	EncargosMensaisOutrosCre int `json:"encargosMensaisOutrosCreditos"`
}

// proponente é um titular. ⚠️ Os campos de perfil vão com valor neutro e não
// com dados reais: nenhum deles mexe no preço — medido a 2026-07-26, um a um,
// variando cada um contra a mesma simulação (profissão, habilitações, estado
// civil, vínculo, situação profissional e NIF deram todos o mesmo cêntimo). O
// que o banco lê de facto é a data de nascimento, que manda no prazo máximo.
type proponente struct {
	NIF                  string `json:"nif"`
	Nacionalidade        string `json:"nacionalidade"`
	Residencia           string `json:"residencia"`
	EstadoCivil          string `json:"estadoCivil"`
	Habilitacoes         string `json:"habilitacoes"`
	Profissao            string `json:"profissao"`
	SituacaoProfissional string `json:"situacaoProfissional"`
	VinculoLaboral       string `json:"vinculoLaboral"`
	PrestacaoSegura      bool   `json:"prestacaoSegura"`
	DataNascimento       string `json:"dataNascimento"`
	RendimentoMensal     int64  `json:"rendimentoMensalLiquido"`
	RendimentoAnual      int64  `json:"rendimentoLiquidoAnual"`
}

type imovel struct {
	Localizacao     string `json:"localizacao"`
	TipoPropriedade string `json:"tipoPropriedade"`
	Tipologia       string `json:"tipologia"`
}

type emprestimo struct {
	FinalidadeCredito string      `json:"finalidadeCredito"`
	ValorAquisicao    json.Number `json:"valorAquisicao"`
	ValorEmprestimo   json.Number `json:"valorEmprestimo"`
}

// Os valores neutros dos campos que o simulador exige e que não mexem no preço.
const (
	nifNeutro                  = "123456789"
	nacionalidadeNeutra        = "PORTUGUESA"
	residenciaNeutra           = "RESIDENTE"
	estadoCivilNeutro          = "1"
	habilitacoesNeutras        = "902"
	profissaoNeutra            = "1369"
	situacaoProfissionalNeutra = "101"
	vinculoLaboralNeutro       = "21"

	// tipologiaNeutra: o simulador pede a tipologia e o preço não se mexe com
	// ela — medido (T1 e T3 deram o mesmo cêntimo).
	tipologiaNeutra = "T3"

	regimeGeral       = "GERAL"
	missaoBase        = "SABER_QUANTO_POSSO_GASTAR"
	finalidadeComprar = "COMPRAR"
)

// Os identificadores das bonificações, do lado do banco.
const (
	bonificacaoPrimeiroBanco = "PRIMEIRO_BANCO"
	bonificacaoProtecao      = "PROTECAO"
)

// bonificacoesDe traduz os produtos escolhidos nos códigos do banco, pela ordem
// em que o simulador os lista.
//
// ⚠️ Devolve uma lista vazia e não nula: o campo `bonificacoes` vai sempre no
// JSON, e a captura `variavel_sem_produtos` prova que o banco aceita `[]` — o
// que responde com o preço sem desconto nenhum, e não com um erro.
func bonificacoesDe(p dominio.Pedido) []string {
	bs := make([]string, 0, 2)
	if p.TemProduto(ProdutoPrimeiroBanco) {
		bs = append(bs, bonificacaoPrimeiroBanco)
	}
	if p.TemProduto(ProdutoProtecao) {
		bs = append(bs, bonificacaoProtecao)
	}
	return bs
}

func localizacaoDe(l dominio.Localizacao) string {
	switch l {
	case dominio.LocalizacaoAcores:
		return "ACORES"
	case dominio.LocalizacaoMadeira:
		return "MADEIRA"
	default:
		return "CONTINENTE"
	}
}

// tipoPropriedadeDe traduz a finalidade.
//
// ⚠️ Não é decorativa neste banco: o arrendamento custa +0,50 p.p. de spread
// (medido a 2026-07-26: 0,90 na própria e na segunda habitação, 1,40 no
// arrendamento). A segunda habitação tem o preço da própria.
func tipoPropriedadeDe(f dominio.Finalidade) string {
	switch f {
	case dominio.FinalidadeSecundaria:
		return "SEGUNDA_HABITACAO"
	case dominio.FinalidadeArrendamento:
		return "ARRENDAMENTO"
	default:
		return "HABITACAO_PROPRIA_PERMANENTE"
	}
}

func euriborDe(i dominio.Indexante) string {
	switch i {
	case dominio.Euribor3M:
		return "INDEXADA_EURIBOR_3_MES"
	case dominio.Euribor6M:
		return "INDEXADA_EURIBOR_6_MES"
	default:
		return "INDEXADA_EURIBOR_12_MES"
	}
}

// EuriborOmissao é o tenor que o próprio simulador do Novo Banco traz escolhido.
// Usa-se quando o pedido não exprime preferência — e não se anota, porque não há
// escolha nenhuma a contrariar.
const EuriborOmissao = dominio.Euribor12M

// construirPayload traduz o Pedido no corpo que o banco espera.
//
// Devolve também os ajustes ao **período fixo** que a tradução obrigou a fazer,
// cada um com a sua nota.
//
// ⚠️ O ajuste ao **prazo** não sai daqui, e a razão é o que a corrida de
// fidelidade da CGD apanhou: com dois sítios a encolher o prazo — o V159 pela
// idade e o encaixe da fixa nos nove valores — saíam dois ajustes em cadeia, e
// o segundo dizia «Pediu 35 anos» a quem tinha pedido 40. Aqui devolve-se só o
// motivo por que o prazo mudou, e quem escreve o ajuste é o simularComPrazo,
// uma vez só e sempre contra o prazo que a pessoa pediu de facto.
func construirPayload(p dominio.Pedido, prazo int) (payload, []*dominio.Ajuste, string, error) {
	var ajustes []*dominio.Ajuste

	tipoTaxa, indexanteCodigo, indexante, ajustePeriodo, prazoFinal, motivoDoPrazo, err := modalidade(p, prazo)
	if err != nil {
		return payload{}, nil, "", err
	}
	if ajustePeriodo != nil {
		ajustes = append(ajustes, ajustePeriodo...)
	}

	proponentes := make([]proponente, 0, len(p.Titulares))
	for _, t := range p.Titulares {
		proponentes = append(proponentes, proponenteDe(t))
	}

	corpo := payload{
		Idade:           0, // o banco calcula-a a partir da data de nascimento
		EntradaInicial:  numero(entradaDe(p)),
		AgregadoFamilia: agregado{},
		Proponentes:     proponentes,
		ContaNB:         true, // obrigatório: sem ele o banco responde V126
		Imovel: imovel{
			Localizacao:     localizacaoDe(p.Localizacao),
			TipoPropriedade: tipoPropriedadeDe(p.Finalidade),
			Tipologia:       tipologiaNeutra,
		},
		Emprestimos: []emprestimo{{
			FinalidadeCredito: finalidadeComprar,
			ValorAquisicao:    numero(p.ValorImovel),
			ValorEmprestimo:   numero(p.Montante),
		}},
		Prazo:          prazoFinal,
		TipoTaxa:       tipoTaxa,
		TipoTaxaIndex:  indexanteCodigo,
		RegimeCredito:  regimeGeral,
		ValorAvaliacao: numero(p.ValorImovel),
		Missao:         missaoBase,
		// ⚠️ Vão as que a pessoa escolheu, e não as que o simulador do banco
		// traz ligadas. Aqui a selecção **vai no pedido**: medido a 2026-07-26,
		// com as duas o spread é 0,90 e sem nenhuma é 1,600, e o que decide qual
		// é este campo — ao contrário da CGD, que devolve as duas colunas na
		// mesma resposta.
		Bonificacoes: bonificacoesDe(p),
		Campanhas:    []string{},
		indexante:    indexante,
	}
	return corpo, ajustes, motivoDoPrazo, nil
}

// modalidade decide o tipo de taxa, o código do indexante e o prazo.
//
// É onde vivem as duas regras que o banco impõe e que a captura mediu:
//   - na **mista**, o período fixo tem de ser **menor** do que o prazo (senão
//     V158) e tem de estar na lista dos nove (senão um erro sem nome);
//   - na **fixa**, o prazo tem de **igualar** o período (senão V157), e por isso
//     é o prazo que se encaixa, não o período.
func modalidade(p dominio.Pedido, prazo int) (tipo, codigo string, idx dominio.Indexante, ajustes []*dominio.Ajuste, prazoFinal int, motivoDoPrazo string, err error) {
	switch p.TipoTaxa {
	case dominio.TaxaVariavel:
		escolhido := p.Indexante
		if escolhido == "" {
			escolhido = EuriborOmissao
		}
		return "INDEXADA", euriborDe(escolhido), escolhido, nil, prazo, "", nil

	case dominio.TaxaMista:
		// ⚠️ O tecto é prazo-1 e não prazo: o período fixo tem de ser
		// estritamente menor do que o prazo. Uma mista de 25 anos num prazo de
		// 25 é recusada com V158 — medido.
		tecto := prazo - 1
		periodo, ajuste, err := dominio.EncaixarPeriodoFixo(periodosValidos, p.PeriodoFixoAnos, &tecto)
		if err != nil {
			return "", "", "", nil, 0, "", &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf("Não há período de taxa fixa do Novo Banco que caiba neste prazo: %v", err),
			}
		}
		if periodo >= prazo {
			return "", "", "", nil, 0, "", &dominio.ErroOferta{
				Codigo: dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf(
					"A taxa mista do Novo Banco começa nos %d anos de período fixo, e esse período tem de ser menor do que o prazo de %d anos.",
					periodo, prazo),
			}
		}
		var lista []*dominio.Ajuste
		if ajuste != nil {
			lista = append(lista, ajuste)
		}
		return "MISTA", fmt.Sprintf("MISTA_%d_ANOS", periodo), "", lista, prazo, "", nil

	case dominio.TaxaFixa:
		// ⚠️ Na fixa o prazo **é** o período: o banco exige que coincidam e
		// devolve o exigido dentro do V157. Por isso encaixa-se o prazo na lista
		// dos nove — e devolve-se o motivo, não o ajuste: quem o escreve é o
		// simularComPrazo, contra o prazo que a pessoa pediu.
		pedido := prazo
		periodo, _, err := dominio.EncaixarPeriodoFixo(periodosValidos, &pedido, nil)
		if err != nil {
			return "", "", "", nil, 0, "", &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: fmt.Sprintf("O Novo Banco não tem taxa fixa para este prazo: %v", err),
			}
		}
		motivo := ""
		if periodo != prazo {
			motivo = "a taxa fixa do Novo Banco é ao prazo todo e só existe em 2, 3, 4, 5, 10, 15, 20, 25 e 30 anos"
		}
		return "FIXA", fmt.Sprintf("FIXA_%d_ANOS", periodo), "", nil, periodo, motivo, nil

	default:
		return "", "", "", nil, 0, "", &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf("tipo de taxa desconhecido: %q", p.TipoTaxa),
		}
	}
}

func proponenteDe(t dominio.Titular) proponente {
	rendimento := t.RendimentoMensal.Decimal().IntPart()
	return proponente{
		NIF:                  nifNeutro,
		Nacionalidade:        nacionalidadeNeutra,
		Residencia:           residenciaNeutra,
		EstadoCivil:          estadoCivilNeutro,
		Habilitacoes:         habilitacoesNeutras,
		Profissao:            profissaoNeutra,
		SituacaoProfissional: situacaoProfissionalNeutra,
		VinculoLaboral:       vinculoLaboralNeutro,
		PrestacaoSegura:      true,
		DataNascimento:       t.DataNascimento.String(),
		RendimentoMensal:     rendimento,
		RendimentoAnual:      rendimento * 14,
	}
}

func entradaDe(p dominio.Pedido) dominio.Dinheiro {
	if p.Montante.Cmp(p.ValorImovel) >= 0 {
		return dominio.DinheiroDeInteiro(0)
	}
	return dominio.DinheiroDeDecimal(p.ValorImovel.Decimal().Sub(p.Montante.Decimal()))
}

// numero converte um montante no literal que vai para o JSON, sem passar por
// float64.
func numero(d dominio.Dinheiro) json.Number { return json.Number(d.String()) }

// --- o que a leitura da resposta precisa de saber do pedido -------------------

func (p payload) ehVariavel() bool { return p.TipoTaxa == "INDEXADA" }
func (p payload) ehMista() bool    { return p.TipoTaxa == "MISTA" }

// indexanteEscolhido devolve o tenor pedido. Só faz sentido na variável — nos
// outros dois casos é vazio de propósito.
func (p payload) indexanteEscolhido() dominio.Indexante { return p.indexante }

// produtosAplicados são as bonificações que foram enviadas.
func (p payload) produtosAplicados() []string {
	aplicados := make([]string, 0, len(p.Bonificacoes))
	for _, b := range p.Bonificacoes {
		switch b {
		case bonificacaoPrimeiroBanco:
			aplicados = append(aplicados, ProdutoPrimeiroBanco)
		case bonificacaoProtecao:
			aplicados = append(aplicados, ProdutoProtecao)
		}
	}
	if len(aplicados) == 0 {
		return nil
	}
	return aplicados
}
