package dominio

import (
	"errors"
	"fmt"
	"slices"
)

// As políticas puras: funções de dados para dados, sem rede, sem relógio e sem
// base de dados. É aqui que vive o "ajusta e anota" — e é por isso que devolvem
// o *Ajuste já construído, com a frase lá dentro, em vez de deixarem a nota ao
// cuidado de quem escrever o próximo banco.

var (
	// ErrSemPeriodosValidos: pediu-se um encaixe a um banco que não pratica
	// período fixo nenhum. É defeito de quem chamou, não do pedido.
	ErrSemPeriodosValidos = errors.New("o banco não declara períodos fixos válidos")

	// ErrSemTitulares: um pedido sem titulares não tem idade.
	ErrSemTitulares = errors.New("pedido sem titulares")

	// ErrValorImovelZero: sem valor do imóvel não há LTV. Esta guarda existe
	// para o decimal não entrar em pânico numa divisão por zero.
	ErrValorImovelZero = errors.New("valor do imóvel tem de ser maior do que zero")
)

// EncaixarPeriodoFixo escolhe o período fixo que o banco consegue aplicar.
//
// Não falha por não ter o pedido: prefere descer para o maior válido abaixo e,
// se não houver nenhum abaixo, sobe para o menor. Sem pedido, aplica o mais
// longo. tecto (tipicamente o prazo) filtra os candidatos, mas se filtrar todos
// volta-se à lista inteira — um período fixo maior do que o prazo é problema do
// banco, não razão para não simular.
//
// O *Ajuste é não-nulo exactamente quando o valor aplicado difere do pedido.
func EncaixarPeriodoFixo(validos []int, pedido, tecto *int) (int, *Ajuste, error) {
	if len(validos) == 0 {
		return 0, nil, ErrSemPeriodosValidos
	}

	candidatos := slices.Clone(validos)
	slices.Sort(candidatos)
	candidatos = slices.Compact(candidatos)

	if tecto != nil {
		abaixoDoTecto := make([]int, 0, len(candidatos))
		for _, anos := range candidatos {
			if anos <= *tecto {
				abaixoDoTecto = append(abaixoDoTecto, anos)
			}
		}
		// ⚠️ Se o tecto não deixa nenhum de pé, volta-se à lista inteira em vez
		// de desistir. Um banco cujo período fixo mais curto excede o prazo
		// pedido continua a conseguir simular — e a diferença fica anotada.
		if len(abaixoDoTecto) > 0 {
			candidatos = abaixoDoTecto
		}
	}

	if pedido == nil {
		return candidatos[len(candidatos)-1], nil, nil
	}
	if slices.Contains(candidatos, *pedido) {
		return *pedido, nil, nil
	}

	// Prefere descer: o maior válido abaixo do pedido. Se não houver nenhum
	// abaixo, sobe para o menor — que é o que candidatos[0] já é.
	escolhido := candidatos[0]
	for _, anos := range candidatos {
		if anos < *pedido {
			escolhido = anos
		}
	}
	return escolhido, AjustePeriodoFixo(*pedido, escolhido), nil
}

// EncaixarPrazo limita o prazo pelos limites do banco e pela idade do titular
// mais velho, que não pode ultrapassar IdadeMaximaFim no fim do contrato.
//
// Devolve *ErroOferta com ErroPrazoImpossivel quando nem o prazo mínimo do
// banco cabe na idade — aí não há ajuste que salve, e fingir que há seria pior.
func EncaixarPrazo(pedidoAnos int, r Requisitos, idadeMaisVelho int) (int, *Ajuste, error) {
	porIdade := r.IdadeMaximaFim - idadeMaisVelho
	maximo := min(r.PrazoMax, porIdade)

	if maximo < r.PrazoMin {
		return 0, nil, ErroDePrazo(r.BancoNome, idadeMaisVelho+r.PrazoMin, r.IdadeMaximaFim)
	}

	switch {
	case pedidoAnos > maximo:
		motivo := fmt.Sprintf("o máximo deste banco é de %d anos", r.PrazoMax)
		if porIdade < r.PrazoMax {
			motivo = fmt.Sprintf("o contrato tem de terminar até aos %d anos", r.IdadeMaximaFim)
		}
		return maximo, AjustePrazo(pedidoAnos, maximo, motivo), nil

	case pedidoAnos < r.PrazoMin:
		motivo := fmt.Sprintf("o mínimo deste banco é de %d anos", r.PrazoMin)
		return r.PrazoMin, AjustePrazo(pedidoAnos, r.PrazoMin, motivo), nil
	}

	return pedidoAnos, nil, nil
}

// IdadeMaisVelho devolve a idade do titular mais velho na data hoje. É essa
// que manda no prazo máximo.
func IdadeMaisVelho(ts []Titular, hoje Data) (int, error) {
	if len(ts) == 0 {
		return 0, ErrSemTitulares
	}
	maior := ts[0].DataNascimento.Idade(hoje)
	for _, t := range ts[1:] {
		if idade := t.DataNascimento.Idade(hoje); idade > maior {
			maior = idade
		}
	}
	return maior, nil
}

// LTV é o rácio entre o montante financiado e o valor do imóvel.
func LTV(montante, valorImovel Dinheiro) (Racio, error) {
	if !valorImovel.Positivo() {
		return Racio{}, ErrValorImovelZero
	}
	return Racio{v: montante.v.Div(valorImovel.v)}, nil
}

// ⚠️ O BandaLTV e o MesmaBanda viviam aqui e saíram a 2026-07-26 (KAN-35).
// Não foram corrigidos para outro passo: o que estava errado não era a
// constante dos 5 %, era a ideia de que existe uma banda de passo fixo. As
// fronteiras são medidas banco a banco e nem sempre caem em LTV inteiro — ver
// o escala_ltv.go, que os substitui, e a §4 do ARQUITETURA.md.

// MontanteParaLTV é o inverso do LTV: devolve o montante que dá aquele rácio
// sobre aquele valor de imóvel, arredondado ao cêntimo.
//
// ⚠️ O arredondamento não é detalhe, e é a razão de esta função existir aqui em
// vez de em quem varre. Um crédito pede-se em euros e cêntimos, não em rácios:
// pedir 66,5625 % de 400 000 € dá 266 250,00 € — exacto —, mas pedir 66,5624 %
// de 333 333 € não dá um número redondo, e o rácio que o banco vê passa a ser o
// do montante arredondado e não o que se pediu. Quem mede fronteiras de LTV tem
// de construir a escala sobre o rácio que saiu daqui, e não sobre o que pediu.
func MontanteParaLTV(valorImovel Dinheiro, ltv Racio) Dinheiro {
	return Dinheiro{v: valorImovel.v.Mul(ltv.v).Round(2)}
}
