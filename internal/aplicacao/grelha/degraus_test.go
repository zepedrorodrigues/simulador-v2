package grelha_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Um degrau é uma linha de catalogo_taxas como as outras, e o que a preenche é
// UMA das observações que o mediram. Este ficheiro afirma qual — e é a §4, «Um
// degrau é uma linha completa», que decide: a do spread SERVIDO.

func TestARepresentativaDeUmDegrauResolvidoEADeCima(t *testing.T) {
	t.Parallel()

	// Num degrau resolvido todas as medições lá dentro têm o mesmo spread, e a
	// escolhida é a de Ate: o maior LTV onde esse spread foi efectivamente
	// medido. Qualquer outra seria igualmente verdadeira em preço — esta é a
	// que não obriga a explicar porque é que não foi a do topo.
	banco := &bancoCompleto{escada: escadariaCGD()}
	d, err := grelha.DescobrirBanco(
		t.Context(), banco, grelha.Referencia{}, hoje, grelha.Config{}, relogioFixo())
	if err != nil {
		t.Fatalf("DescobrirBanco: %v", err)
	}

	for i, degrau := range d.Degraus {
		if !degrau.Resolvido() {
			continue
		}
		if !degrau.Representativa.LTV.Equal(degrau.Ate) {
			t.Errorf(
				"degrau %d (%s; %s]: a representativa foi medida em %s, e devia ser a de Ate",
				i+1, degrau.De, degrau.Ate, degrau.Representativa.LTV)
		}
		if !degrau.Representativa.Spread.Equal(degrau.Spread) {
			t.Errorf(
				"degrau %d (%s; %s]: serve %s e a representativa mediu %s",
				i+1, degrau.De, degrau.Ate, degrau.Spread, degrau.Representativa.Spread)
		}
	}
}

func TestARepresentativaDeUmDegrauPorResolverEADoLadoCaro(t *testing.T) {
	t.Parallel()

	// É o teste que dá razão de ser a esta fatia inteira.
	//
	// A forma medida da CGD DESCE na última fronteira — 2,050 a 67 % e 1,350 a
	// 68 % —, e com o banco a recusar LTV não inteiro o refinamento não chega
	// lá: nasce um degrau por resolver a servir 2,050, que é o lado de BAIXO.
	// Logo a observação representativa tem de ser a de 67 %, e não a de 68 %.
	//
	// ⚠️ Escolher a de 68 % dava uma linha com `spread` = 2,050 e `taeg` do
	// preço de 1,350. Nenhum CHECK apanha isso — a linha é coerente para a base
	// e mentirosa para quem a lê.
	banco := &bancoCompleto{escada: escadariaCGD(), soInteiros: true}
	d, err := grelha.DescobrirBanco(
		t.Context(), banco, grelha.Referencia{}, hoje, grelha.Config{}, relogioFixo())
	if err != nil {
		t.Fatalf("DescobrirBanco: %v", err)
	}

	porResolver := 0
	for i, degrau := range d.Degraus {
		if degrau.Resolvido() {
			continue
		}
		porResolver++

		if !degrau.Representativa.Spread.Equal(degrau.Spread) {
			t.Errorf(
				"degrau %d por resolver (%s; %s]: serve o spread %s e a representativa mediu %s — "+
					"a linha ficaria com o spread de um preço e o TAEG de outro",
				i+1, degrau.De, degrau.Ate, degrau.Spread, degrau.Representativa.Spread)
		}

		// E a mesma afirmação vista pelo campo que a linha grava e o spread não
		// determina: o TAEG deste banco é o spread mais 1 p.p., por construção.
		taeg := degrau.Representativa.Oferta.TAEG
		if taeg == nil {
			t.Fatalf("degrau %d por resolver: a representativa não trouxe TAEG", i+1)
		}
		if esperado := degrau.Spread.Add(pp("1.000")); !taeg.Equal(esperado) {
			t.Errorf(
				"degrau %d por resolver (%s; %s]: serve o spread %s, que pede o TAEG %s, e a linha leva %s",
				i+1, degrau.De, degrau.Ate, degrau.Spread, esperado, taeg)
		}
	}

	if porResolver == 0 {
		t.Fatal("não nasceu nenhum degrau por resolver, e sem ele este teste não afirma nada")
	}
}

