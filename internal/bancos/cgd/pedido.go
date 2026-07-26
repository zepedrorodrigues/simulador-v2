package cgd

import (
	"fmt"
	"net/url"

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
	ajustes := junta(nil, ajustePrazo)

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
		anos, ajustesFixa, fixaErr := periodoDaFixa(p, prazo, cat.fixa)
		if fixaErr != nil {
			return nil, nil, fixaErr
		}
		valores.Set("IndexFixedRate", cat.fixa[anos])
		valores.Set("Years", fmt.Sprint(anos))
		ajustes = append(ajustes, ajustesFixa...)

	default:
		return nil, nil, &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: fmt.Sprintf("A CGD não simula taxa %q.", p.TipoTaxa),
		}
	}

	return valores, ajustes, nil
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
func periodoDaFixa(p dominio.Pedido, prazo int, cat codigos) (int, []*dominio.Ajuste, error) {
	anos, _, err := dominio.EncaixarPeriodoFixo(cat.anos(), &prazo, nil)
	if err != nil {
		return 0, nil, semPeriodos(err)
	}

	var ajustes []*dominio.Ajuste
	if p.PeriodoFixoAnos != nil && *p.PeriodoFixoAnos != anos {
		ajustes = junta(ajustes, dominio.AjustePeriodoFixo(*p.PeriodoFixoAnos, anos))
	}
	if anos != prazo {
		ajustes = junta(ajustes, dominio.AjustePrazo(prazo, anos,
			"a taxa fixa da CGD só existe entre 5 e 40 anos"))
	}
	return anos, ajustes, nil
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
	if lim.LTVMaximo > 0 && maior(ltv.Decimal(), lim.LTVMaximo) {
		return recusa(fmt.Sprintf(
			"O financiamento é de %s %% do valor do imóvel e a CGD vai até %.0f %% neste caso.",
			percentagem(ltv), lim.LTVMaximo*100))
	}
	// ⚠️ A Medida Jovem tem LTV **mínimo**, e não só máximo: a Garantia do
	// Estado cobre a fatia dos 85 % aos 100 %, e abaixo disso não há o que
	// garantir. Medido: com IsMedidaJovem, o /limits devolve ltvMinimum 0,85.
	if lim.LTVMinimo > 0 && menor(ltv.Decimal(), lim.LTVMinimo) {
		return recusa(fmt.Sprintf(
			"Com a Garantia Pública Jovens a CGD exige financiar pelo menos %.0f %% do imóvel, e este pedido fica-se por %s %%. Sem a garantia, este LTV é simulável.",
			lim.LTVMinimo*100, percentagem(ltv)))
	}
	return nil
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
func euros(d dominio.Dinheiro) string { return d.Decimal().Truncate(0).String() }

func booleano(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func percentagem(r dominio.Racio) string {
	return r.Decimal().Mul(decimal.NewFromInt(100)).StringFixed(1)
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
