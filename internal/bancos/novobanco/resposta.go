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
	anotarBonificacoes(&o, res, len(enviado.Bonificacoes) > 0)
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

// anotarBonificacoes diz o que as bonificações valem, ou que não estão neste
// preço.
//
// Um preço com desconto apresentado ao lado da CGD sem uma palavra que o diga
// era comparar duas coisas diferentes (CONTRATO-BANCO.md §6) — e a KAN-33 mediu
// que não era ruído: invertia a ordem dos dois bancos.
//
// ⚠️ A nota de quem **não** escolheu não leva números, e é deliberado. Com
// `bonificacoes: []` o banco devolve `spread` igual a `spreadSemBonificacao`
// (medido na captura `variavel_sem_produtos`), logo o preço bonificado não vem
// nesta resposta e não se pode afirmar daqui. Os valores medidos vivem nos
// Requisitos, com data — inventar aqui um número seria dar por cotado o que
// ninguém cotou (ARQUITETURA.md §4).
func anotarBonificacoes(o *dominio.Oferta, res *resultado, escolhidas bool) {
	if !escolhidas {
		o.Anotar("Este preço não inclui as bonificações do Novo Banco. " +
			"Escolhê-las desconta spread, e exigem domiciliar o ordenado e contratar os seguros.")
		return
	}
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

// --- os limites do /configuracoes (KAN-37) ------------------------------------

// configuracoes é o que o GET /configuracoes publica: a lista dos limites com
// que o banco simula.
//
// ⚠️ Os limites vêm DO BANCO e não de constantes nossas — é o mesmo padrão do
// /credit_limit do Santander e do /limits da CGD: uma tabela nossa envelhecia
// em silêncio. Zero é "o banco não o deu", e cada verificação salta o limite
// que vier a zero: falha aberto por limite.
type configuracoes struct {
	Limites limites
}

type limites struct {
	MontanteMinimo dominio.Dinheiro
	MontanteMaximo dominio.Dinheiro
	ImovelMinimo   dominio.Dinheiro
	ImovelMaximo   dominio.Dinheiro
	IdadeMinima    int
	IdadeMaxima    int
}

// configuracoesResposta é a forma bruta do /configuracoes, só para o
// json.Unmarshal. Os números vêm como json.Number e convertem-se por dinheiro e
// inteiro — nunca por float64, que é a regra de dominio/dinheiro.go.
type configuracoesResposta struct {
	Data *struct {
		Limites *struct {
			MontanteFinanciarMinimo json.Number `json:"montanteFinanciarMinimo"`
			MontanteFinanciarMaximo json.Number `json:"montanteFinanciarMaximo"`
			ValorImovelMinimo       json.Number `json:"valorImovelMinimo"`
			ValorImovelMaximo       json.Number `json:"valorImovelMaximo"`
			IdadeMinima             json.Number `json:"idadeMinima"`
			IdadeMaxima             json.Number `json:"idadeMaxima"`
		} `json:"limites"`
	} `json:"data"`
}

// lerConfiguracoes traduz o corpo do /configuracoes nos limites com que o banco
// simula. É pura: dados para dados, sem rede, sem relógio.
//
// ⚠️ Um limite que não se leia não derruba os outros — fica a zero, que é o
// mesmo que o banco não o ter publicado, e a verificação correspondente
// simplesmente não se faz.
func lerConfiguracoes(corpo []byte) (configuracoes, error) {
	var r configuracoesResposta
	if err := json.Unmarshal(corpo, &r); err != nil {
		return configuracoes{}, ilegivel("os limites", err)
	}
	if r.Data == nil || r.Data.Limites == nil {
		return configuracoes{}, &dominio.ErroOferta{
			Codigo:   dominio.ErroRespostaIlegivel,
			Mensagem: "O Novo Banco respondeu sem limites nenhuns.",
		}
	}

	l := r.Data.Limites
	var cfg configuracoes
	if d, err := dinheiro(l.MontanteFinanciarMinimo); err == nil && d != nil {
		cfg.Limites.MontanteMinimo = *d
	}
	if d, err := dinheiro(l.MontanteFinanciarMaximo); err == nil && d != nil {
		cfg.Limites.MontanteMaximo = *d
	}
	if d, err := dinheiro(l.ValorImovelMinimo); err == nil && d != nil {
		cfg.Limites.ImovelMinimo = *d
	}
	if d, err := dinheiro(l.ValorImovelMaximo); err == nil && d != nil {
		cfg.Limites.ImovelMaximo = *d
	}
	if i, ok := inteiro(l.IdadeMinima); ok {
		cfg.Limites.IdadeMinima = i
	}
	if i, ok := inteiro(l.IdadeMaxima); ok {
		cfg.Limites.IdadeMaxima = i
	}
	return cfg, nil
}

// temLimites diz se o corpo tinha ao menos um limite para verificar. Separa um
// /configuracoes útil de uma resposta vazia, e é o que impede que se guarde em
// catálogo — e sirva durante a validade — uma tabela sem nada dentro.
func (c configuracoes) temLimites() bool {
	l := c.Limites
	return l.MontanteMinimo.Positivo() || l.MontanteMaximo.Positivo() ||
		l.ImovelMinimo.Positivo() || l.ImovelMaximo.Positivo() ||
		l.IdadeMinima > 0 || l.IdadeMaxima > 0
}

// inteiro lê um número de JSON que é de facto um inteiro. Um campo ausente vem
// como json.Number vazia, e isso é "o banco não o deu".
func inteiro(n json.Number) (int, bool) {
	if n.String() == "" {
		return 0, false
	}
	i, err := strconv.Atoi(n.String())
	if err != nil {
		return 0, false
	}
	return i, true
}

// verificarConfiguracoes recusa antes de ir à rede o que os limites do banco já
// dizem que não passa — é o critério de pronto do KAN-37: um pedido abaixo do
// mínimo recusado **sem ida ao /calculo**, com uma mensagem que nomeia o limite
// e o valor.
//
// ⚠️ Manda o titular mais velho, a mesma idade que aperta o prazo; sem
// titulares (idade zero) a verificação por idade desliga-se.
func verificarConfiguracoes(cfg configuracoes, p dominio.Pedido, idade int) *dominio.ErroOferta {
	l := cfg.Limites

	if l.MontanteMinimo.Positivo() && p.Montante.Cmp(l.MontanteMinimo) < 0 {
		return &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O Novo Banco não financia menos de %s € (pedido: %s €).",
				l.MontanteMinimo.ParaPessoa(), p.Montante.ParaPessoa()),
		}
	}
	if l.MontanteMaximo.Positivo() && p.Montante.Cmp(l.MontanteMaximo) > 0 {
		return &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O Novo Banco não financia mais de %s € (pedido: %s €).",
				l.MontanteMaximo.ParaPessoa(), p.Montante.ParaPessoa()),
		}
	}
	if l.ImovelMinimo.Positivo() && p.ValorImovel.Cmp(l.ImovelMinimo) < 0 {
		return &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O Novo Banco não simula imóveis abaixo de %s € (o imóvel do pedido vale %s €).",
				l.ImovelMinimo.ParaPessoa(), p.ValorImovel.ParaPessoa()),
		}
	}
	if l.ImovelMaximo.Positivo() && p.ValorImovel.Cmp(l.ImovelMaximo) > 0 {
		return &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O Novo Banco não simula imóveis acima de %s € (o imóvel do pedido vale %s €).",
				l.ImovelMaximo.ParaPessoa(), p.ValorImovel.ParaPessoa()),
		}
	}
	if l.IdadeMinima > 0 && idade > 0 && idade < l.IdadeMinima {
		return &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O Novo Banco exige pelo menos %d anos de idade (o titular mais velho tem %d).",
				l.IdadeMinima, idade),
		}
	}
	if l.IdadeMaxima > 0 && idade > l.IdadeMaxima {
		return &dominio.ErroOferta{
			Codigo: dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf(
				"O Novo Banco não simula acima dos %d anos de idade (o titular mais velho tem %d).",
				l.IdadeMaxima, idade),
		}
	}
	return nil
}
