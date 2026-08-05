package dominio

import (
	"errors"
	"fmt"
	"slices"

	"github.com/shopspring/decimal"
)

// O modelo de encargos de um banco: dois parâmetros, AJUSTADOS a observações e
// não escolhidos por nós.
//
// O porquê das duas naturezas — antecipada e recorrente —, os rácios medidos que
// as tornam identificáveis e as hipóteses que ficam declaradas estão na §4 do
// ARQUITETURA.md («A TAEG deriva-se com os encargos MEDIDOS»).
//
// ⚠️ Uma das hipóteses não se pode medir aqui, e é por isso que fica escrita: que
// o recorrente incide sobre o capital em DÍVIDA e não sobre o inicial. Separá-la
// exigia observar a TAEG a montantes diferentes, e o montante não é dimensão da
// grelha — está medido que o spread não depende dele. É escolha de forma
// funcional, não medição.

var (
	// ErrPoucasObservacoes: com menos de dois prazos os dois parâmetros não são
	// identificáveis. Devolve-se erro em vez de se fixar um deles: fixá-lo era
	// exactamente a assunção que este ficheiro existe para não fazer.
	ErrPoucasObservacoes = errors.New("o ajuste dos encargos exige observações em pelo menos dois prazos")

	// ErrEncargosNaoAjustaveis: a raiz não está dentro dos limites plausíveis. É
	// dados maus — uma TAEG abaixo da TAN, por exemplo — e não um banco caro.
	ErrEncargosNaoAjustaveis = errors.New("não há encargos plausíveis que expliquem estas observações")
)

// Os limites da procura. Existem para uma raiz absurda sair como erro em vez de
// sair como número.
//
// ⚠️ 10 % do capital em encargos antecipados e 2 p.p. ao ano de encargo
// recorrente são ambos muito acima de qualquer coisa praticada — um seguro de
// vida ronda 0,2 a 0,5 p.p. O intervalo é largo de propósito: o que se paga por
// o exagerar são iterações de bissecção, e o que se pagava por o apertar era um
// erro em vez de um ajuste, no dia em que um banco cobrasse mais do que se
// esperava.
var (
	antecipadoMaximo = decimal.NewFromFloat(0.10)
	recorrenteMaximo = decimal.NewFromInt(2)
)

// Encargos é o modelo ajustado de um banco.
type Encargos struct {
	// Antecipado é a fracção do capital paga no início.
	Antecipado Racio

	// Recorrente é a taxa anual, em pontos percentuais, que incide sobre o
	// capital em dívida.
	Recorrente Taxa
}

// ObservacaoDeEncargo é um par (TAN, TAEG) que um banco devolveu num prazo.
//
// ⚠️ O Capital vai no campo porque a TAEG depende dele, e não se assume que
// todas as observações partilhem o mesmo — hoje partilham (a grelha varre o
// prazo ao montante de referência), e amarrar isso ao tipo tornaria o dia em que
// deixarem de partilhar num erro silencioso.
type ObservacaoDeEncargo struct {
	Capital    Dinheiro
	PrazoMeses int
	TAN        Taxa
	TAEG       Taxa
}

// Residuo é o que o ajuste não explicou.
type Residuo struct {
	// PrazoMeses é a observação em que se mediu o resíduo.
	PrazoMeses int

	// Observada e Prevista são as duas TAEG, para o desvio ser verificável e não
	// apenas afirmado.
	Observada, Prevista Taxa

	// Desvio é Prevista − Observada, em pontos percentuais. O sinal interessa: um
	// desvio positivo é uma TAEG nossa mais CARA do que a do banco.
	Desvio Taxa
}

// ResiduoTolerado é o desvio a partir do qual o ajuste deixa de descrever o
// banco.
//
// ⚠️ 0,05 p.p. é o mesmo orçamento que o refinamento do LTV usa, e não é
// coincidência: é a metade da casa decimal que o contrato publica, portanto é o
// maior desvio que ainda não muda o número que a pessoa lê. Acima disto, o modelo
// de duas naturezas não descreve este banco — e quem o serve tem de o saber.
var ResiduoTolerado = decimal.NewFromFloat(0.05)

// Excede diz se o resíduo passou do tolerado.
func (r Residuo) Excede() bool { return r.Desvio.v.Abs().GreaterThan(ResiduoTolerado) }