func TestNumDegrauPorResolverDeDescidaARepresentativaEstaEmLtvMin(t *testing.T) {
	t.Parallel()

	// A consequência que a §4 manda escrever para não ser «corrigida» mais
	// tarde: quando o lado caro é o de baixo, o LTV da linha cai em `De` — o
	// extremo que o intervalo (De, Ate] exclui. Não é incoerência; é o único
	// ponto onde o spread servido foi observado, porque num degrau por resolver
	// não há medições no meio.
	banco := &bancoCompleto{escada: escadariaCGD(), soInteiros: true}
	d, err := grelha.DescobrirBanco(
		t.Context(), banco, grelha.Referencia{}, hoje, grelha.Config{}, relogioFixo())
	if err != nil {
		t.Fatalf("DescobrirBanco: %v", err)
	}

	visto := false
	for _, degrau := range d.Degraus {
		if degrau.Resolvido() || !degrau.Representativa.LTV.Equal(degrau.De) {
			continue
		}
		visto = true
		if degrau.Representativa.Spread.Cmp(*degrau.SpreadMinimo) <= 0 {
			t.Errorf(
				"degrau (%s; %s]: a representativa está em De e mediu %s, que não é o lado caro (%s)",
				degrau.De, degrau.Ate, degrau.Representativa.Spread, degrau.Spread)
		}
	}
	if !visto {
		t.Skip("nenhum degrau por resolver com o lado caro em baixo nesta forma — nada a afirmar")
	}
}

func TestUmaLinhaDeDegrauLevaOsCamposQueOCheckExige(t *testing.T) {
	t.Parallel()

	// O CHECK catalogo_taxas_resposta_completa_quando_sucesso exige TAN, TAEG,
	// prestação e MTIC em qualquer linha de sucesso. Um degrau não é excepção, e
	// nenhum desses campos se deriva de um spread.
	banco := &bancoCompleto{escada: escadariaCGD()}
	d, err := grelha.DescobrirBanco(
		t.Context(), banco, grelha.Referencia{}, hoje, grelha.Config{}, relogioFixo())
	if err != nil {
		t.Fatalf("DescobrirBanco: %v", err)
	}

	obs, err := grelha.ObservacoesDeEscala(d)
	if err != nil {
		t.Fatalf("ObservacoesDeEscala: %v", err)
	}
	if len(obs) != len(d.Degraus) {
		t.Fatalf("saíram %d linhas de %d degraus", len(obs), len(d.Degraus))
	}

	for i, o := range obs {
		if o.Degrau == nil {
			t.Fatalf("linha %d não vem marcada como degrau, e sem isso grava ltv_min nulo", i+1)
		}
		if o.Oferta.CapturadoEm.IsZero() {
			t.Errorf("linha %d sem instante de captura, e a coluna é NOT NULL", i+1)
		}
		for nome, presente := range map[string]bool{
			"TAN":       o.Oferta.TAN != nil,
			"TAEG":      o.Oferta.TAEG != nil,
			"prestação": o.Oferta.Prestacao != nil,
			"MTIC":      o.Oferta.MTIC != nil,
		} {
			if !presente {
				t.Errorf("linha %d de degrau sem %s: o CHECK da tabela recusa-a", i+1, nome)
			}
		}
	}
}

func TestCadaLinhaDeDegrauLevaOSeuIntervaloENaoODoVizinho(t *testing.T) {
	t.Parallel()

	// Cada linha afirma o intervalo do SEU degrau. Uma escala em que as linhas
	// trocassem de intervalo entre si — ou levassem todas o mesmo — dizia que o
	// banco pratica um preço onde pratica outro, e nenhum CHECK apanha isso:
	// cada linha, sozinha, é válida.
	//
	// ⚠️ Não é a armadilha do apontador para a variável de ciclo: medido a
	// 2026-07-27, desde a Go 1.22 ela é por iteração e `&degrau.DegrauLTV` já
	// dá apontadores distintos. O que este teste apanha é enganar-se no índice.
	banco := &bancoCompleto{escada: escadariaCGD()}
	d, err := grelha.DescobrirBanco(
		t.Context(), banco, grelha.Referencia{}, hoje, grelha.Config{}, relogioFixo())
	if err != nil {
		t.Fatalf("DescobrirBanco: %v", err)
	}
	if len(d.Degraus) < 2 {
		t.Fatalf("a forma da CGD deu %d degrau(s), e este teste precisa de vários", len(d.Degraus))
	}

	obs, err := grelha.ObservacoesDeEscala(d)
	if err != nil {
		t.Fatalf("ObservacoesDeEscala: %v", err)
	}
	for i, o := range obs {
		if !o.Degrau.De.Equal(d.Degraus[i].De) || !o.Degrau.Ate.Equal(d.Degraus[i].Ate) {
			t.Errorf(
				"linha %d leva o intervalo (%s; %s] e o degrau dela é (%s; %s]",
				i+1, o.Degrau.De, o.Degrau.Ate, d.Degraus[i].De, d.Degraus[i].Ate)
		}
	}
}

