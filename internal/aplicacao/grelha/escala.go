// Package grelha decide que pontos se pedem a um banco e reconstrói, do que
// voltou, a função de preço que ele pratica.
//
// É a metade do varrimento que o pacote varrimento não faz: lá vive a mecânica
// de concorrência — quem corre, com que travão e com que prazo —, aqui vive o
// que se pergunta e o que se conclui. A separação é a da §3 do ARQUITETURA.md
// levada a sério: o orquestrador não sabe o que é um LTV.
//
// O que está escrito hoje é a dimensão do LTV (KAN-16, primeira fatia). As
// outras dimensões da grelha — período fixo, tenor da Euribor, finalidade e
// produtos — entram aqui a seguir.
package grelha

import (
	"context"
	"errors"
	"fmt"

	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A descoberta das fronteiras de LTV, e porque é que é assim.
//
// A §4 do ARQUITETURA.md decidiu, a 2026-07-26 (KAN-35), que o spread se guarda
// por intervalo de LTV com fronteiras MEDIDAS, e não por banda de passo fixo. O
// dominio.EscalaDeLTV é o tipo que representa o resultado; isto é o que o
// preenche, e é a «opção D» da docs/ANALISE-KAN-35.md §5, que essa análise
// deixou explicitamente para aqui (§8, passo 4).
//
// São duas fases, e cada uma existe por uma medição:
//
//  1. amostragem UNIFORME ao passo. Não é o caminho ingénuo — é o que apanha
//     patamares estreitos. Na CGD há um spread de 2,050 que ocupa cerca de 1,25
//     p.p. de LTV, entre dois patamares mais largos, e o preço SOBE e volta a
//     DESCER. Uma bissecção pura, que assuma monotonia ou poucos degraus, salta
//     -o — e foi ele que abriu a KAN-35.
//
//  2. refinamento por bissecção SÓ entre vizinhos que discordem, até o intervalo
//     ficar abaixo da tolerância. É onde está a exactidão: a fase 1 diz que há
//     uma fronteira entre dois pontos, a fase 2 diz onde.
//
// ⚠️ E o remate, que é o que separa isto de uma aproximação com outro nome:
// quando o refinamento não chega ao fim — o banco recusou, a rede caiu, o
// arredondamento do montante não deixa estreitar mais — o intervalo fica
// marcado como NÃO RESOLVIDO, e a escala serve nele o spread mais alto com nota
// obrigatória. Não se escolhe um lado em silêncio. É o Anexo I, Parte II,
// alínea (d) da Directiva 2014/17/UE (MCD), e é a regra que a §4 adopta com
// ele: perante incerteza, o valor menos favorável ao consumidor, declarado.

// Medicao é um spread medido num LTV.
//
// ⚠️ O LTV é o que o banco viu, e não o que se lhe pediu. Um pedido põe-se em
// euros e o montante arredonda ao cêntimo, por isso pedir 66,5625 % pode medir
// 66,5624 %. A escala constrói-se sobre o que se mediu — dizer que a fronteira
// está onde se pediu era afirmar um número que ninguém observou.
type Medicao struct {
	LTV    dominio.Racio
	Spread dominio.Taxa
}

// Amostrar pede a um banco o spread que ele pratica num LTV.
//
// Devolver erro quer dizer «não se conseguiu medir aqui», e a descoberta trata
// todas as causas da mesma maneira: o banco a recusar o LTV por estar fora do
// que pratica, a rede em baixo, o prazo esgotado. Nenhuma delas inventa um
// ponto; todas fazem com que o intervalo à volta dele fique por resolver.
type Amostrar func(ctx context.Context, ltv dominio.Racio) (Medicao, error)

// Config é o plano da amostragem.
type Config struct {
	// De e Ate são o domínio de LTV a varrer, inclusive. Zero vale os de
	// omissão.
	De, Ate dominio.Racio

	// Passo é a fase 1. Zero vale PassoOmissao.
	Passo dominio.Racio

	// Tolerancia é onde a fase 2 pára. Zero vale ToleranciaOmissao.
	Tolerancia dominio.Racio
}

// Os valores de omissão saem da docs/ANALISE-KAN-35.md.
//
// ⚠️ O passo de 1 p.p. e o domínio de 30 % a 100 % são o custo que a análise
// orçamentou: ~71 pedidos de fase 1 mais ~25 de refinamento, cerca de um minuto
// por banco de HTTP em hora morta.
//
// ⚠️ A tolerância é ESCOLHIDA e não medida, e fica escrito que o é (Decisão 3
// da análise): 0,05 p.p. é uma ordem de grandeza abaixo do menor degrau
// observado — os 0,05 p.p. entre o 1,950 e o 2,000 da CGD. A primeira medição
// que a contrarie muda-a sem discussão.
var (
	DeOmissao         = racio("0.30")
	AteOmissao        = racio("1.00")
	PassoOmissao      = racio("0.01")
	ToleranciaOmissao = racio("0.0005")
)

var (
	// ErrSemMedicoes: não se mediu nada, ou mediu-se um ponto só. Uma escala
	// precisa de dois LTVs distintos para ter um intervalo.
	ErrSemMedicoes = errors.New("a descoberta não mediu LTVs suficientes para uma escala")

	// ErrDominioInvalido: o plano de amostragem não descreve um varrimento.
	ErrDominioInvalido = errors.New("domínio de LTV inválido")
)

// Descoberta é o que uma corrida da descoberta produziu: a escala e o que ela
// custou.
//
// ⚠️ As Falhas não são decoração. São o número de pontos que o banco ou a rede
// não deram, e é por elas que se percebe uma escala com poucos degraus por o
// banco ser simples ou por metade do varrimento ter falhado.
type Descoberta struct {
	Escala   dominio.EscalaDeLTV
	Amostras int
	Falhas   int
}

// Descobrir mede a escala de LTV de um banco.
//
// Não devolve erro por o banco falhar: falhas viram intervalos por resolver, e
// esses são servidos pelo lado mais caro com nota. Devolve erro quando não há
// escala nenhuma a construir — nada medido, ou um plano de amostragem que não é
// um varrimento.
func Descobrir(ctx context.Context, cfg Config, amostrar Amostrar) (Descoberta, error) {
	cfg = cfg.comOmissoes()
	if err := cfg.validar(); err != nil {
		return Descoberta{}, err
	}

	d := &descoberta{cfg: cfg, amostrar: amostrar}

	medidas := d.varrerUniforme(ctx)
	if len(medidas) < 2 {
		return Descoberta{Amostras: d.amostras, Falhas: d.falhas}, fmt.Errorf(
			"%w: %d ponto(s) medido(s) em %d tentativa(s)", ErrSemMedicoes, len(medidas), d.amostras)
	}

	medidas = d.refinar(ctx, medidas)

	// ⚠️ Um varrimento cancelado a meio não dá meia escala. Os degraus que se
	// chegaram a medir descreveriam uma fatia do domínio de LTV, e nada na
	// escala diria que o resto ficou por varrer — quem a consultasse via um
	// banco que «não financia acima de 39 %». É a §7.4 outra vez: o que se
	// serve tem de saber o que não sabe.
	if err := ctx.Err(); err != nil {
		return Descoberta{Amostras: d.amostras, Falhas: d.falhas},
			fmt.Errorf("descoberta interrompida ao fim de %d amostras: %w", d.amostras, err)
	}

	escala, err := construir(medidas, cfg.Tolerancia)
	if err != nil {
		return Descoberta{Amostras: d.amostras, Falhas: d.falhas}, err
	}
	return Descoberta{Escala: escala, Amostras: d.amostras, Falhas: d.falhas}, nil
}

func (c Config) comOmissoes() Config {
	if c.De.Cmp(dominio.Racio{}) <= 0 {
		c.De = DeOmissao
	}
	if c.Ate.Cmp(dominio.Racio{}) <= 0 {
		c.Ate = AteOmissao
	}
	if c.Passo.Cmp(dominio.Racio{}) <= 0 {
		c.Passo = PassoOmissao
	}
	if c.Tolerancia.Cmp(dominio.Racio{}) <= 0 {
		c.Tolerancia = ToleranciaOmissao
	}
	return c
}

func (c Config) validar() error {
	if c.De.Cmp(c.Ate) >= 0 {
		return fmt.Errorf("%w: de %s a %s não é um intervalo", ErrDominioInvalido, c.De, c.Ate)
	}
	if c.Passo.Cmp(c.Ate.Sub(c.De)) > 0 {
		return fmt.Errorf(
			"%w: o passo %s é maior do que o domínio %s a %s, e não haveria fase 1",
			ErrDominioInvalido, c.Passo, c.De, c.Ate)
	}
	if c.Tolerancia.Cmp(c.Passo) >= 0 {
		// Uma tolerância maior do que o passo desliga a fase 2 sem o dizer: toda
		// a fronteira nasceria já «dentro da tolerância» e a escala teria a
		// resolução do passo — que é a banda fixa que a KAN-35 matou.
		return fmt.Errorf(
			"%w: a tolerância %s não é menor do que o passo %s, e o refinamento nunca correria",
			ErrDominioInvalido, c.Tolerancia, c.Passo)
	}
	return nil
}

// descoberta é o estado de uma corrida: a contagem do que se pediu ao banco.
type descoberta struct {
	cfg      Config
	amostrar Amostrar
	amostras int
	falhas   int
}

// medir pede um ponto e conta o que aconteceu. O bool é «serve», não «correu
// bem»: um ponto medido fora do intervalo que se pediu não é utilizável.
func (d *descoberta) medir(ctx context.Context, ltv dominio.Racio) (Medicao, bool) {
	// ⚠️ O ctx verifica-se AQUI, e não só no ciclo da fase 1. O refinamento é
	// recursivo e não tem ciclo onde pôr a guarda: sem isto, um varrimento
	// cancelado continuava a bater no simulador do banco durante todas as
	// bissecções que ainda estivessem por fazer.
	if ctx.Err() != nil {
		return Medicao{}, false
	}
	d.amostras++
	m, err := d.amostrar(ctx, ltv)
	if err != nil {
		d.falhas++
		return Medicao{}, false
	}
	return m, true
}

// varrerUniforme é a fase 1: o domínio inteiro ao passo, e o extremo de cima
// sempre, mesmo que o passo não caia lá certinho.
//
// As medições saem por ordem crescente de LTV e sem repetições. ⚠️ A ordem é do
// LTV MEDIDO, não do pedido: se o arredondamento do montante fizer dois pedidos
// caírem no mesmo ponto, o segundo descarta-se — guardar os dois deixaria a
// escala com um intervalo de largura zero, que o dominio.NovaEscalaDeLTV
// recusa, e com razão.
func (d *descoberta) varrerUniforme(ctx context.Context) []Medicao {
	var medidas []Medicao
	juntar := func(m Medicao) {
		if len(medidas) > 0 && m.LTV.Cmp(medidas[len(medidas)-1].LTV) <= 0 {
			return
		}
		medidas = append(medidas, m)
	}

	for ltv := d.cfg.De; ltv.Cmp(d.cfg.Ate) < 0; ltv = ltv.Add(d.cfg.Passo) {
		if ctx.Err() != nil {
			return medidas
		}
		if m, serve := d.medir(ctx, ltv); serve {
			juntar(m)
		}
	}
	if ctx.Err() == nil {
		if m, serve := d.medir(ctx, d.cfg.Ate); serve {
			juntar(m)
		}
	}
	return medidas
}

// refinar é a fase 2: entre cada par de vizinhos que discordem, bissecta até o
// intervalo ficar abaixo da tolerância.
//
// Devolve a mesma sequência com os pontos novos inseridos, por ordem. O que
// ficou por estreitar reconhece-se pela largura, e é o construtor que decide o
// que fazer com ele — não há aqui uma lista de casos falhados a manter de
// acordo com a sequência.
func (d *descoberta) refinar(ctx context.Context, medidas []Medicao) []Medicao {
	refinadas := make([]Medicao, 0, len(medidas))
	for i, m := range medidas {
		refinadas = append(refinadas, m)
		if i+1 < len(medidas) {
			refinadas = append(refinadas, d.entre(ctx, m, medidas[i+1])...)
		}
	}
	return refinadas
}

// entre devolve os pontos a inserir estritamente entre a e b, por ordem
// crescente.
//
// A recursão nos dois lados não é zelo: um ponto médio com um spread que não é
// nem o de a nem o de b é um patamar inteiro escondido entre os dois, e tratar
// o par como uma fronteira só perdia-o. É o caso do 2,050 da CGD, uma escala
// abaixo.
//
// ⚠️ Parar por não se conseguir medir NÃO é um fracasso silencioso: o par fica
// com a largura que tinha, e a escala vai marcá-lo por resolver.
func (d *descoberta) entre(ctx context.Context, a, b Medicao) []Medicao {
	if a.Spread.Equal(b.Spread) {
		return nil
	}
	if b.LTV.Sub(a.LTV).Cmp(d.cfg.Tolerancia) <= 0 {
		return nil
	}

	m, serve := d.medir(ctx, a.LTV.Meio(b.LTV))
	if !serve {
		return nil
	}
	if m.LTV.Cmp(a.LTV) <= 0 || m.LTV.Cmp(b.LTV) >= 0 {
		// O ponto médio pedido caiu, depois de arredondado, em cima de um dos
		// extremos. Não há como estreitar mais, e insistir seria um ciclo.
		return nil
	}

	esquerda := d.entre(ctx, a, m)
	direita := d.entre(ctx, m, b)

	pontos := make([]Medicao, 0, len(esquerda)+1+len(direita))
	pontos = append(pontos, esquerda...)
	pontos = append(pontos, m)
	pontos = append(pontos, direita...)
	return pontos
}

// construir passa da sequência de medições à escala em degraus.
//
// A regra é uma só, aplicada a cada par de vizinhos:
//
//   - spreads iguais — continua o mesmo degrau;
//   - spreads diferentes e o par já abaixo da tolerância — a fronteira está
//     ali: o degrau fecha no LTV de baixo, que é o maior LTV onde o spread de
//     baixo foi MEDIDO. Não se fecha no de cima: isso afirmaria o spread antigo
//     num ponto onde se observou o novo;
//   - spreads diferentes e o par ainda largo — o refinamento não chegou lá.
//     Nasce um degrau por resolver, a servir o mais alto dos dois com o mais
//     baixo guardado ao lado, que é o que torna a nota verificável.
//
// ⚠️ Fica por medir a fatia entre o último ponto do spread de baixo e o
// primeiro do de cima, e ela é servida pelo spread de cima. É menor do que a
// tolerância por construção — 0,05 p.p. de LTV, ou 200 € de montante num imóvel
// de 400 000 € — e é o preço de nunca afirmar um spread num ponto onde se
// mediu outro.
func construir(medidas []Medicao, tolerancia dominio.Racio) (dominio.EscalaDeLTV, error) {
	if len(medidas) < 2 {
		return dominio.EscalaDeLTV{}, fmt.Errorf("%w: %d ponto(s)", ErrSemMedicoes, len(medidas))
	}

	var degraus []dominio.DegrauLTV
	inicio, spread := medidas[0].LTV, medidas[0].Spread

	for i := 1; i < len(medidas); i++ {
		a, b := medidas[i-1], medidas[i]
		if b.Spread.Equal(spread) {
			continue
		}

		// ⚠️ O degrau que fecha pode ter largura zero, e aí não se escreve. É o
		// caso de duas fronteiras seguidas sem nada de medido entre elas — a
		// primeira medição a discordar logo da segunda, por exemplo. Um degrau
		// de largura zero não é um intervalo, e o dominio.NovaEscalaDeLTV
		// recusa-o (com razão: seria um preço que vale em ponto nenhum).
		if inicio.Cmp(a.LTV) < 0 {
			degraus = append(degraus, dominio.DegrauLTV{De: inicio, Ate: a.LTV, Spread: spread})
		}

		if b.LTV.Sub(a.LTV).Cmp(tolerancia) > 0 {
			alto, baixo := a.Spread, b.Spread
			if alto.Cmp(baixo) < 0 {
				alto, baixo = baixo, alto
			}
			degraus = append(degraus, dominio.DegrauLTV{
				De: a.LTV, Ate: b.LTV, Spread: alto, SpreadMinimo: &baixo,
			})
			inicio = b.LTV
		} else {
			inicio = a.LTV
		}
		spread = b.Spread
	}

	ultimo := medidas[len(medidas)-1].LTV
	if inicio.Cmp(ultimo) < 0 {
		degraus = append(degraus, dominio.DegrauLTV{De: inicio, Ate: ultimo, Spread: spread})
	}

	return dominio.NovaEscalaDeLTV(degraus)
}

// racio lê um rácio constante deste ficheiro. Um erro aqui é um literal mal
// escrito no código, como no regexp.MustCompile — não um dado de fora.
func racio(s string) dominio.Racio {
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		panic(err)
	}
	return r
}
