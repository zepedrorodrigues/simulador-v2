package dominio

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// A escala de LTV de um banco: a função em degraus que leva um rácio ao spread.
//
// ⚠️ As fronteiras são MEDIDAS banco a banco, e não uma constante do domínio.
// Substituiu as vinte bandas fixas de 5 % na KAN-35; os números que o obrigaram
// — fronteiras fora dos inteiros, o erro que não diminui com o passo, o patamar
// não monótono — estão na §4 do ARQUITETURA.md.
//
// Este pacote define o que a escala é e o que se lhe pergunta. Quem a preenche
// a partir do varrimento é o aplicacao/grelha.

var (
	// ErrEscalaVazia: uma escala sem degraus não responde a nada.
	ErrEscalaVazia = errors.New("escala de LTV sem degraus")

	// ErrLTVForaDaEscala: o rácio está fora do que o banco preça. Quem
	// constrói a Oferta traduz isto para ErroProdutoIndisponivel — é
	// literalmente o caso que esse código nomeia («LTV fora da banda que
	// pratica»).
	ErrLTVForaDaEscala = errors.New("LTV fora da escala que o banco pratica")
)

// DegrauLTV é um intervalo de LTV com um spread medido.
//
// O intervalo é fechado em cima e aberto em baixo — (De, Ate] —, excepto o
// primeiro da escala, que inclui o seu De. ⚠️ Não é convenção arbitrária: está
// medido no Novo Banco, onde 50,00 % ainda leva o preço de baixo e 50,25 % já
// leva o de cima. É a leitura literal de «LTV até 50 %».
type DegrauLTV struct {
	De, Ate Racio

	// Spread é o spread medido do degrau.
	//
	// ⚠️ Num degrau NÃO resolvido este é o mais alto dos dois lados, nunca o
	// mais baixo, e o construtor recusa o contrário. É o que prescreve o Anexo
	// I, Parte II, alínea (d) da Directiva 2014/17/UE (MCD) para «vários
	// valores possíveis»: presume-se a taxa mais alta.
	Spread Taxa

	// SpreadMinimo é o outro lado de um degrau não resolvido: sabe-se que o
	// preço muda aqui dentro, e sabe-se entre que dois valores, mas não onde.
	// Nulo num degrau resolvido.
	//
	// ⚠️ Guardar os dois lados, em vez de uma flag, é o que torna a nota
	// verificável: a frase que a pessoa lê nomeia os dois números, e sem eles
	// diria apenas «é aproximado», que não é informação.
	SpreadMinimo *Taxa
}

// Resolvido diz se se sabe o preço em todo o degrau. Deriva de SpreadMinimo
// para não existir o estado impossível «não resolvido sem o outro lado».
func (d DegrauLTV) Resolvido() bool { return d.SpreadMinimo == nil }

// EscalaDeLTV é a função em degraus de um banco: ordenada, contígua e sem
// buracos nem sobreposições.
//
// Os degraus são não exportados de propósito. Uma escala só se constrói pelo
// NovaEscalaDeLTV, que valida — e assim uma escala com buracos não é
// representável, à imagem do que o ValidarFases faz ao plano de prestações.
type EscalaDeLTV struct {
	degraus []DegrauLTV
}

// NovaEscalaDeLTV valida e constrói. Os degraus têm de vir por ordem crescente
// e encostados uns aos outros: o Ate de cada um é o De do seguinte.
func NovaEscalaDeLTV(degraus []DegrauLTV) (EscalaDeLTV, error) {
	if len(degraus) == 0 {
		return EscalaDeLTV{}, ErrEscalaVazia
	}

	for i, d := range degraus {
		if d.De.Cmp(d.Ate) >= 0 {
			return EscalaDeLTV{}, fmt.Errorf(
				"degrau %d vai de %s a %s, que não é um intervalo",
				i+1, percentagem(d.De), percentagem(d.Ate))
		}
		if !d.Resolvido() && d.SpreadMinimo.Cmp(d.Spread) >= 0 {
			// ⚠️ Um degrau por resolver cujo Spread não fosse o mais alto
			// serviria o lado barato em silêncio, que é exactamente o que a
			// MCD proíbe e o que esta issue existe para não deixar acontecer.
			return EscalaDeLTV{}, fmt.Errorf(
				"degrau %d não está resolvido e o spread servido (%s) não é o mais alto dos dois lados (%s)",
				i+1, d.Spread, d.SpreadMinimo)
		}
		if i > 0 && !degraus[i-1].Ate.Equal(d.De) {
			return EscalaDeLTV{}, fmt.Errorf(
				"degrau %d começa em %s e o anterior acabou em %s: a escala tem um buraco ou uma sobreposição",
				i+1, percentagem(d.De), percentagem(degraus[i-1].Ate))
		}
	}

	copia := make([]DegrauLTV, len(degraus))
	copy(copia, degraus)
	return EscalaDeLTV{degraus: copia}, nil
}

