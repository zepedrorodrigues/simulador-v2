package grelha_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O caminho todo, sem rede: um banco cuja função de preço é a forma MEDIDA da
// CGD, atravessado pelo AmostrarBanco e pelo Descobrir, a produzir a escala.
//
// ⚠️ É a única afirmação deste repositório sobre a descoberta de LTV ligada a
// um bancos.Banco a sério. As outras medem as duas metades em separado — a
// função de preço no escala_test.go, o banco nos testes dele —, e uma ponte que
// ninguém atravessa é onde os enganos vivem.
func TestDescobrirBancoReconstroiAFormaMedidaDaCGD(t *testing.T) {
	t.Parallel()

	banco := &bancoEmDegraus{escada: escadariaCGD()}
	d, err := grelha.DescobrirBanco(t.Context(), banco, grelha.Referencia{}, hoje, grelha.Config{}, relogioFixo())
	if err != nil {
		t.Fatalf("DescobrirBanco: %v", err)
	}

	for _, caso := range []struct{ ltv, spread string }{
		{"0.6650", "2"},
		{"0.6675", "2.05"},
		{"0.6800", "1.35"},
	} {
		spread, _, err := d.Escala.SpreadEm(ltv(caso.ltv))
		if err != nil {
			t.Fatalf("SpreadEm(%s): %v", caso.ltv, err)
		}
		if got := spread.String(); got != caso.spread {
			t.Errorf("SpreadEm(%s) = %s, esperava %s — escala: %s",
				caso.ltv, got, caso.spread, desenhar(d.Escala))
		}
	}
	t.Logf("CGD por um banco a sério: %d amostras, %d degraus", d.Amostras, len(d.Escala.Degraus()))
}

func TestOAmostradorVariaOMontanteEDeixaORestoQuieto(t *testing.T) {
	t.Parallel()

	// O que se varia é UMA coisa. Se o amostrador mexesse no prazo, na
	// finalidade ou nos produtos, a escala medida deixava de ser a do cenário
	// de referência — e o argumento de que as dimensões se somam caía com ela.
	banco := &bancoEmDegraus{escada: escadariaCGD()}
	amostrar, err := grelha.AmostrarBanco(banco, grelha.Referencia{}, hoje, relogioFixo())
	if err != nil {
		t.Fatalf("AmostrarBanco: %v", err)
	}

	for _, r := range []string{"0.40", "0.80", "1.00"} {
		if _, err := amostrar(t.Context(), ltv(r)); err != nil {
			t.Fatalf("amostrar(%s): %v", r, err)
		}
	}

	if len(banco.vistos) != 3 {
		t.Fatalf("o banco viu %d pedidos, esperava 3", len(banco.vistos))
	}
	primeiro := banco.vistos[0]
	for i, p := range banco.vistos {
		if p.PrazoAnos != primeiro.PrazoAnos || p.TipoTaxa != primeiro.TipoTaxa ||
			p.Finalidade != primeiro.Finalidade || len(p.Produtos) != 0 ||
			!p.ValorImovel.Equal(primeiro.ValorImovel) {
			t.Errorf("pedido %d mudou mais do que o montante: %+v", i+1, p)
		}
	}

	// 40 % de 400 000 € são 160 000 €, ao cêntimo.
	if !banco.vistos[0].Montante.Equal(dominio.DinheiroDeInteiro(160_000)) {
		t.Errorf("montante para LTV 40 %% = %s, esperava 160000", banco.vistos[0].Montante)
	}
}

func TestOAmostradorDevolveOLTVQueOBancoViuENaoOPedido(t *testing.T) {
	t.Parallel()

	// ⚠️ Com um imóvel que não dá números redondos, o montante arredonda ao
	// cêntimo e o rácio que o banco vê deixa de ser o pedido. A escala tem de
	// ficar com o segundo: uma fronteira no primeiro é uma afirmação sobre um
	// ponto que ninguém observou.
	banco := &bancoEmDegraus{escada: escadariaCGD()}
	amostrar, err := grelha.AmostrarBanco(banco, grelha.Referencia{
		ValorImovel: dominio.DinheiroDeInteiro(333_333),
		Montante:    dominio.DinheiroDeInteiro(200_000),
	}, hoje, relogioFixo())
	if err != nil {
		t.Fatalf("AmostrarBanco: %v", err)
	}

	pedido := ltv("0.665625")
	m, err := amostrar(t.Context(), pedido)
	if err != nil {
		t.Fatalf("amostrar: %v", err)
	}
	if m.LTV.Equal(pedido) {
		t.Fatalf("devolveu o LTV pedido (%s) — com 333 333 € não há montante ao cêntimo que o dê", pedido)
	}

	// E o que devolveu tem de ser o rácio do montante que o banco viu.
	visto := banco.vistos[len(banco.vistos)-1]
	esperado, err := dominio.LTV(visto.Montante, visto.ValorImovel)
	if err != nil {
		t.Fatalf("LTV: %v", err)
	}
	if !m.LTV.Equal(esperado) {
		t.Errorf("devolveu %s e o banco viu %s", m.LTV, esperado)
	}
}

