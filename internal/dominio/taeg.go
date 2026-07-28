package dominio

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// A TAEG, pelo Anexo I da Directiva 2014/17/UE (MCD).
//
// A definição é uma equação e não uma fórmula: a TAEG é a taxa X que iguala, na
// data zero, o valor actualizado do que se recebe ao valor actualizado do que se
// paga. Não se resolve em fechado — resolve-se por procura.
//
//	Σ Cₖ (1+X)^(−tₖ)  =  Σ Dₗ (1+X)^(−sₗ)
//
// ⚠️ Trabalha-se em MESES e converte-se no fim, e isso não é atalho: com
// intervalos mensais iguais, (1+X)^(−m/12) é exactamente (1+j)^(−m) para
// 1+X = (1+j)¹², portanto a equação é a mesma. O que se ganha é decisivo — o
// expoente passa a ser inteiro. Com expoentes fraccionários seria preciso
// PowWithPrecision a cada termo de cada iteração, que é lento e traz erro onde
// a aritmética podia ser exacta.
//
// ⚠️ Este ficheiro não sabe o que é um encargo. Recebe fluxos e devolve a taxa
// que os explica. Quem decide que fluxos existem — e sob que hipóteses — é o
// encargos.go, e a fronteira é deliberada: a definição legal da TAEG não é uma
// hipótese nossa, e não deve estar no mesmo sítio que as nossas hipóteses.

var (
	// ErrSemFluxos: sem pagamentos não há taxa que os iguale a nada.
	ErrSemFluxos = errors.New("não há fluxos para calcular a TAEG")

	// ErrLiquidoNaoPositivo: se o capital líquido recebido não for positivo, os
	// encargos comeram o empréstimo e a equação não tem raiz com sentido.
	ErrLiquidoNaoPositivo = errors.New("o capital líquido recebido tem de ser maior do que zero")

	// ErrTAEGForaDoIntervalo: a raiz não está no intervalo procurado. É defeito
	// de dados — fluxos que não correspondem ao capital — e não um produto
	// exótico. Devolve-se erro em vez de se devolver o extremo do intervalo:
	// uma TAEG que é o limite da procura é um número errado com ar de certo.
	ErrTAEGForaDoIntervalo = errors.New("a TAEG não está no intervalo procurado")
)

// taxaMensalMaxima é o tecto da procura, em fracção mensal.
//
// 1 % ao mês é ~12,7 % ao ano de TAEG; 10 % ao mês é ~213 %. O tecto está em
// 10 % ao mês, que é absurdo para crédito à habitação de propósito: o intervalo
// da bissecção tem de conter a raiz com folga, e o que se paga por o exagerar
// são quatro iterações. O que se pagava por o apertar era um erro em vez de um
// número, no dia em que um banco publicasse um produto caro.
var taxaMensalMaxima = decimal.NewFromFloat(0.10)

// precisaoDoDesconto é a precisão com que o factor de desconto acumulado se
// mantém ao longo dos meses.
//
// ⚠️ Vinte e quatro casas, e o arredondamento a cada mês é obrigatório e não
// zelo. O decimal.Mul é EXACTO — os expoentes somam-se e os coeficientes
// multiplicam-se —, portanto vⁿ calculado por multiplicação sucessiva sem
// arredondar teria, ao mês 480, um coeficiente de milhares de dígitos: um
// big.Int a crescer sem limite dentro do ciclo mais interno da bissecção.
// Arredondar a 24 casas custa ~1e−24 por mês, ~5e−22 ao fim de 480 — onze ordens
// de grandeza abaixo do que uma TAEG publicada a três casas consegue notar.
const precisaoDoDesconto = 24

// iteracoesDaBisseccao é o tecto de iterações.
//
// A bissecção parte um intervalo de largura 0,1 ao meio a cada passo: 60
// iterações levam-no a ~1e−19, muito abaixo do necessário para três casas na
// TAEG anual. O tecto é 100 e existe para o ciclo ser provadamente finito, não
// porque se espere chegar lá.
const iteracoesDaBisseccao = 100

// toleranciaDaBisseccao é a largura de intervalo a partir da qual se para.
//
// ⚠️ 1e−11 na taxa MENSAL, o que dá ~1,2e−10 na anual: sete ordens de grandeza
// abaixo da meia milésima de ponto percentual que a terceira casa da TAEG
// publicada consegue notar. Parte de uma largura de 0,1, portanto são ~33
// iterações.
//
// Não é o menor número que caiba num decimal, e isso é deliberado: este solver
// corre dentro das duas bissecções do ajuste dos encargos, portanto o seu custo
// multiplica-se por elas. Com 1e−15 aqui — 47 iterações — cada ajuste levava mais
// de cinco segundos, medido. Precisão que ninguém pode ler paga-se em tempo que
// todos esperam.
var toleranciaDaBisseccao = decimal.New(1, -11)