func TestOCenarioDeUmaLinhaDeDegrauEODeReferencia(t *testing.T) {
	t.Parallel()

	// A escala mede-se na variável, em habitação própria e sem produtos, e a
	// chave da linha tem de o dizer — é por ela que a leitura sabe o que aquele
	// spread pressupõe.
	banco := &bancoCompleto{escada: escadariaMontepio()}
	d, err := grelha.DescobrirBanco(
		t.Context(), banco, grelha.Referencia{}, hoje, grelha.Config{}, relogioFixo())
	if err != nil {
		t.Fatalf("DescobrirBanco: %v", err)
	}

	obs, err := grelha.ObservacoesDeEscala(d)
	if err != nil {
		t.Fatalf("ObservacoesDeEscala: %v", err)
	}
	for i, o := range obs {
		if o.Ponto.Cenario != "variavel/0/propria" {
			t.Errorf("linha %d de degrau tem o cenário %q, e a escala mede-se em variavel/0/propria",
				i+1, o.Ponto.Cenario)
		}
	}
}

func TestUmaEscalaSemProvenienciaNaoSeGrava(t *testing.T) {
	t.Parallel()

	// Uma Descoberta construída à mão — como as dos testes puros da descoberta —
	// tem degraus e não tem observações. Gravá-la escreveria linhas sem os
	// campos nucleares, que desceriam a falha com o nome do banco em cima, como
	// se ele tivesse respondido mal.
	d := grelha.Descoberta{Degraus: []grelha.Degrau{{
		DegrauLTV: dominio.DegrauLTV{De: ltv("0.30"), Ate: ltv("0.80"), Spread: pp("1.350")},
	}}}

	_, err := grelha.ObservacoesDeEscala(d)
	if err == nil {
		t.Fatal("gravou-se uma escala que nunca passou por um banco")
	}
	if !strings.Contains(err.Error(), "observação representativa") {
		t.Errorf("o erro não nomeia o que falta: %v", err)
	}
}

// bancoCompleto é um banco cuja função de preço é uma escadaria medida e cuja
// resposta traz tudo o que uma linha de sucesso precisa.
//
// ⚠️ O TAEG é o spread mais 1 p.p., por construção. É o que permite afirmar que
// a linha gravada leva o TAEG DO spread que serve: com um TAEG constante, trocar
// a observação representativa não partia teste nenhum.
type bancoCompleto struct {
	escada     escadaria
	soInteiros bool
}

func (b *bancoCompleto) ID() string   { return "completo" }
func (b *bancoCompleto) Nome() string { return "Banco Completo" }

func (b *bancoCompleto) Requisitos() dominio.Requisitos {
	return dominio.Requisitos{
		BancoID: "completo", BancoNome: "Banco Completo", Custo: dominio.CustoBarato,
		PeriodosFixosModo: dominio.ModoLista, PrazoMin: 1, PrazoMax: 40, IdadeMaximaFim: 75,
	}
}

func (b *bancoCompleto) Simular(_ context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	racio, err := dominio.LTV(p.Montante, p.ValorImovel)
	if err != nil {
		return dominio.Oferta{}, err
	}

	// A recusa de LTV não inteiro é a versão determinista de «o refinamento não
	// chegou ao fim»: a fase 1 mede, a fase 2 leva com uma recusa ao primeiro
	// ponto médio. Como um banco a sério recusa — oferta de falha, não erro.
	if b.soInteiros {
		emPontos := racio.Decimal().Shift(2)
		if !emPontos.Equal(emPontos.Truncate(0)) {
			return dominio.Falhar(b.ID(), b.Nome(), &dominio.ErroOferta{
				Codigo:   dominio.ErroProdutoIndisponivel,
				Mensagem: "este banco só simula LTV inteiro",
			}), nil
		}
	}

	spread, err := b.escada.spreadEm(racio)
	if err != nil {
		return dominio.Falhar(b.ID(), b.Nome(), &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: "este banco não financia um LTV tão alto",
		}), nil
	}

	tan := spread.Add(pp("2.000"))
	taeg := spread.Add(pp("1.000"))
	prestacao := dominio.DinheiroDeInteiro(900)
	mtic := dominio.DinheiroDeInteiro(324_000)

	return dominio.Oferta{
		BancoID: b.ID(), BancoNome: b.Nome(),
		TAN: &tan, TAEG: &taeg, Spread: &spread,
		Prestacao: &prestacao, MTIC: &mtic,
		Indexante: dominio.Euribor6M, EuriborValor: ptr(pp("2.100")),
	}, nil
}

func ptr[T any](v T) *T { return &v }

// relogioFixo carimba sempre o mesmo instante. Um relógio a sério tornava os
// testes dependentes da hora a que correm, e o que aqui se afirma é que o
// carimbo EXISTE — não quando é.
func relogioFixo() func() time.Time {
	return func() time.Time { return time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC) }
}
