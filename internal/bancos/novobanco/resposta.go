package novobanco

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A resposta do Novo Banco é a mais rica dos dez: taxas, prestação, MTIC,
// seguros, comissões, despesas e o rácio DSTI — e tudo em duas variantes, com e
// sem bonificações, na mesma resposta.
//
// ⚠️ Os números vêm como números de JSON e são lidos para json.Number, não para
// float64. É a regra do dominio/dinheiro.go — dinheiro nunca passa por float64 —
// e aqui não custa nada: json.Number guarda o texto tal como veio e
// TaxaDeTexto/DinheiroDeTexto lêem-no exacto.
type resposta struct {
	Data   *dados  `json:"data"`
	Status *estado `json:"status"`
}

type dados struct {
	Resultado *resultado `json:"resultado"`
}

type resultado struct {
	Prestacao struct {
		Base           json.Number `json:"base"`
		SemBonificacao json.Number `json:"semBonificacao"`
	} `json:"prestacao"`

	Taxas struct {
		Spread               json.Number `json:"spread"`
		SpreadSemBonificacao json.Number `json:"spreadSemBonificacao"`
		TAEG                 json.Number `json:"taeg"`
		TAN                  json.Number `json:"tan"`
		TANSemBonificacao    json.Number `json:"tanSemBonificacao"`
		TaxaIndexada         json.Number `json:"taxaIndexada"`
	} `json:"taxas"`

	Prazo             int         `json:"prazo"`
	TipoTaxa          string      `json:"tipoTaxa"`
	TipoTaxaIndexante string      `json:"tipoTaxaIndexante"`
	MTIC              json.Number `json:"mtic"`
}

// estado é o erro estruturado do banco. ⚠️ É o melhor padrão dos dez: o código
// nomeia a causa e os parâmetros trazem o limite lá dentro, o que permite
// reaplicar em vez de adivinhar.
type estado struct {
	Code       string            `json:"code"`
	Severity   string            `json:"severity"`
	Parameters map[string]string `json:"parameters"`
}

// Os códigos que o banco usa e que este pacote sabe ler. Medidos a 2026-07-26,
// um a um, contra o simulador a sério — as capturas estão em `capturas/`.
const (
	// codigoPrazoPorIdade traz o prazo máximo para a idade no parâmetro "0".
	codigoPrazoPorIdade = "V159"
	// codigoFixaForaDoPrazo: na fixa o prazo tem de igualar o período, e o
	// parâmetro "0" diz qual.
	codigoFixaForaDoPrazo = "V157"
	// codigoPeriodoNaoCabe: o período fixo da mista tem de ser menor do que o
	// prazo. Sem parâmetros.
	codigoPeriodoNaoCabe = "V158"
	// codigoSemConta: contaNB=false. Sem parâmetros.
	codigoSemConta = "V126"
	// codigoPrestacaoAcimaDoTecto: a prestação passa o tecto que o parâmetro
	// "1" nomeia (7500 € medidos).
	codigoPrestacaoAcimaDoTecto = "V118"
	// codigoCamposEmFalta: campos obrigatórios em falta.
	codigoCamposEmFalta = "V137"
)

// parametro lê um parâmetro do erro. ⚠️ O banco prefixa-os com "DONT_TRANSLATE:"
// — "DONT_TRANSLATE:35" quer dizer 35 — e há-os inteiros ("35") e decimais
// ("7500.0"), por isso corta-se no ponto antes de converter.
func (e *estado) parametro(chave string) (int, bool) {
	if e == nil {
		return 0, false
	}
	bruto, existe := e.Parameters[chave]
	if !existe {
		return 0, false
	}
	texto := bruto
	if _, depois, achou := strings.Cut(texto, ":"); achou {
		texto = depois
	}
	texto, _, _ = strings.Cut(texto, ".")
	n, err := strconv.Atoi(strings.TrimSpace(texto))
	if err != nil {
		return 0, false
	}
	return n, true
}

