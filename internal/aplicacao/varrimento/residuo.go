package varrimento

import (
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O resíduo da §7.4, e porque é que é uma função pura.
//
// ⚠️ É o travão contra o modo de falha deste desenho, e a §7.4 chama-lhe
// obrigatório: ao vivo, um campo mal lido estragava UMA resposta; com a grelha
// envenena TODAS até ao varrimento seguinte, e envenena-as com ar de certas.
//
// Não é um campo da Observacao preenchido por quem a constrói, e isso é
// deliberado: os degraus da escala de LTV não passam pelo `simular` — o
// amostrador chama o `b.Simular` directamente — e foi exactamente assim que o
// `capturado_em` quase ficou a zero em todas as linhas de degrau (KAN-16,
// sétima fatia). Um campo a preencher em dois caminhos esquece-se num deles;
// uma função que se chama a partir da observação inteira não tem como.

// Residuo é a diferença, em euros, entre a prestação que o banco devolveu para
// a primeira fase e a que a amortização francesa dá sobre o plano dele.
//
// O bool é falso quando não havia o que comparar — e isso é diferente de um
// resíduo de zero, que é uma medição que fechou.
//
// ⚠️ Com sinal. Positivo é o banco a cobrar mais do que a nossa conta prevê,
// negativo o contrário; um valor absoluto perdia a direcção sem poupar nada.
//
// ⚠️ A PRIMEIRA fase e não outra: a segunda de uma mista amortiza um capital que
// já vem do arredondamento aos cêntimos de dezenas de prestações, e esse ruído é
// do banco. Os números que o mostram estão na §4 do ARQUITETURA.md.
func Residuo(o Observacao) (dominio.Dinheiro, bool) {
	if !o.Sucesso() || len(o.Oferta.Fases) == 0 {
		return dominio.Dinheiro{}, false
	}

	// ⚠️ O prazo é o que o banco APLICOU, lido da última fase, e não o que se
	// pediu. Os bancos encolhem prazos — a idade máxima ao fim do contrato é a
	// razão mais comum, e o Novo Banco traz o máximo dentro do próprio erro —, e
	// medir contra o prazo pedido daria um resíduo de dezenas de euros a dizer
	// «o banco mudou» quando o que houve foi um ajuste que ele declarou.
	meses := o.Oferta.Fases[len(o.Oferta.Fases)-1].AteMes
	primeira := o.Oferta.Fases[0]

	// ⚠️ O capital é o montante do pedido, e é seguro que seja: os quatro
	// dominio.CampoAjustado são o período fixo, o prazo, o tipo de taxa e o
	// indexante — nenhum banco ajusta o montante. Se algum dia um o fizer, o
	// ajuste tem de chegar aqui, senão o resíduo dele passa a ser ruído.
	esperada, err := dominio.PrestacaoFrancesa(o.Ponto.Pedido.Montante, primeira.Taxa, meses)
	if err != nil {
		// Um plano que não descreve um plano — zero meses, ou fases que não
		// fecham. Não há resíduo, e inventar um zero seria dizer que bateu.
		return dominio.Dinheiro{}, false
	}

	return primeira.Prestacao.Sub(esperada), true
}
