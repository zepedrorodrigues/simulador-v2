package dominio

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// A amortização francesa: prestação constante dentro de cada troço, juro
// decrescente e capital crescente. É a forma que os bancos portugueses praticam
// no crédito à habitação, e é o que reproduz a prestação que eles devolvem —
// está conferido contra a CGD ao vivo (o TestE2E... verifica a prestação
// observada contra esta fórmula, com o mesmo capital e a mesma taxa).
//
// Este ficheiro é o motor de CÁLCULO da resposta local, e é aqui que a inversão
// da §1 se paga: a prestação de um cliente não vem de um banco, deriva-se da
// taxa medida e do montante que ele pediu. É aritmética exacta sobre uma
// medição, e não uma estimativa.
//
// ⚠️ NÃO decide fases. Recebe uma lista de troços — quantos, com que duração e
// com que taxa — e desenrola-a. Se uma taxa `fixa` é um troço só do prazo todo
// ou dois troços, e o que a `mista` faz depois do período fixo, é decisão de
// quem chama, e não deste ficheiro: essa semântica é do vocabulário do produto,
// muda de banco para banco (o Montepio aproxima a fixa com mista do prazo todo)
// e não tem lugar no motor. Um troço só dá exactamente o mesmo resultado que a
// fórmula de sempre, portanto a taxa variável pura não paga nada por esta
// generalidade.

// mesesPorAno e centimos são as duas constantes de escala. Estão nomeadas
// porque um 12 e um 2 soltos no meio de uma fórmula financeira não se
// distinguem de um erro.
const (
	mesesPorAno = 12
	centimos    = 2
)

var (
	// ErrCapitalNaoAmortizavel: sem capital positivo não há nada a amortizar, e
	// a fórmula dividiria por um denominador construído a partir dele.
	ErrCapitalNaoAmortizavel = errors.New("capital tem de ser maior do que zero")

	// ErrPrazoNaoAmortizavel: um plano de zero meses — ou negativo — não é um
	// plano curto, é um erro de quem o montou.
	ErrPrazoNaoAmortizavel = errors.New("o plano tem de ter pelo menos um mês")

	// ErrTaxaNegativa: a Euribor negativa é histórica e real, mas uma TAN
	// negativa faria o banco pagar a quem pede. Se aparecer, é sinal de leitura
	// errada, não de um produto novo — e passar isto à fórmula dava uma
	// prestação menor do que o capital dividido pelos meses, sem nada a dizê-lo.
	ErrTaxaNegativa = errors.New("taxa anual negativa")
)

// Trecho é um troço de plano: quantos meses, e a que taxa anual.
//
// ⚠️ Meses é DURAÇÃO e não acumulado, ao contrário do Fase.AteMes. É a mesma
// distinção que o FaseDuracao já faz, e pela mesma razão: é assim que os bancos
// declaram, e a conversão faz-se num sítio só.
type Trecho struct {
	Meses int
	Anual Taxa
}

// Plano é o desenrolar completo de um crédito.
type Plano struct {
	// Fases é o plano como o contrato o publica: acumulado, com a prestação
	// nominal de cada troço.
	Fases []Fase

	// Fluxos são as prestações mês a mês, pela ordem, uma por mês do prazo.
	//
	// ⚠️ Existem porque a TAEG se calcula sobre fluxos e não sobre fases: o
	// Anexo I da Directiva 2014/17/UE define-a como a taxa que iguala o valor
	// actualizado dos pagamentos ao do capital, e para isso é preciso cada
	// pagamento com a sua data. Uma fase diz «1 234,56 € durante 120 meses»,
	// o que basta para mostrar, e não basta para actualizar.
	Fluxos []Dinheiro
}

// Meses é a duração total do plano.
func (p Plano) Meses() int { return len(p.Fluxos) }