// AjustarEncargos ajusta os dois parâmetros às observações.
//
// Resolve-se o sistema 2×2 «reproduzir a TAEG observada no prazo mais curto e no
// mais longo» por quase-Newton, com o Jacobiano obtido uma vez por diferenças
// finitas e reutilizado. É o algoritmo certo porque a resposta da TAEG a
// (antecipado, recorrente) é quase linear na gama plausível — ver o comentário
// dentro da função para o que isso poupou face às bissecções encaixadas que aqui
// estavam.
//
// ⚠️ Ancorar nos dois EXTREMOS, e não fazer mínimos quadrados sobre tudo, é
// escolha: assim as observações do meio ficam livres para serem resíduo. Um
// ajuste que consumisse todas as observações não teria com que se contradizer, e
// era um modelo que nunca podia estar errado — que é o oposto de uma medição.
//
// Devolve os encargos e o resíduo em TODAS as observações, incluindo as duas
// âncoras: nelas o desvio deve ser ~0, e vê-lo é o que confirma que o Newton
// convergiu em vez de se acreditar que convergiu.
func AjustarEncargos(obs []ObservacaoDeEncargo) (Encargos, []Residuo, error) {
	prazos := map[int]bool{}
	for _, o := range obs {
		prazos[o.PrazoMeses] = true
	}
	if len(prazos) < 2 {
		return Encargos{}, nil, fmt.Errorf("%w: %d prazo(s) distinto(s)", ErrPoucasObservacoes, len(prazos))
	}

	ordenadas := slices.Clone(obs)
	slices.SortFunc(ordenadas, func(a, b ObservacaoDeEncargo) int { return a.PrazoMeses - b.PrazoMeses })
	curta, longa := ordenadas[0], ordenadas[len(ordenadas)-1]

	// ⚠️ Sem encargos nenhuns a TAEG já é a anualização efectiva da TAN, e
	// portanto há um PISO abaixo do qual nenhuma repartição chega. Uma TAEG
	// observada abaixo dele não é um banco baratíssimo: é uma leitura errada — a
	// TAN e a TAEG trocadas, ou uma delas de outra linha. Sai como erro.
	semNada := Encargos{}
	piso, err := semNada.TAEGDe(longa.Capital, longa.PrazoMeses, longa.TAN)
	if err != nil {
		return Encargos{}, nil, err
	}
	if piso.v.GreaterThan(longa.TAEG.v) {
		return Encargos{}, nil, fmt.Errorf(
			"%w: a %d meses, sem encargo nenhum a TAEG já seria %s %% e a observada é %s %% — "+
				"nenhuma repartição de encargos desce abaixo do piso da própria TAN",
			ErrEncargosNaoAjustaveis, longa.PrazoMeses, piso, longa.TAEG)
	}

	// O sistema 2×2, resolvido por quase-Newton.
	//
	// ⚠️ Duas bissecções encaixadas era o que aqui estava, e funcionava — e era
	// errado como algoritmo. Custava ~18 iterações externas × ~14 internas = 252
	// resoluções da TAEG por ajuste, e cada uma delas desconta 480 meses: o
	// pacote de testes levava 38 segundos, medido. E é desperdício, não preço: a
	// resposta da TAEG a (antecipado, recorrente) é QUASE LINEAR na gama
	// plausível, portanto uma bissecção — que não usa nenhuma informação sobre a
	// forma da função — está a redescobrir do zero uma relação já conhecida.
	//
	// Com o Jacobiano calculado UMA vez por diferenças finitas e reutilizado, são
	// 4 resoluções para o Jacobiano, 2 para o resíduo inicial e 2 por iteração —
	// tipicamente ~12 no total. Vinte vezes menos.
	residuoEm := func(a, r decimal.Decimal) (curto, longo decimal.Decimal, err error) {
		e := Encargos{Antecipado: RacioDeDecimal(a), Recorrente: TaxaDeDecimal(r)}
		pc, err := e.TAEGDe(curta.Capital, curta.PrazoMeses, curta.TAN)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		pl, err := e.TAEGDe(longa.Capital, longa.PrazoMeses, longa.TAN)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		return pc.v.Sub(curta.TAEG.v), pl.v.Sub(longa.TAEG.v), nil
	}

	a, r := decimal.Zero, decimal.Zero
	fc, fl, err := residuoEm(a, r)
	if err != nil {
		return Encargos{}, nil, err
	}

	// O Jacobiano, por diferenças finitas. Os passos são grandes de propósito —
	// 0,5 % do capital e 0,1 p.p. ao ano são perturbações que movem a TAEG muito
	// acima do ruído de arredondamento, e numa função quase linear um passo grande
	// dá uma derivada melhor do que um passo pequeno.
	fcA, flA, err := residuoEm(a.Add(passoDoAntecipado), r)
	if err != nil {
		return Encargos{}, nil, err
	}
	fcR, flR, err := residuoEm(a, r.Add(passoDoRecorrente))
	if err != nil {
		return Encargos{}, nil, err
	}
	j11 := fcA.Sub(fc).Div(passoDoAntecipado) // ∂curto/∂antecipado
	j12 := fcR.Sub(fc).Div(passoDoRecorrente) // ∂curto/∂recorrente
	j21 := flA.Sub(fl).Div(passoDoAntecipado) // ∂longo/∂antecipado
	j22 := flR.Sub(fl).Div(passoDoRecorrente) // ∂longo/∂recorrente

	// ⚠️ O determinante É a identificabilidade, em número. Ele anula-se
	// exactamente quando as duas observações respondem aos dois encargos na mesma
	// proporção — que é o que acontece quando os prazos são iguais ou quase. Um
	// determinante pequeno não é uma dificuldade numérica a contornar: é o aviso
	// de que a repartição não está determinada pelos dados, e resolver o sistema
	// assim mesmo devolvia a assunção de quem escreveu o código com aparência de
	// medição. Sai como erro, e nomeia os prazos.
	det := j11.Mul(j22).Sub(j12.Mul(j21))
	if det.Abs().LessThan(determinanteMinimo) {
		return Encargos{}, nil, fmt.Errorf(
			"%w: as observações de %d e %d meses respondem aos dois encargos quase na mesma proporção "+
				"(determinante %s), portanto a repartição entre antecipado e recorrente não está "+
				"determinada — é preciso varrer prazos mais afastados",
			ErrEncargosNaoAjustaveis, curta.PrazoMeses, longa.PrazoMeses, det.Round(9))
	}

	for range iteracoesDoAjuste {
		if fc.Abs().LessThan(convergenciaDoAjuste) && fl.Abs().LessThan(convergenciaDoAjuste) {
			break
		}
		// Regra de Cramer sobre J·d = −F.
		dA := fl.Mul(j12).Sub(fc.Mul(j22)).Div(det)
		dR := fc.Mul(j21).Sub(fl.Mul(j11)).Div(det)

		a = limitar(a.Add(dA), decimal.Zero, antecipadoMaximo)
		r = limitar(r.Add(dR), decimal.Zero, recorrenteMaximo)

		if fc, fl, err = residuoEm(a, r); err != nil {
			return Encargos{}, nil, err
		}
	}

	ajustados := Encargos{Antecipado: RacioDeDecimal(a), Recorrente: TaxaDeDecimal(r)}

	// O resíduo mede-se em TODAS as observações, incluindo as duas âncoras — nelas
	// deve ser ~0, e vê-lo confirma que a bissecção convergiu em vez de se
	// acreditar que convergiu.
	residuos := make([]Residuo, 0, len(ordenadas))
	for _, o := range ordenadas {
		prevista, err := ajustados.TAEGDe(o.Capital, o.PrazoMeses, o.TAN)
		if err != nil {
			return Encargos{}, nil, err
		}
		residuos = append(residuos, Residuo{
			PrazoMeses: o.PrazoMeses,
			Observada:  o.TAEG,
			Prevista:   prevista,
			Desvio:     TaxaDeDecimal(prevista.v.Sub(o.TAEG.v).Round(casasDaTaxa)),
		})
	}
	return ajustados, residuos, nil
}

