package grelha

import (
	"context"
	"fmt"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Da escala medida às linhas que se gravam.
//
// Um degrau não se grava a partir do intervalo e do spread: o CHECK
// catalogo_taxas_resposta_completa_quando_sucesso exige TAN, TAEG, prestação e
// MTIC em qualquer linha de sucesso, e nenhum deles se deriva de um spread. O
// que preenche a linha é a observação representativa do degrau — a que traz o
// spread SERVIDO —, e é o construir() que a escolhe (§4, «Um degrau é uma linha
// completa»).
//
// ⚠️ O cenário de uma linha de degrau é o de referência: `variavel/0/propria`.
// Não é um cenário à parte, e é de propósito — a escala mede-se na variável,
// está medido que o spread não depende do tipo de taxa, e a §4 já conta com
// duas linhas do mesmo varrimento a partilharem o cenario e a distinguirem-se
// pelas colunas tipadas.

// ObservacoesDeEscala passa os degraus de uma descoberta às linhas de
// catalogo_taxas que os representam.
//
// Recusa uma descoberta sem proveniência em vez de gravar linhas a meio: uma
// Medicao construída à mão — como as dos testes puros da descoberta — não traz
// oferta nenhuma, e uma linha de degrau sem os campos nucleares desceria a
// falha na gravação com o nome do banco em cima, como se ele tivesse respondido
// mal.
func ObservacoesDeEscala(d Descoberta) ([]varrimento.Observacao, error) {
	if len(d.Degraus) == 0 {
		return nil, fmt.Errorf("%w: a descoberta não trouxe degraus", ErrSemMedicoes)
	}

	obs := make([]varrimento.Observacao, 0, len(d.Degraus))
	for i, degrau := range d.Degraus {
		m := degrau.Representativa

		// ⚠️ A verificação é sobre o instante de captura e não sobre o spread: o
		// spread está sempre lá (é ele que constrói a escala), e é justamente
		// por isso que não distingue uma medição a sério de uma escrita à mão.
		if m.Oferta.CapturadoEm.IsZero() {
			return nil, fmt.Errorf(
				"degrau %d (%s a %s) não tem observação representativa: "+
					"a escala foi medida sem passar por um banco, e um degrau é uma linha completa (§4)",
				i+1, degrau.De, degrau.Ate)
		}

		cenario, err := CenarioDe(m.Pedido)
		if err != nil {
			return nil, fmt.Errorf("cenário do degrau %d (%s a %s): %w", i+1, degrau.De, degrau.Ate, err)
		}

		// A cópia local é explícita e não obrigatória — medido a 2026-07-27:
		// desde a Go 1.22 a variável de ciclo é por iteração, e `&degrau.DegrauLTV`
		// daria um apontador distinto por linha. Fica escrita assim para não
		// obrigar quem lê a saber essa regra de cor.
		intervalo := degrau.DegrauLTV
		obs = append(obs, varrimento.Observacao{
			Ponto:  varrimento.Ponto{Cenario: cenario.Chave(), Pedido: m.Pedido},
			Oferta: m.Oferta,
			Degrau: &intervalo,
		})
	}
	return obs, nil
}

// DegrausPorBanco é a DegrausDe que o varredor usa: mede a escala de LTV do
// banco e devolve-a já como observações.
//
// ⚠️ Um banco que não pratique preço por LTV não é caso de erro nenhum — o
// Montepio dá um degrau só, e uma linha é uma escala legítima. O que devolve
// erro é não se ter conseguido medir, e aí o varrimento regista a escala como
// não medida em vez de a calar.
func DegrausPorBanco(
	ref Referencia, hoje dominio.Data, cfg Config, agora func() time.Time,
) varrimento.DegrausDe {
	return func(ctx context.Context, b bancos.Banco) ([]varrimento.Observacao, error) {
		d, err := DescobrirBanco(ctx, b, ref, hoje, cfg, agora)
		if err != nil {
			return nil, err
		}
		return ObservacoesDeEscala(d)
	}
}
