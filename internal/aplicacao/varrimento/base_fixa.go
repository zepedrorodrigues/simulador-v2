package varrimento

import (
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A base da taxa fixa (ARQUITETURA.md §4, «O que se guarda por período é a BASE»).
//
// A base sai de TAN - spread(ltv). É constante por período — medido no
// cartesiano da CGD, ver a §4 do ARQUITETURA.md.
// EscalasPorBanco reconstrói a escala de LTV de cada banco a partir das linhas
// de degrau do lote.
//
// ⚠️ Reconstrói-se e não se transporta: as observações de degrau **são** a
// escala — cada uma traz o seu intervalo e o spread que nele vale —, e passá-las
// pelo dominio.NovaEscalaDeLTV volta a validar que são ordenadas, contíguas e
// sem buracos. Uma escala que não valide não vira meia escala: o banco fica sem
// nenhuma, e as linhas de taxa fixa dele ficam sem base.
//
// Um banco sem degraus no lote não aparece no mapa. Não é o mesmo que uma escala
// vazia, e a diferença importa: uma escala vazia responderia ErrEscalaVazia a
// cada consulta, e o que aqui se quer é dizer «deste banco não se sabe».
func EscalasPorBanco(obs []Observacao) map[string]dominio.EscalaDeLTV {
	porBanco := map[string][]dominio.DegrauLTV{}
	for _, o := range obs {
		if o.Degrau == nil || !o.Sucesso() {
			continue
		}
		id := o.Oferta.BancoID
		porBanco[id] = append(porBanco[id], *o.Degrau)
	}

	escalas := make(map[string]dominio.EscalaDeLTV, len(porBanco))
	for id, degraus := range porBanco {
		escala, err := dominio.NovaEscalaDeLTV(degraus)
		if err != nil {
			// ⚠️ Sem escala válida, sem base. Não se cala nem se inventa: quem
			// gravar as linhas de taxa fixa deste banco vai encontrá-las sem
			// base, e a §4 diz o que fazer a uma linha assim — grava-se, e não
			// se serve.
			continue
		}
		escalas[id] = escala
	}
	return escalas
}

// BaseDaTaxaFixa devolve a base de uma observação de taxa fixa: a TAN menos o
// spread do degrau em que o LTV dela cai.
//
// O bool é falso quando não há base a calcular, e são todos casos legítimos: a
// observação não é de taxa fixa, é de falha, veio sem TAN, o LTV não se deriva,
// ou o LTV cai fora da escala medida do banco.
//
// ⚠️ Só a taxa fixa. Na variável e na mista o spread é publicado à parte e a
// identidade fecha com a Euribor — não há base nenhuma a extrair, e extraí-la
// seria subtrair duas vezes o mesmo número.
func BaseDaTaxaFixa(o Observacao, escala dominio.EscalaDeLTV) (dominio.Taxa, bool) {
	if o.Ponto.Pedido.TipoTaxa != dominio.TaxaFixa || !o.Sucesso() || o.Oferta.TAN == nil {
		return dominio.Taxa{}, false
	}

	ltv, err := dominio.LTV(o.Ponto.Pedido.Montante, o.Ponto.Pedido.ValorImovel)
	if err != nil {
		return dominio.Taxa{}, false
	}

	// ⚠️ Fora da escala não há base. É o caso de uma observação medida num LTV
	// que a descoberta não alcançou — e inventar-lhe o degrau mais próximo era
	// dar-lhe o preço de outro cliente, que é o que o SpreadEm já recusa fazer.
	spread, _, err := escala.SpreadEm(ltv)
	if err != nil {
		return dominio.Taxa{}, false
	}

	return o.Oferta.TAN.Sub(spread), true
}