// PrestacaoFrancesa devolve a prestação constante que amortiza o capital em
// `meses` à taxa anual dada, arredondada ao cêntimo.
//
// ⚠️ Arredonda ao cêntimo, e não é cosmética: é o valor que o banco cobra, e é
// sobre ele que o plano tem de correr. Guardar a prestação com dezasseis casas e
// só arredondar na apresentação dava um plano que fecha exactamente a zero no
// papel e não fecha na conta bancária de ninguém.
func PrestacaoFrancesa(capital Dinheiro, anual Taxa, meses int) (Dinheiro, error) {
	if !capital.Positivo() {
		return Dinheiro{}, fmt.Errorf("%w: %s", ErrCapitalNaoAmortizavel, capital)
	}
	if meses < 1 {
		return Dinheiro{}, fmt.Errorf("%w: %d meses", ErrPrazoNaoAmortizavel, meses)
	}
	if anual.v.IsNegative() {
		return Dinheiro{}, fmt.Errorf("%w: %s", ErrTaxaNegativa, anual)
	}

	i := mensalDe(anual)

	// ⚠️ Taxa zero não é um caso a mais: é o caso em que a fórmula geral divide
	// por zero. Com i = 0 o denominador 1-(1+i)^-n é exactamente 0, e a
	// amortização é o capital repartido pelos meses.
	if i.IsZero() {
		return DinheiroDeDecimal(capital.v.DivRound(decimal.NewFromInt(int64(meses)), centimos)), nil
	}

	um := decimal.NewFromInt(1)
	fator, err := um.Add(i).PowInt32(int32(meses)) //nolint:gosec // meses vem de um prazo em anos × 12
	if err != nil {
		return Dinheiro{}, fmt.Errorf("(1+i)^%d: %w", meses, err)
	}

	// p = C · i / (1 − (1+i)^−n)
	denominador := um.Sub(um.Div(fator))
	if denominador.IsZero() {
		// Inalcançável com i > 0 e n ≥ 1, e afirma-se em vez de se assumir: o
		// custo é um if e o que evita é uma divisão por zero num pânico do
		// decimal, a meio de um pedido de um cliente.
		return Dinheiro{}, fmt.Errorf("denominador nulo para %d meses à taxa %s", meses, anual)
	}
	return DinheiroDeDecimal(capital.v.Mul(i).Div(denominador).Round(centimos)), nil
}

// PlanoFrances desenrola os troços e devolve o plano completo.
//
// A prestação recalcula-se em CADA mudança de taxa, sobre o capital que resta e
// os meses que faltam — que é o que os bancos fazem e o que a prestação da fase
// indexada de uma mista significa.
//
// ⚠️ O último pagamento é o saldo que resta mais o juro desse mês, e não a
// prestação nominal. A diferença é de cêntimos e existe porque a prestação está
// arredondada: ao longo de 480 meses o arredondamento acumula, e um plano que
// terminasse com a prestação nominal deixaria um saldo residual — dívida ou
// crédito — que ninguém explicaria. É a mesma correcção que os bancos fazem na
// última prestação.
func PlanoFrances(capital Dinheiro, trechos []Trecho) (Plano, error) {
	if !capital.Positivo() {
		return Plano{}, fmt.Errorf("%w: %s", ErrCapitalNaoAmortizavel, capital)
	}
	if len(trechos) == 0 {
		return Plano{}, fmt.Errorf("%w: plano sem troços", ErrPrazoNaoAmortizavel)
	}

	total := 0
	for n, t := range trechos {
		if t.Meses < 1 {
			return Plano{}, fmt.Errorf("%w: troço %d com %d meses", ErrPrazoNaoAmortizavel, n+1, t.Meses)
		}
		if t.Anual.v.IsNegative() {
			return Plano{}, fmt.Errorf("%w: troço %d a %s", ErrTaxaNegativa, n+1, t.Anual)
		}
		total += t.Meses
	}

	plano := Plano{
		Fases:  make([]Fase, 0, len(trechos)),
		Fluxos: make([]Dinheiro, 0, total),
	}

	saldo := capital.v
	restantes := total
	acumulado := 0

	for n, t := range trechos {
		// ⚠️ Sobre os meses que FALTAM, não sobre os do troço. A prestação da
		// fase fixa de uma mista de 30 anos com 5 anos fixos é a que amortizaria
		// o capital em 30 anos àquela taxa — não em 5. Calcular sobre a duração
		// do troço dava uma prestação várias vezes maior, com ar de plausível.
		nominal, err := PrestacaoFrancesa(DinheiroDeDecimal(saldo), t.Anual, restantes)
		if err != nil {
			return Plano{}, fmt.Errorf("troço %d: %w", n+1, err)
		}

		i := mensalDe(t.Anual)
		for m := range t.Meses {
			juro := saldo.Mul(i).Round(centimos)
			ultimo := acumulado+m+1 == total

			pago := nominal.v
			if ultimo {
				// Fecha exactamente: o que resta, mais o juro do mês.
				pago = saldo.Add(juro).Round(centimos)
			}

			saldo = saldo.Add(juro).Sub(pago)
			plano.Fluxos = append(plano.Fluxos, DinheiroDeDecimal(pago))
		}

		acumulado += t.Meses
		restantes -= t.Meses
		plano.Fases = append(plano.Fases, Fase{
			AteMes:    acumulado,
			Taxa:      t.Anual,
			Prestacao: nominal,
		})
	}

	// Afirma-se em vez de se prometer: o último pagamento foi construído para
	// fechar, e se não fechou o defeito está aqui e não na apresentação.
	if !saldo.Round(centimos).IsZero() {
		return Plano{}, fmt.Errorf(
			"o plano de %d meses fechou com um saldo de %s em vez de zero", total, saldo.Round(centimos))
	}
	return plano, nil
}

