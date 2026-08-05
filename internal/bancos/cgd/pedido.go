package cgd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Este ficheiro é PURO: recebe o pedido, os limites e o catálogo de períodos, e
// devolve o formulário. Sem rede, sem relógio — a idade entra já calculada,
// porque quem tem relógio é o cgd.go.

// Os códigos do formulário. `tax` é o tipo de taxa e `ProductPurpose` o destino
// do imóvel; ambos saíram das capturas de 2026-07-26.
const (
	taxFixa     = "1"
	taxVariavel = "2"
	taxMista    = "3"

	destinoPropria      = "1"
	destinoSecundaria   = "2"
	destinoArrendamento = "3"

	// purposeAquisicao é fixo: este simulador é de aquisição. Transferência de
	// crédito é outro produto e não se pede por aqui.
	purposeAquisicao = "1"
)

// catalogo são os dois conjuntos de períodos que a CGD publica, já lidos do
// HTML. São dois porque a CGD vende dois produtos diferentes — ver
// periodosDoHTML.
type catalogo struct {
	fixa  codigos
	mista codigos

	// deRecurso diz que estes períodos não são os que a CGD publica hoje: são
	// os últimos conhecidos, porque o HTML não se deixou ler. ⚠️ Muda o que se
	// pode afirmar a quem lê a oferta — sem isto, um prazo encolhido por
	// ignorância nossa saía com a justificação de ser o banco a não o praticar.
	deRecurso bool
}

// construirPayload monta o formulário do /calculate e diz o que teve de ajustar.
//
// idadeMaisVelho a zero desliga o limite por idade sem caso especial: o
// EncaixarPrazo faz `IdadeMaximaFim - idade`, e com zero isso dá sempre mais do
// que o prazo máximo do banco.
func construirPayload(
	p dominio.Pedido, lim limites, cat catalogo, idadeMaisVelho int,
) (url.Values, []*dominio.Ajuste, error) {
	if err := dentroDosLimites(p, lim); err != nil {
		return nil, nil, err
	}

	prazo, ajustePrazo, err := dominio.EncaixarPrazo(p.PrazoAnos, requisitosVivos(lim), idadeMaisVelho)
	if err != nil {
		return nil, nil, err
	}
	var ajustes []*dominio.Ajuste

	valores := url.Values{
		"SimulationSubOriginID": {"1"},
		"Code":                  {""},
		"IsMedidaJovem":         {booleano(p.GarantiaPublica)},
		"Purpose":               {purposeAquisicao},
		"ProductPurpose":        {destino(p.Finalidade)},
		"PropertyValue":         {euros(p.ValorImovel)},
		"Loan":                  {euros(p.Montante)},
		"Years":                 {fmt.Sprint(prazo)},
		"IndexFixedRate":        {""},
		"IndexRateMixedFixed":   {""},
	}

	switch p.TipoTaxa {
	case dominio.TaxaVariavel:
		valores.Set("tax", taxVariavel)

	case dominio.TaxaMista:
		valores.Set("tax", taxMista)
		anos, ajuste, encErr := dominio.EncaixarPeriodoFixo(cat.mista.anos(), p.PeriodoFixoAnos, &prazo)
		if encErr != nil {
			return nil, nil, semPeriodos(encErr)
		}
		valores.Set("IndexRateMixedFixed", cat.mista[anos])
		ajustes = junta(ajustes, ajuste)

	case dominio.TaxaFixa:
		valores.Set("tax", taxFixa)
		anos, ajustePeriodo, fixaErr := periodoDaFixa(p, prazo, cat)
		if fixaErr != nil {
			return nil, nil, fixaErr
		}
		valores.Set("IndexFixedRate", cat.fixa[anos])
		valores.Set("Years", fmt.Sprint(anos))
		ajustes = junta(ajustes, ajustePeriodo)

		// ⚠️ Se a fixa mexeu no prazo outra vez, o ajuste dos limites é
		// deitado fora e faz-se **um só**, do que a pessoa pediu para o que
		// se simulou. Encadear dois dava uma segunda nota a dizer «Pediu 32
		// anos» a quem tinha pedido 35 — um número que ninguém pediu, numa
		// frase que se apresenta como sendo o pedido dela. Medido na corrida
		// de fidelidade de 2026-07-26, 5 vezes em 2889.
		if anos != prazo {
			ajustePrazo = dominio.AjustePrazo(p.PrazoAnos, anos, motivoDoPrazoDaFixa(cat, prazo != p.PrazoAnos))
		}

	default:
		return nil, nil, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf("A CGD não simula taxa %q.", p.TipoTaxa),
		}
	}

	return valores, junta(ajustes, ajustePrazo), nil
}