// lerErro traduz uma recusa do banco.
//
// Devolve nil quando a resposta não é uma recusa. ⚠️ A distinção entre "este
// banco não faz isto" e "este banco está em baixo" é a do CONTRATO-BANCO.md §6,
// e a app responde-lhes de maneiras diferentes.
func lerErro(corpo []byte) (*estado, *dominio.ErroOferta) {
	var r resposta
	if err := json.Unmarshal(corpo, &r); err != nil {
		return nil, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: fmt.Sprintf("O Novo Banco respondeu qualquer coisa que não é JSON: %v", err),
		}
	}
	if r.Status == nil || r.Status.Code == "" {
		return nil, nil
	}

	e := r.Status
	switch e.Code {
	case codigoPrazoPorIdade:
		max, _ := e.parametro("0")
		return e, &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O Novo Banco não financia este prazo à idade do titular mais velho; o máximo que aceita é %d anos.", max),
		}
	case codigoFixaForaDoPrazo:
		exigido, _ := e.parametro("0")
		return e, &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"A taxa fixa do Novo Banco é ao prazo todo: com %d anos de taxa fixa o prazo tem de ser de %d anos.",
				exigido, exigido),
		}
	case codigoPeriodoNaoCabe:
		return e, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: "O período de taxa fixa da mista tem de ser menor do que o prazo do crédito.",
		}
	case codigoSemConta:
		return e, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: "O Novo Banco só simula com conta no banco (é o pressuposto que o simulador dele impõe).",
		}
	case codigoPrestacaoAcimaDoTecto:
		tecto, temTecto := e.parametro("1")
		msg := "A prestação que este prazo obriga passa o tecto que o Novo Banco simula."
		if temTecto {
			msg = fmt.Sprintf(
				"A prestação que este prazo obriga passa o tecto de %d € que o Novo Banco simula.", tecto)
		}
		return e, &dominio.ErroOferta{Codigo: dominio.ErroProdutoIndisponivel, Mensagem: msg}
	case codigoCamposEmFalta:
		return e, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: "O Novo Banco diz que faltam campos obrigatórios no pedido — é defeito nosso, não dele.",
		}
	default:
		// ⚠️ Um código que não se conhece não se traduz para português a fingir
		// que se percebeu: nomeia-se o código, que é o que permite ir procurá-lo.
		return e, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf("O Novo Banco recusou a simulação com o código %s.", e.Code),
		}
	}
}

// lerResposta traduz o corpo do banco numa Oferta.
//
// É pura: dados para dados, sem rede, sem relógio. Recebe o pedido tal como foi
// enviado — o `p` — porque o significado do campo `taxaIndexada` depende dele.
func lerResposta(corpo []byte, enviado payload) (dominio.Oferta, error) {
	var r resposta
	if err := json.Unmarshal(corpo, &r); err != nil {
		return dominio.Oferta{}, ilegivel("o corpo", err)
	}
	if r.Data == nil || r.Data.Resultado == nil {
		return dominio.Oferta{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: "O Novo Banco respondeu sem resultado nenhum.",
		}
	}
	res := r.Data.Resultado

	var o dominio.Oferta
	var err error

	if o.TAN, err = taxa(res.Taxas.TAN); err != nil {
		return o, ilegivel("a TAN", err)
	}
	if o.TAEG, err = taxa(res.Taxas.TAEG); err != nil {
		return o, ilegivel("a TAEG", err)
	}
	if o.Spread, err = taxa(res.Taxas.Spread); err != nil {
		return o, ilegivel("o spread", err)
	}
	if o.Prestacao, err = dinheiro(res.Prestacao.Base); err != nil {
		return o, ilegivel("a prestação", err)
	}
	if o.MTIC, err = dinheiro(res.MTIC); err != nil {
		return o, ilegivel("o MTIC", err)
	}

	// ⚠️ `taxaIndexada` só é uma Euribor na taxa variável. Na mista é a taxa de
	// referência do período fixo (medido a 2026-07-26: 3,061 aos 2 anos e 4,37
	// aos 30, com a Euribor a 12M nos 2,798) e na fixa é a própria taxa base.
	// Publicá-la como Euribor nos outros dois casos era inventar um indexante
	// que o banco não declarou — e o banco também não diz para que indexante o
	// crédito passa depois do período fixo.
	if enviado.ehVariavel() {
		if o.EuriborValor, err = taxa(res.Taxas.TaxaIndexada); err != nil {
			return o, ilegivel("o valor da Euribor", err)
		}
		o.Indexante = enviado.indexanteEscolhido()
	}

	// ⚠️ Ler da resposta o que o banco aplicou, e não confiar no nosso mapa
	// (CONTRATO-BANCO.md §5). Uma renumeração do lado dele fica visível em vez
	// de produzir números errados com ar de certos.
	prazo := res.Prazo
	if prazo < 1 {
		return o, ilegivel("o prazo", fmt.Errorf("o banco devolveu %d anos", res.Prazo))
	}

	if o.Fases, err = lerFases(res, prazo, enviado); err != nil {
		return o, err
	}
	if len(o.Fases) > 0 {
		if err := dominio.ValidarFases(o.Fases, prazo); err != nil {
			return o, ilegivel("o plano de fases", err)
		}
	}

	o.ProdutosAplicados = enviado.produtosAplicados()
	anotarBonificacoes(&o, res)
	return o, nil
}

