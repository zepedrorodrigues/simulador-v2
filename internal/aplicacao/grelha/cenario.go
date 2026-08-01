package grelha

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A chave do ponto da grelha: <tipo>/<periodo>/<finalidade>, três segmentos
// sempre. A forma e as três decisões que a fixam — arity fixa, vocabulário
// fechado, e ser redonda — estão na §4 do ARQUITETURA.md.
const separadorDeCenario = "/"

// ErrCenarioIlegivel: a chave não tem a forma acima, ou tem um valor fora do
// vocabulário do domínio.
//
// ⚠️ Falhar aqui é o objectivo. Uma chave que não se reconhece e devolve um
// ponto plausível é o modo de falha da §7.4 — um número errado com ar de certo.
var ErrCenarioIlegivel = errors.New("cenário ilegível")

// Cenario é a chave estruturada de um ponto da grelha.
type Cenario struct {
	TipoTaxa dominio.TipoTaxa

	// PeriodoFixoAnos é 0 na taxa variável, que não tem fase fixa.
	PeriodoFixoAnos int

	Finalidade dominio.Finalidade
}

// Chave escreve o cenário na forma que vai para a coluna.
func (c Cenario) Chave() string {
	return string(c.TipoTaxa) + separadorDeCenario +
		strconv.Itoa(c.PeriodoFixoAnos) + separadorDeCenario +
		string(c.Finalidade)
}

// Validar recusa um cenário que não descreve um ponto.
//
// ⚠️ Recusa também as duas incoerências entre campos, e não só os valores
// isolados: uma variável com período fixo, e uma fixa ou mista sem ele. Um
// cenário assim escreve-se sem esforço e não corresponde a pedido nenhum.
func (c Cenario) Validar() error {
	if !c.TipoTaxa.Valido() {
		return fmt.Errorf("%w: tipo de taxa %q", ErrCenarioIlegivel, c.TipoTaxa)
	}
	if !c.Finalidade.Valido() {
		return fmt.Errorf("%w: finalidade %q", ErrCenarioIlegivel, c.Finalidade)
	}
	if c.PeriodoFixoAnos < 0 {
		return fmt.Errorf("%w: período fixo negativo (%d)", ErrCenarioIlegivel, c.PeriodoFixoAnos)
	}
	temFase, temPeriodo := c.TipoTaxa.TemPeriodoFixo(), c.PeriodoFixoAnos > 0
	if temFase != temPeriodo {
		return fmt.Errorf(
			"%w: a taxa %s com período fixo de %d anos — a variável não tem fase fixa, e a fixa e a mista têm",
			ErrCenarioIlegivel, c.TipoTaxa, c.PeriodoFixoAnos)
	}
	return nil
}

// LerCenario faz o caminho inverso da Chave.
//
// ⚠️ Exige exactamente três segmentos e valida cada um contra o vocabulário do
// domínio. É o que torna a chave redonda — e é o que faz uma mudança de formato
// aparecer como erro em vez de como um ponto errado.
func LerCenario(chave string) (Cenario, error) {
	partes := strings.Split(chave, separadorDeCenario)
	if len(partes) != 3 {
		return Cenario{}, fmt.Errorf(
			"%w: %q tem %d segmentos e a chave tem três (<tipo>/<periodo>/<finalidade>)",
			ErrCenarioIlegivel, chave, len(partes))
	}

	periodo, err := strconv.Atoi(partes[1])
	if err != nil {
		return Cenario{}, fmt.Errorf("%w: período %q não é um número em %q", ErrCenarioIlegivel, partes[1], chave)
	}

	c := Cenario{
		TipoTaxa:        dominio.TipoTaxa(partes[0]),
		PeriodoFixoAnos: periodo,
		Finalidade:      dominio.Finalidade(partes[2]),
	}
	if err := c.Validar(); err != nil {
		return Cenario{}, fmt.Errorf("%w (em %q)", err, chave)
	}
	return c, nil
}

// CenarioDe deriva o cenário de um pedido. É a metade que interessa em
// produção: é por aqui que a pergunta de um cliente encontra a linha da grelha.
//
// ⚠️ Um pedido de taxa fixa ou mista sem período escolhido não tem cenário —
// "sem preferência" é uma pergunta, não um ponto da grelha, e quem responde tem
// primeiro de a fechar com o dominio.EncaixarPeriodoFixo contra os períodos que
// o banco pratica. Devolver aqui um período inventado punha o cliente na linha
// de outro.
func CenarioDe(p dominio.Pedido) (Cenario, error) {
	c := Cenario{TipoTaxa: p.TipoTaxa, Finalidade: p.Finalidade}
	if p.TipoTaxa.TemPeriodoFixo() {
		if p.PeriodoFixoAnos == nil {
			return Cenario{}, fmt.Errorf(
				"%w: pedido de taxa %s sem período fixo escolhido — encaixa-se primeiro nos períodos do banco",
				ErrCenarioIlegivel, p.TipoTaxa)
		}
		c.PeriodoFixoAnos = *p.PeriodoFixoAnos
	}
	if err := c.Validar(); err != nil {
		return Cenario{}, err
	}
	return c, nil
}