// motivoDoPrazoDaFixa nomeia a razão verdadeira de o prazo ter mudado.
//
// ⚠️ Antes dizia sempre «a taxa fixa da CGD só existe entre 5 e 40 anos», e
// isso era falso quando o prazo estava lá dentro: 32 anos está entre 5 e 40 e a
// CGD vende-os — o que faltava era a lista, que não se conseguiu ler. Dar a
// nossa ignorância como recusa do banco é pôr na boca dele uma coisa que ele
// não disse.
func motivoDoPrazoDaFixa(cat catalogo, tambemPorLimites bool) string {
	motivo := fmt.Sprintf("a taxa fixa da CGD é ao prazo todo e só existe entre %d e %d anos",
		minimoDe(cat.fixa), maximoDe(cat.fixa))
	if cat.deRecurso {
		motivo = "a taxa fixa da CGD é ao prazo todo e não se conseguiu ler a lista de prazos que ela pratica hoje"
	}
	if tambemPorLimites {
		motivo = "os limites da CGD para este caso, e " + motivo
	}
	return motivo
}

func minimoDe(c codigos) int {
	menor := 0
	for ano := range c {
		if menor == 0 || ano < menor {
			menor = ano
		}
	}
	return menor
}

func maximoDe(c codigos) int {
	maior := 0
	for ano := range c {
		if ano > maior {
			maior = ano
		}
	}
	return maior
}

// periodoDaFixa escolhe o código da taxa fixa e explica o que muda com isso.
//
// ⚠️ Na CGD a taxa fixa é ao prazo todo, e é o código do período que **manda no
// prazo** — o campo Years é decorativo. Medido a 2026-07-26: com o código de 30
// anos e Years=10, a resposta veio com TotalDuration 30; com o código de 10
// anos e Years=30, veio 10. Por isso o que se encaixa no catálogo da fixa é o
// prazo, e não o período fixo pedido.
//
// O v1 mandava os dois campos como se fossem independentes e depois reportava o
// prazo pedido. Um pedido de fixa a 10 anos num prazo de 30 dava-lhe uma
// prestação de 10 anos rotulada de 30.
func periodoDaFixa(p dominio.Pedido, prazo int, cat catalogo) (int, *dominio.Ajuste, error) {
	anos, _, err := dominio.EncaixarPeriodoFixo(cat.fixa.anos(), &prazo, nil)
	if err != nil {
		return 0, nil, semPeriodos(err)
	}
	if p.PeriodoFixoAnos != nil && *p.PeriodoFixoAnos != anos {
		return anos, dominio.AjustePeriodoFixo(*p.PeriodoFixoAnos, anos), nil
	}
	return anos, nil, nil
}

// dentroDosLimites recusa o que a CGD não vende.
//
// ⚠️ Verifica-se aqui e não se deixa a resposta falar: medido a 2026-07-26, o
// /calculate aceita um LTV de 96 % e devolve um preço com todo o ar de válido,
// que o banco não pratica. É a lição do Banco CTT outra vez — uma API aceitar
// não é o banco vender. E, quando ele recusa, recusa com `{"success":false}` e
// mais nada, sem uma palavra que se possa mostrar a alguém.
func dentroDosLimites(p dominio.Pedido, lim limites) error {
	if lim.ValorImovelMin > 0 && menor(p.ValorImovel.Decimal(), lim.ValorImovelMin) {
		return recusa(fmt.Sprintf(
			"A CGD só simula imóveis a partir de %.0f €.", lim.ValorImovelMin))
	}
	if lim.MontanteMinimo > 0 && menor(p.Montante.Decimal(), lim.MontanteMinimo) {
		return recusa(fmt.Sprintf(
			"A CGD só empresta a partir de %.0f €.", lim.MontanteMinimo))
	}
	if lim.MontanteMaximo > 0 && maior(p.Montante.Decimal(), lim.MontanteMaximo) {
		return recusa(fmt.Sprintf(
			"A CGD empresta no máximo %.0f € neste produto.", lim.MontanteMaximo))
	}

	ltv, err := dominio.LTV(p.Montante, p.ValorImovel)
	if err != nil {
		return recusa("Sem valor do imóvel não há LTV, e a CGD decide por LTV.")
	}
	// ⚠️ As duas recusas seguintes falam em euros e não só em percentagens, e
	// isso é correcção de um defeito medido: com um LTV de 80,004 %, a
	// mensagem antiga saía «O financiamento é de 80.0 % do valor do imóvel e a
	// CGD vai até 80 %» — as duas percentagens arredondavam para o mesmo
	// número e a frase lia-se como disparate. Em euros não há arredondamento
	// que a torne contraditória.
	if lim.LTVMaximo > 0 && maior(ltv.Decimal(), lim.LTVMaximo) {
		return recusa(fmt.Sprintf(
			"A CGD financia no máximo %.0f %% do valor do imóvel: com um imóvel de %s €, são %s €. O pedido é de %s €.",
			lim.LTVMaximo*100, eurosParaPessoa(p.ValorImovel),
			tectoEmEuros(p.ValorImovel, lim.LTVMaximo), eurosParaPessoa(p.Montante)))
	}
	// ⚠️ A Medida Jovem tem LTV **mínimo**, e não só máximo: a Garantia do
	// Estado cobre a fatia dos 85 % aos 100 %, e abaixo disso não há o que
	// garantir. Medido: com IsMedidaJovem, o /limits devolve ltvMinimum 0,85.
	if lim.LTVMinimo > 0 && menor(ltv.Decimal(), lim.LTVMinimo) {
		return recusa(fmt.Sprintf(
			"Com a Garantia Pública Jovens a CGD exige financiar pelo menos %.0f %% do imóvel: com um imóvel de %s €, são %s €. O pedido é de %s €. Sem a garantia, este montante é simulável.",
			lim.LTVMinimo*100, eurosParaPessoa(p.ValorImovel),
			tectoEmEuros(p.ValorImovel, lim.LTVMinimo), eurosParaPessoa(p.Montante)))
	}
	return nil
}