// lerFases monta o plano.
//
// ⚠️ A mista fica **sem fases**, e isso é uma afirmação e não uma omissão: o
// Novo Banco devolve uma TAN só, a do período fixo, e não diz nada sobre o que
// vem depois dele — nem a taxa, nem o indexante, nem a prestação. Publicar essa
// TAN como se valesse o prazo todo era dizer que 3,992 % duram 30 anos quando
// duram 5. O contrato não exige `fases`, e quem lê recebe uma nota que o diz.
//
// Na variável e na fixa há uma fase e ela cobre o prazo todo — o que é verdade
// nas duas: a variável tem uma taxa só (que varia com a Euribor, e isso é
// inerente) e a fixa do Novo Banco é ao prazo todo por construção.
func lerFases(res *resultado, prazo int, enviado payload) ([]dominio.Fase, error) {
	if enviado.ehMista() {
		return nil, nil
	}
	tan, err := taxa(res.Taxas.TAN)
	if err != nil || tan == nil {
		return nil, ilegivel("a TAN da fase", err)
	}
	prestacao, err := dinheiro(res.Prestacao.Base)
	if err != nil || prestacao == nil {
		return nil, ilegivel("a prestação da fase", err)
	}
	fases, err := dominio.FasesDeDuracoes([]dominio.FaseDuracao{
		{Meses: prazo * 12, Taxa: *tan, Prestacao: *prestacao},
	})
	if err != nil {
		return nil, ilegivel("o plano de fases", err)
	}
	return fases, nil
}

// anotarBonificacoes diz, em números, o que as bonificações valem.
//
// ⚠️ As duas variantes vêm sempre na mesma resposta, e o dominio.Pedido não tem
// por onde se escolher produtos (KAN-33): as duas bonificações vão sempre
// ligadas, porque são as de omissão do simulador. É por isso que este preço é um
// preço com desconto — e dizê-lo é obrigatório. Apresentá-lo ao lado da CGD, que
// simula sem os packs, sem uma palavra que o diga, era comparar duas coisas
// diferentes (CONTRATO-BANCO.md §6).
func anotarBonificacoes(o *dominio.Oferta, res *resultado) {
	com, err := taxa(res.Taxas.Spread)
	if err != nil || com == nil {
		return
	}
	sem, err := taxa(res.Taxas.SpreadSemBonificacao)
	if err != nil || sem == nil {
		return
	}
	if com.Equal(*sem) {
		return
	}
	o.Anotar(fmt.Sprintf(
		"O preço inclui as bonificações do Novo Banco, que descontam %s p.p. de spread (%s em vez de %s). "+
			"Exigem domiciliar o ordenado e contratar os seguros.",
		sem.Sub(*com), com, sem))
}

// --- números ------------------------------------------------------------------

// taxa e dinheiro lêem um número de JSON sem passar por float64. Um campo
// ausente vem como json.Number vazia, e isso é "o banco não o deu" — devolve-se
// nil, que é o que a Oferta usa para omitir em vez de inventar.
func taxa(n json.Number) (*dominio.Taxa, error) {
	if n.String() == "" {
		return nil, nil
	}
	t, err := dominio.TaxaDeTexto(n.String())
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func dinheiro(n json.Number) (*dominio.Dinheiro, error) {
	if n.String() == "" {
		return nil, nil
	}
	d, err := dominio.DinheiroDeTexto(n.String())
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func ilegivel(oQue string, err error) error {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroRespostaIlegivel,
		Mensagem: fmt.Sprintf("Não se conseguiu ler %s da resposta do Novo Banco: %v", oQue, err),
	}
}
