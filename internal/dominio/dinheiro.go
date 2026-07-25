package dominio

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Os três tipos numéricos do domínio. São finos — embrulham um
// decimal.Decimal e mais nada — e existem por duas razões que valem cada linha
// de cerimónia que custam.
//
// A primeira é que == sobre um decimal.Decimal compila e mente. O decimal
// guarda um ponteiro para um big.Int, por isso == compara ponteiros: medido,
// dá falso entre decimal.NewFromInt(1) e decimal.RequireFromString("1.00"), e
// dá falso até entre dois decimal.NewFromInt(1). Nenhum linter do portão
// apanha isso. O campo [0]func() à cabeça de cada tipo torna a comparação um
// erro de compilação:
//
//	invalid operation: a == b (struct containing [0]func() cannot be compared)
//
// ⚠️ O campo vem PRIMEIRO de propósito. No fim da struct, o Go acrescenta
// enchimento e o tipo passa de 16 para 24 bytes — medido. À cabeça, custa zero.
//
// A segunda razão é a unidade. O contrato publica `tan: 3.25` (pontos
// percentuais) e `ltv: 0.8` (fracção) lado a lado, e são grandezas diferentes
// com a mesma aparência. Tipos distintos fazem dessa confusão — e de somar uma
// taxa a um montante — um erro de compilação em vez de um número errado.

// Dinheiro é um montante em euros.
type Dinheiro struct {
	_ [0]func()
	v decimal.Decimal
}

// DinheiroDeTexto lê um montante já normalizado ("250000", "1303.23").
//
// ⚠️ Normalizado quer dizer com ponto decimal e sem separador de milhares. A
// tradução dos formatos dos bancos — vírgula decimal, espaço não-quebrável,
// e o anglo-saxónico do Bankinter — é trabalho de cada banco, no seu
// resposta.go, e nunca passa por float64.
func DinheiroDeTexto(s string) (Dinheiro, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Dinheiro{}, fmt.Errorf("montante inválido %q: %w", s, err)
	}
	return Dinheiro{v: d}, nil
}

// DinheiroDeDecimal é a porta de entrada a partir da fronteira da base de
// dados (pgtype.Numeric) e dos parsers dos bancos.
func DinheiroDeDecimal(d decimal.Decimal) Dinheiro { return Dinheiro{v: d} }

// DinheiroDeInteiro constrói a partir de um número inteiro de euros.
func DinheiroDeInteiro(n int64) Dinheiro { return Dinheiro{v: decimal.NewFromInt(n)} }

// Equal diz se os dois montantes são o mesmo número, independentemente da
// escala com que foram escritos: 100 e 100.00 são iguais.
//
// O nome é em inglês de propósito, como String ou Error: é o que o go-cmp
// procura por reflexão, e com ele o cmp.Diff funciona nos testes sem
// cmp.Comparer nenhum.
func (d Dinheiro) Equal(o Dinheiro) bool { return d.v.Equal(o.v) }

// Cmp devolve -1, 0 ou 1, como o strings.Compare.
func (d Dinheiro) Cmp(o Dinheiro) int { return d.v.Cmp(o.v) }

// Positivo diz se o montante é estritamente maior do que zero. É a pergunta
// que a validação do Pedido faz, e a que evita uma divisão por zero.
func (d Dinheiro) Positivo() bool { return d.v.IsPositive() }

// Decimal devolve o valor por baixo. É para a fronteira — a base de dados e os
// tipos gerados do contrato — e não para contas dentro do domínio.
func (d Dinheiro) Decimal() decimal.Decimal { return d.v }

func (d Dinheiro) String() string { return d.v.String() }

// Taxa é uma taxa anual em pontos percentuais: 3.25 é 3,25 %.
//
// TAN, TAEG, spread e o valor da Euribor são todos Taxa. Somar-lhes um
// Dinheiro não compila, que é o objectivo.
type Taxa struct {
	_ [0]func()
	v decimal.Decimal
}

// TaxaDeTexto lê uma taxa já normalizada ("3.25").
func TaxaDeTexto(s string) (Taxa, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Taxa{}, fmt.Errorf("taxa inválida %q: %w", s, err)
	}
	return Taxa{v: d}, nil
}

// TaxaDeDecimal é a porta de entrada a partir da fronteira.
func TaxaDeDecimal(d decimal.Decimal) Taxa { return Taxa{v: d} }

// Add soma duas taxas. Spread mais Euribor é a TAN da fase indexada, e é a
// única aritmética que o domínio faz sobre taxas.
func (t Taxa) Add(o Taxa) Taxa { return Taxa{v: t.v.Add(o.v)} }

// Sub subtrai duas taxas. É como o Crédito Agrícola devolve a Euribor:
// interest_rate menos reference_rate_value, porque o campo que diz chamar-se
// índice é o spread (ver DOSSIE-BANCOS.md).
func (t Taxa) Sub(o Taxa) Taxa { return Taxa{v: t.v.Sub(o.v)} }

func (t Taxa) Equal(o Taxa) bool { return t.v.Equal(o.v) }

func (t Taxa) Cmp(o Taxa) int { return t.v.Cmp(o.v) }

func (t Taxa) Decimal() decimal.Decimal { return t.v }

func (t Taxa) String() string { return t.v.String() }

// Racio é uma fracção adimensional: 0.8 é 80 %.
//
// O LTV é um Racio e não uma Taxa, e a distinção não é preciosismo: o contrato
// publica ltv: 0.8 e tan: 3.25, e trocá-los dá um número oitenta vezes errado
// com ar de plausível.
type Racio struct {
	_ [0]func()
	v decimal.Decimal
}

// RacioDeTexto lê uma fracção ("0.8").
func RacioDeTexto(s string) (Racio, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Racio{}, fmt.Errorf("rácio inválido %q: %w", s, err)
	}
	return Racio{v: d}, nil
}

// RacioDeDecimal é a porta de entrada a partir da fronteira.
func RacioDeDecimal(d decimal.Decimal) Racio { return Racio{v: d} }

func (r Racio) Equal(o Racio) bool { return r.v.Equal(o.v) }

func (r Racio) Cmp(o Racio) int { return r.v.Cmp(o.v) }

func (r Racio) Decimal() decimal.Decimal { return r.v }

func (r Racio) String() string { return r.v.String() }
