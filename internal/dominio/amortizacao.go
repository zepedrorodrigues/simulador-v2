package dominio

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// A amortização francesa: prestação constante ao longo de um plano, e o capital
// que sobra a meio dele.
//
// São duas funções e são o «cálculo local» da §4 — a coluna direita da tabela
// «o que é consulta e o que é cálculo». Servem dois sítios com a mesma conta:
// o resíduo do varrimento (§7.4), que compara a prestação do banco com esta, e
// a resposta ao cliente, que a calcula a partir dos parâmetros consultados.
//
// ⚠️ Viveram dentro de um teste até 2026-07-28 — o e2e da CGD, atrás de
// `//go:build rede` —, e é por isso que a §7.4 nunca chegou a ter travão: a
// única implementação da conta que decide se um banco mudou só corria quando
// alguém corria os testes de rede daquele banco.
//
// ⚠️ Nada disto passa por float64, e a razão é a mesma da §4: um resíduo medido
// com vírgula flutuante traz ruído nosso lá dentro, e é o resíduo que decide se
// o banco mudou. Um erro de 0,01 € que ninguém explica é indistinguível do sinal
// que estas funções existem para dar.

// mesesDeUmAnoEmPercentagem converte uma taxa anual em pontos percentuais na
// taxa mensal em fracção: 12 meses × 100 pontos.
var mesesDeUmAnoEmPercentagem = decimal.NewFromInt(1200)

var umDecimal = decimal.NewFromInt(1)

// PrestacaoFrancesa é a prestação constante que amortiza `capital` em
// `mesesDoPlano` meses, à taxa anual dada.
//
// ⚠️ `mesesDoPlano` é o plano **inteiro**, e não a duração da fase. Numa taxa
// mista, a prestação da fase fixa calcula-se sobre o prazo todo — é isso que faz
// dela uma fase de um plano e não um empréstimo de cinco anos. Quem passar aqui
// a duração da fase obtém um número muito maior e perfeitamente plausível.
func PrestacaoFrancesa(capital Dinheiro, anual Taxa, mesesDoPlano int) (Dinheiro, error) {
	if err := validarPlano(capital, anual, mesesDoPlano); err != nil {
		return Dinheiro{}, err
	}

	i := anual.Decimal().Div(mesesDeUmAnoEmPercentagem)
	if i.IsZero() {
		// Sem juro, a prestação é o capital repartido pelos meses. Não é caso de
		// escola: é o que evita a divisão por zero na fórmula abaixo.
		return DinheiroDeDecimal(capital.Decimal().Div(decimal.NewFromInt(int64(mesesDoPlano)))), nil
	}

	fator := potencia(umDecimal.Add(i), mesesDoPlano)
	prestacao := capital.Decimal().Mul(i).Div(umDecimal.Sub(umDecimal.Div(fator)))
	return DinheiroDeDecimal(prestacao), nil
}

// CapitalEmDivida é o que falta pagar ao fim de `pagos` meses de um plano de
// `mesesDoPlano`, à taxa anual dada.
//
// É o que a fase seguinte de uma mista vai amortizar, e é por isso que existe:
// sem ele, a prestação da fase indexada compara-se contra o capital inicial e o
// resíduo aparece enorme por razão nenhuma.
func CapitalEmDivida(capital Dinheiro, anual Taxa, mesesDoPlano, pagos int) (Dinheiro, error) {
	if err := validarPlano(capital, anual, mesesDoPlano); err != nil {
		return Dinheiro{}, err
	}
	if pagos < 0 || pagos > mesesDoPlano {
		return Dinheiro{}, fmt.Errorf("pagaram-se %d meses de um plano de %d", pagos, mesesDoPlano)
	}

	i := anual.Decimal().Div(mesesDeUmAnoEmPercentagem)
	if i.IsZero() {
		emFalta := decimal.NewFromInt(int64(mesesDoPlano - pagos))
		return DinheiroDeDecimal(
			capital.Decimal().Mul(emFalta).Div(decimal.NewFromInt(int64(mesesDoPlano)))), nil
	}

	fatorTotal := potencia(umDecimal.Add(i), mesesDoPlano)
	fatorPagos := potencia(umDecimal.Add(i), pagos)
	return DinheiroDeDecimal(
		capital.Decimal().Mul(fatorTotal.Sub(fatorPagos)).Div(fatorTotal.Sub(umDecimal))), nil
}

// validarPlano recusa o que não é um plano.
//
// ⚠️ Devolve erro em vez de devolver zero. Um plano de zero meses ou uma taxa
// negativa é erro de quem chama, e um zero silencioso viraria um resíduo de
// centenas de euros que se leria como «o banco mudou».
func validarPlano(capital Dinheiro, anual Taxa, mesesDoPlano int) error {
	if mesesDoPlano < 1 {
		return fmt.Errorf("plano de %d meses", mesesDoPlano)
	}
	if !capital.Positivo() {
		return fmt.Errorf("capital de %s", capital)
	}
	if anual.Decimal().IsNegative() {
		return fmt.Errorf("taxa anual de %s", anual)
	}
	return nil
}

// potencia eleva a um expoente inteiro não negativo, por multiplicação
// sucessiva.
//
// ⚠️ Não passa por math.Pow, e o custo é conhecido: 480 multiplicações no plano
// mais longo que os bancos praticam (40 anos). É o preço de o resultado não
// trazer ruído de float — ver o comentário do topo.
func potencia(base decimal.Decimal, expoente int) decimal.Decimal {
	resultado := decimal.NewFromInt(1)
	for range expoente {
		resultado = resultado.Mul(base)
	}
	return resultado
}