// SaldoApos devolve o capital em dívida depois de `pagas` prestações de um plano
// de taxa única.
//
// Serve quem precisa do capital que resta sem desenrolar o plano todo — a
// amortização antecipada, e o capital com que a fase indexada de uma mista
// começa. A fórmula fechada é B = C·(1+i)^k − p·((1+i)^k − 1)/i.
func SaldoApos(capital Dinheiro, anual Taxa, meses, pagas int) (Dinheiro, error) {
	if pagas < 0 || pagas > meses {
		return Dinheiro{}, fmt.Errorf("%w: %d prestações pagas de um plano de %d meses",
			ErrPrazoNaoAmortizavel, pagas, meses)
	}
	p, err := PrestacaoFrancesa(capital, anual, meses)
	if err != nil {
		return Dinheiro{}, err
	}
	if pagas == 0 {
		return capital, nil
	}

	i := mensalDe(anual)
	if i.IsZero() {
		return DinheiroDeDecimal(capital.v.Sub(p.v.Mul(decimal.NewFromInt(int64(pagas))))), nil
	}

	um := decimal.NewFromInt(1)
	fator, err := um.Add(i).PowInt32(int32(pagas)) //nolint:gosec // pagas ≤ meses, que vem de anos × 12
	if err != nil {
		return Dinheiro{}, fmt.Errorf("(1+i)^%d: %w", pagas, err)
	}

	crescido := capital.v.Mul(fator)
	amortizado := p.v.Mul(fator.Sub(um)).Div(i)
	return DinheiroDeDecimal(crescido.Sub(amortizado).Round(centimos)), nil
}

// mensalDe converte uma taxa anual em pontos percentuais na taxa mensal em
// fracção: 3,25 % ao ano dá 0,0027083…
//
// ⚠️ É a divisão por 12 e não a raiz de índice 12. A taxa mensal equivalente a
// uma anual nominal, no crédito à habitação, é a nominal a dividir por doze —
// é o que a convenção do mercado usa e é o que reproduz a prestação que os
// bancos devolvem. A raiz daria a taxa efectiva, um número diferente, e a
// prestação calculada com ela não bateria com nenhum simulador.
func mensalDe(anual Taxa) decimal.Decimal {
	return anual.v.
		Div(decimal.NewFromInt(100)).
		Div(decimal.NewFromInt(mesesPorAno))
}