// tectoEmEuros é a fatia do valor do imóvel que uma percentagem representa.
func tectoEmEuros(valorImovel dominio.Dinheiro, fraccao float64) string {
	tecto := valorImovel.Decimal().Mul(decimal.NewFromFloat(fraccao)).Truncate(0)
	return eurosParaPessoa(dominio.DinheiroDeDecimal(tecto))
}

// requisitosVivos monta uns Requisitos a partir dos limites que a CGD acabou de
// devolver, para o EncaixarPrazo do domínio decidir com os números de hoje e
// não com constantes nossas.
func requisitosVivos(lim limites) dominio.Requisitos {
	return dominio.Requisitos{
		BancoID:        BancoID,
		BancoNome:      BancoNome,
		PrazoMin:       max(lim.PrazoMinimo, 1),
		PrazoMax:       lim.PrazoMaximo,
		IdadeMaximaFim: lim.IdadeMaximaAoFim,
	}
}

func destino(f dominio.Finalidade) string {
	switch f {
	case dominio.FinalidadeSecundaria:
		return destinoSecundaria
	case dominio.FinalidadeArrendamento:
		return destinoArrendamento
	default:
		return destinoPropria
	}
}

// euros escreve o montante como a CGD o quer: euros inteiros, sem separadores.
// Trunca em vez de arredondar — pedir mais do que se pediu podia atravessar um
// degrau de LTV, e o degrau é o preço.
// euros escreve um montante para o PAYLOAD da CGD: inteiro, sem separadores.
//
// ⚠️ É formato de máquina e não muda (KAN-51). O simulador recebe isto num
// formulário; um espaço de milhares lá dentro é um pedido que o banco não lê.
// Para as frases que as pessoas lêem há o `eurosParaPessoa` — e a distinção
// custou uma corrida do portão a descobrir, com cinco payloads a falhar.
func euros(d dominio.Dinheiro) string { return d.Decimal().Truncate(0).String() }

// eurosParaPessoa é o mesmo montante para uma frase de recusa: «250 000».
//
// ⚠️ Sem cêntimos: numa frase sobre tectos de LTV eles são ruído, e o valor
// entra aqui já truncado — daí o sufixo `,00` ser sempre removível.
func eurosParaPessoa(d dominio.Dinheiro) string {
	inteiro := dominio.DinheiroDeDecimal(d.Decimal().Truncate(0))
	return strings.TrimSuffix(inteiro.ParaPessoa(), ",00")
}

func booleano(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func menor(d decimal.Decimal, limite float64) bool {
	return d.LessThan(decimal.NewFromFloat(limite))
}

func maior(d decimal.Decimal, limite float64) bool {
	return d.GreaterThan(decimal.NewFromFloat(limite))
}

func recusa(mensagem string) error {
	return &dominio.ErroOferta{Codigo: dominio.ErroProdutoIndisponivel, Mensagem: mensagem}
}

func semPeriodos(err error) error {
	return &dominio.ErroOferta{
		Codigo:   dominio.ErroRespostaIlegivel,
		Mensagem: fmt.Sprintf("Não se apurou nenhum período de taxa fixa da CGD: %v", err),
	}
}

// junta acrescenta um ajuste que pode ser nulo — as políticas do domínio
// devolvem nulo quando não houve nada a ajustar.
func junta(ajustes []*dominio.Ajuste, a *dominio.Ajuste) []*dominio.Ajuste {
	if a == nil {
		return ajustes
	}
	return append(ajustes, a)
}