// Os parâmetros numéricos do quase-Newton.
var (
	// Os passos das diferenças finitas: 0,5 % do capital e 0,1 p.p. ao ano.
	// Grandes de propósito — numa função quase linear, um passo grande dá uma
	// derivada melhor do que um passo pequeno, porque o ruído de arredondamento
	// da TAEG (meia milésima) pesa menos no quociente.
	passoDoAntecipado = decimal.NewFromFloat(0.005)
	passoDoRecorrente = decimal.NewFromFloat(0.1)

	// convergenciaDoAjuste é o desvio de TAEG a que se para: 2 décimas de
	// milésima de ponto percentual, abaixo da meia milésima que a terceira casa
	// publicada consegue notar.
	convergenciaDoAjuste = decimal.NewFromFloat(0.0002)

	// determinanteMinimo é o limiar abaixo do qual se declara a repartição não
	// determinada pelos dados. Com os prazos que a grelha varre — os extremos do
	// banco — o determinante medido é da ordem de 1e−2; 1e−6 é folgado o
	// suficiente para nunca recusar um ajuste legítimo e apertado o suficiente
	// para recusar prazos que se tocam.
	determinanteMinimo = decimal.New(1, -6)
)

// iteracoesDoAjuste é o tecto de iterações do quase-Newton. Numa função quase
// linear com o Jacobiano certo, duas ou três bastam; o tecto existe para o ciclo
// ser provadamente finito.
const iteracoesDoAjuste = 12