func TestUmBancoQueRecusaOLTVNaoInventaPonto(t *testing.T) {
	t.Parallel()

	// Recusar acima do tecto que o banco financia é informação: é assim que a
	// descoberta descobre onde a escala acaba. O que não pode é virar um ponto.
	banco := &bancoEmDegraus{escada: escadaria{{ate: ltv("0.90"), spread: pp("1.350")}}}
	amostrar, err := grelha.AmostrarBanco(banco, grelha.Referencia{}, hoje, relogioFixo())
	if err != nil {
		t.Fatalf("AmostrarBanco: %v", err)
	}

	if _, err := amostrar(t.Context(), ltv("0.95")); !errors.Is(err, grelha.ErrBancoRecusou) {
		t.Fatalf("erro = %v, esperava ErrBancoRecusou", err)
	}
}

func TestUmaEscalaAcabaOndeOBancoDeixaDeFinanciar(t *testing.T) {
	t.Parallel()

	// A CGD financia até 90 % na habitação própria. A escala tem de acabar lá,
	// e não estender o último spread até 100 % — quem pedisse 95 % receberia um
	// preço que o banco não pratica.
	banco := &bancoEmDegraus{escada: escadaria{
		{ate: ltv("0.80"), spread: pp("1.350")},
		{ate: ltv("0.90"), spread: pp("1.500")},
	}}

	d, err := grelha.DescobrirBanco(t.Context(), banco, grelha.Referencia{}, hoje, grelha.Config{}, relogioFixo())
	if err != nil {
		t.Fatalf("DescobrirBanco: %v", err)
	}

	degraus := d.Escala.Degraus()
	if topo := degraus[len(degraus)-1].Ate; topo.Cmp(ltv("0.90")) > 0 {
		t.Errorf("a escala vai até %s, e o banco financia até 0,90", topo)
	}
	if _, _, err := d.Escala.SpreadEm(ltv("0.95")); !errors.Is(err, dominio.ErrLTVForaDaEscala) {
		t.Errorf("SpreadEm(0.95) devolveu %v, esperava ErrLTVForaDaEscala", err)
	}
	if d.Falhas == 0 {
		t.Error("não contou as recusas acima do tecto, e elas são o que diz onde a escala acaba")
	}
}

func TestUmBancoQueNaoDaSpreadNaoEntraNaEscala(t *testing.T) {
	t.Parallel()

	// Um banco que responde sem spread não dá ponto nenhum — e não se estima o
	// spread a partir da TAN menos uma Euribor que não veio.
	banco := &bancoEmDegraus{escada: escadariaCGD(), semSpread: true}
	amostrar, err := grelha.AmostrarBanco(banco, grelha.Referencia{}, hoje, relogioFixo())
	if err != nil {
		t.Fatalf("AmostrarBanco: %v", err)
	}

	if _, err := amostrar(t.Context(), ltv("0.80")); !errors.Is(err, grelha.ErrBancoSemSpread) {
		t.Fatalf("erro = %v, esperava ErrBancoSemSpread", err)
	}
}

// bancoEmDegraus é um banco cuja função de preço é uma escadaria medida.
// Regista os pedidos que viu, que é como se afirma o que o amostrador variou.
type bancoEmDegraus struct {
	escada    escadaria
	semSpread bool
	vistos    []dominio.Pedido
}

func (b *bancoEmDegraus) ID() string   { return "degraus" }
func (b *bancoEmDegraus) Nome() string { return "Banco em Degraus" }

func (b *bancoEmDegraus) Requisitos() dominio.Requisitos {
	return dominio.Requisitos{
		BancoID: "degraus", BancoNome: "Banco em Degraus", Custo: dominio.CustoBarato,
		PeriodosFixosModo: dominio.ModoLista, PrazoMin: 1, PrazoMax: 40, IdadeMaximaFim: 75,
	}
}

func (b *bancoEmDegraus) Simular(_ context.Context, p dominio.Pedido) (dominio.Oferta, error) {
	b.vistos = append(b.vistos, p)

	racio, err := dominio.LTV(p.Montante, p.ValorImovel)
	if err != nil {
		return dominio.Oferta{}, err
	}
	spread, err := b.escada.spreadEm(racio)
	if err != nil {
		// Como um banco a sério recusa: oferta de falha, não erro de transporte.
		return dominio.Falhar(b.ID(), b.Nome(), &dominio.ErroOferta{
			Codigo:   dominio.ErroProdutoIndisponivel,
			Mensagem: "este banco não financia um LTV tão alto",
		}), nil
	}

	oferta := dominio.Oferta{BancoID: b.ID(), BancoNome: b.Nome()}
	if !b.semSpread {
		oferta.Spread = &spread
	}
	return oferta, nil
}