// TAEGDeFluxos devolve a TAEG que iguala os fluxos ao capital líquido recebido.
//
//   - liquido é o que a pessoa recebe de facto: o montante menos os encargos
//     que paga no início. É a forma (b) do Anexo I — descontar os encargos
//     antecipados do capital em vez de os somar aos pagamentos —, e as duas dão
//     exactamente o mesmo X;
//   - fluxos são os pagamentos mensais, pela ordem, um por mês, JÁ com os
//     encargos recorrentes somados à prestação.
//
// O resultado vem em pontos percentuais anuais, como qualquer Taxa deste
// domínio: 3,412 é 3,412 %.
func TAEGDeFluxos(liquido Dinheiro, fluxos []Dinheiro) (Taxa, error) {
	if len(fluxos) == 0 {
		return Taxa{}, ErrSemFluxos
	}
	if !liquido.Positivo() {
		return Taxa{}, fmt.Errorf("%w: %s", ErrLiquidoNaoPositivo, liquido)
	}

	// f(j) = Σ fluxoₘ·(1+j)^(−m) − liquido.
	//
	// ⚠️ É estritamente decrescente em j: cada termo decresce e a subtracção é
	// constante. É essa monotonia — e não uma esperança sobre a forma da função —
	// que faz a bissecção convergir para a única raiz. Uma raiz que não seja
	// única não existe aqui, e é por isso que não se usa Newton: não é preciso
	// derivada, e não há ponto de partida que possa divergir.
	f := func(j decimal.Decimal) decimal.Decimal {
		return valorActual(fluxos, j).Sub(liquido.v)
	}

	baixo, alto := decimal.Zero, taxaMensalMaxima

	// A juro zero, o valor actual é a soma nominal dos pagamentos. Se nem assim
	// alcança o capital, a pessoa paga menos do que recebeu: não há TAEG ≥ 0.
	if f(baixo).IsNegative() {
		return Taxa{}, fmt.Errorf(
			"%w: a soma dos pagamentos (%s) é inferior ao capital líquido (%s), e não há taxa não-negativa que os iguale",
			ErrTAEGForaDoIntervalo, valorActual(fluxos, decimal.Zero).Round(centimos), liquido)
	}
	if f(alto).Sign() > 0 {
		return Taxa{}, fmt.Errorf(
			"%w: nem %s %% ao mês desconta os pagamentos até ao capital líquido (%s)",
			ErrTAEGForaDoIntervalo, taxaMensalMaxima.Mul(decimal.NewFromInt(100)), liquido)
	}

	for range iteracoesDaBisseccao {
		if alto.Sub(baixo).LessThan(toleranciaDaBisseccao) {
			break
		}
		meio := baixo.Add(alto).Div(decimal.NewFromInt(2))
		if f(meio).Sign() > 0 {
			baixo = meio
		} else {
			alto = meio
		}
	}

	return anualDeMensal(baixo.Add(alto).Div(decimal.NewFromInt(2)))
}

// MTICDeFluxos é o Montante Total Imputado ao Consumidor: tudo o que a pessoa
// paga, sem actualizar nada.
//
// ⚠️ Ao contrário da TAEG, isto é uma soma e não uma equação — os encargos
// antecipados entram porque são pagos, e não se descontam do capital como na
// forma (b). Dar-lhe o `liquido` em vez do antecipado daria um MTIC menor do que
// a verdade, e é o género de engano que passa despercebido porque o número
// continua grande.
func MTICDeFluxos(antecipado Dinheiro, fluxos []Dinheiro) Dinheiro {
	total := antecipado.v
	for _, f := range fluxos {
		total = total.Add(f.v)
	}
	return DinheiroDeDecimal(total.Round(centimos))
}

// valorActual desconta os fluxos à taxa mensal j.
//
// O factor de desconto acumula-se por multiplicação em vez de se elevar a cada
// mês: 480 multiplicações em vez de 480 exponenciações, dentro do ciclo da
// bissecção. Ver precisaoDoDesconto para o porquê do arredondamento.
func valorActual(fluxos []Dinheiro, j decimal.Decimal) decimal.Decimal {
	um := decimal.NewFromInt(1)

	// v = 1/(1+j), o factor de um mês.
	v := um
	if !j.IsZero() {
		v = um.DivRound(um.Add(j), precisaoDoDesconto)
	}

	soma := decimal.Zero
	acumulado := um
	for _, f := range fluxos {
		acumulado = acumulado.Mul(v).Round(precisaoDoDesconto)
		soma = soma.Add(f.v.Mul(acumulado))
	}
	return soma
}

// anualDeMensal converte a taxa mensal em fracção para a anual equivalente em
// pontos percentuais: X = ((1+j)¹² − 1) × 100.
//
// ⚠️ É a capitalização e não a multiplicação por doze. O Anexo I define a TAEG
// como taxa EFECTIVA anual, e j×12 daria a nominal — a diferença numa TAEG de
// 3,4 % é de cerca de 0,05 p.p., que é maior do que a casa decimal que o
// contrato publica. Foi por isto que o mensalDe do amortizacao.go divide por
// doze e este multiplica por composição: são convenções diferentes porque
// descrevem coisas diferentes, e trocá-las é um erro que não se vê no resultado.
func anualDeMensal(j decimal.Decimal) (Taxa, error) {
	um := decimal.NewFromInt(1)
	anual, err := um.Add(j).PowInt32(mesesPorAno)
	if err != nil {
		return Taxa{}, fmt.Errorf("(1+j)^12 para j=%s: %w", j, err)
	}
	return TaxaDeDecimal(anual.Sub(um).Mul(decimal.NewFromInt(100)).Round(casasDaTaxa)), nil
}

// casasDaTaxa são as casas decimais com que uma taxa se publica. É o numeric(6,3)
// de catalogo_taxas e o que o contrato devolve.
const casasDaTaxa = 3