// limitar prende um valor ao intervalo. O Newton pode dar um passo para fora do
// plausível numa primeira iteração, e prendê-lo é melhor do que abortar: a
// iteração seguinte volta para dentro, e quem julga o resultado é o resíduo.
func limitar(v, minimo, maximo decimal.Decimal) decimal.Decimal {
	if v.LessThan(minimo) {
		return minimo
	}
	if v.GreaterThan(maximo) {
		return maximo
	}
	return v
}

// TAEGDe devolve a TAEG de um crédito de taxa única sob estes encargos. É o
// atalho para o caso de um troço só, que é o da taxa variável pura.
func (e Encargos) TAEGDe(capital Dinheiro, meses int, tan Taxa) (Taxa, error) {
	taeg, _, err := e.Aplicar(capital, []Trecho{{Meses: meses, Anual: tan}})
	return taeg, err
}

// Aplicar devolve a TAEG e o MTIC de um plano sob estes encargos.
//
// ⚠️ É aqui que os dois números que o contrato marca como DERIVADOS se produzem,
// e é o único sítio. Quem os quiser tem de passar por aqui, e portanto tem de ter
// uns Encargos ajustados — não há caminho por onde uma TAEG saia sem modelo.
func (e Encargos) Aplicar(capital Dinheiro, trechos []Trecho) (Taxa, Dinheiro, error) {
	plano, err := PlanoFrances(capital, trechos)
	if err != nil {
		return Taxa{}, Dinheiro{}, err
	}

	antecipado := DinheiroDeDecimal(capital.v.Mul(e.Antecipado.v).Round(centimos))

	// ⚠️ O encargo de cada mês incide sobre o capital em dívida no INÍCIO desse
	// mês — o saldo depois da prestação anterior, e o capital inteiro no primeiro.
	// Usar o saldo do fim do mês subestimaria o encargo, sempre, em todos os
	// meses.
	mensal := e.Recorrente.v.Div(decimal.NewFromInt(100)).Div(decimal.NewFromInt(mesesPorAno))
	comEncargo := make([]Dinheiro, len(plano.Fluxos))
	emDivida := capital.v
	for i, f := range plano.Fluxos {
		encargo := emDivida.Mul(mensal).Round(centimos)
		comEncargo[i] = DinheiroDeDecimal(f.v.Add(encargo))
		emDivida = plano.Saldos[i].v
	}

	liquido := DinheiroDeDecimal(capital.v.Sub(antecipado.v))
	taeg, err := TAEGDeFluxos(liquido, comEncargo)
	if err != nil {
		return Taxa{}, Dinheiro{}, err
	}
	return taeg, MTICDeFluxos(antecipado, comEncargo), nil
}

// Pressupostos são as hipóteses sob que a TAEG e o MTIC foram derivados, em
// português e para uma pessoa ler.
//
// ⚠️ Vão para o campo `pressupostos` do contrato, à parte das `notas`. É o que o
// Anexo I, Parte II e o Anexo II da Directiva 2014/17/UE mandam fazer a um valor
// que depende de hipóteses declaradas: declará-las junto dele. Uma lista vazia
// com uma TAEG preenchida é defeito nosso, e o contrato di-lo.
func (e Encargos) Pressupostos(capital Dinheiro) []string {
	antecipado := DinheiroDeDecimal(capital.v.Mul(e.Antecipado.v).Round(centimos))
	return []string{
		fmt.Sprintf(
			"A TAEG e o MTIC não são valores que o banco tenha devolvido para o seu pedido: " +
				"são calculados por nós. O que o banco publica é medido num cenário de referência, " +
				"com um titular fictício — e a TAEG depende de quem pede, pelo seguro de vida.",
		),
		fmt.Sprintf(
			"Os encargos foram estimados a partir da distância entre a TAN e a TAEG que este banco "+
				"publicou em prazos diferentes, e deram %s € de encargos iniciais (%s %% do montante) "+
				"mais o equivalente a %s %% ao ano sobre o capital que falta pagar.",
			antecipado.ParaPessoa(), percentagemArredondada(e.Antecipado, 2), taxaTexto(e.Recorrente)),
		"Os encargos reais dependem do seguro de vida, que depende da sua idade e do seu estado " +
			"de saúde, e das comissões que o banco lhe aplicar. O valor que vai pagar pode ser " +
			"diferente deste. A TAEG que vincula um banco vem na ficha de informação normalizada, " +
			"por escrito, depois de ele avaliar quem pede.",
	}
}