// Degraus devolve os degraus da escala, por ordem.
func (e EscalaDeLTV) Degraus() []DegrauLTV {
	saida := make([]DegrauLTV, len(e.degraus))
	copy(saida, e.degraus)
	return saida
}

// SpreadEm devolve o spread que o banco pratica naquele LTV, e a nota que tem
// de acompanhá-lo.
//
// ⚠️ A nota é vazia num degrau resolvido e OBRIGATÓRIA num degrau por resolver
// — aí o spread devolvido é o mais alto do intervalo e a frase diz que é um
// limite superior. Devolver os dois juntos, como o EncaixarPeriodoFixo devolve
// o *Ajuste ao lado do valor, é o que impede o modo de falha da §7.4: um número
// errado com ar de certo. Quem chama passa a nota ao Oferta.Anotar.
//
// Fora da escala devolve ErrLTVForaDaEscala: o banco não preça ali, e inventar
// o degrau mais próximo seria servir o preço de outro cliente.
func (e EscalaDeLTV) SpreadEm(ltv Racio) (Taxa, string, error) {
	if len(e.degraus) == 0 {
		return Taxa{}, "", ErrEscalaVazia
	}

	primeiro, ultimo := e.degraus[0], e.degraus[len(e.degraus)-1]
	if ltv.Cmp(primeiro.De) < 0 || ltv.Cmp(ultimo.Ate) > 0 {
		return Taxa{}, "", fmt.Errorf(
			"%w: %s está fora de %s a %s",
			ErrLTVForaDaEscala, percentagem(ltv), percentagem(primeiro.De), percentagem(ultimo.Ate))
	}

	// O primeiro degrau cujo Ate alcança o rácio. Como os degraus são contíguos
	// e fechados em cima, é este o que fecha «até X %» com o X lá dentro.
	for _, d := range e.degraus {
		if ltv.Cmp(d.Ate) > 0 {
			continue
		}
		if d.Resolvido() {
			return d.Spread, "", nil
		}
		return d.Spread, fmt.Sprintf(
			"O preço deste banco muda algures entre um LTV de %s %% e %s %%, e não se conseguiu medir onde. "+
				"Usou-se o spread mais alto do intervalo (%s %%) e não o mais baixo (%s %%): "+
				"o valor apresentado é um limite superior, e não pagará mais do que isto.",
			percentagem(d.De), percentagem(d.Ate), taxaTexto(d.Spread), taxaTexto(*d.SpreadMinimo)), nil
	}

	// Inalcançável: o rácio está dentro dos extremos e os degraus são
	// contíguos, logo algum tem de o conter.
	return Taxa{}, "", fmt.Errorf("%w: %s", ErrLTVForaDaEscala, percentagem(ltv))
}

// percentagem escreve um rácio em pontos percentuais e com vírgula decimal,
// para as frases que as pessoas lêem: 0.6675 dá "66,75".
// ⚠️ NÃO arredonda, e é para as fronteiras de LTV: elas são medidas e têm até
// sete casas (0,6659375 na CGD). Arredondá-las aqui deitava fora ~18 pedidos por
// banco de refinamento. Para uma fracção que não é fronteira — a dos encargos —
// há a percentagemArredondada (KAN-51).
func percentagem(r Racio) string {
	return virgula(r.v.Mul(decimal.NewFromInt(100)).String())
}

// percentagemArredondada escreve uma fracção como percentagem com um número
// escolhido de casas. É para valores em que a precisão não é o ponto — uma
// hipótese declarada de encargos não ganha nada com catorze casas, e perde: um
// número assim ensina a saltar o parágrafo em que está.
func percentagemArredondada(r Racio, casas int32) string {
	return virgula(r.v.Mul(decimal.NewFromInt(100)).Round(casas).String())
}

// taxaTexto escreve uma taxa com vírgula decimal. A Taxa já está em pontos
// percentuais, por isso não se multiplica por nada.
func taxaTexto(t Taxa) string { return virgula(t.v.String()) }

func virgula(s string) string { return strings.Replace(s, ".", ",", 1) }
