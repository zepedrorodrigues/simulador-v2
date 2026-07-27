package grelha_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// As escadarias abaixo são funções de preço medidas, não inventadas. Servem de
// banco falso: respondem sem rede e sem relógio, e é contra elas que se afirma
// que a descoberta reconstrói o que o banco pratica.

// escadariaCGD é a forma medida da CGD a 2026-07-26 (docs/ANALISE-KAN-35.md §3), na
// fatia que foi varrida.
//
// ⚠️ Duas das três fronteiras não caem em LTV inteiro, e o 2,050 é um patamar
// isolado de 1,3 p.p. onde o preço SOBE e volta a DESCER. É esta forma que
// matou a banda de passo fixo, e é por isso que ela é o caso de referência
// deste ficheiro.
func escadariaCGD() escadaria {
	return escadaria{
		{ate: ltv("0.3325"), spread: pp("1.950")},
		{ate: ltv("0.6660"), spread: pp("2.000")},
		{ate: ltv("0.6790"), spread: pp("2.050")},
		{ate: ltv("1.00"), spread: pp("1.350")},
	}
}

// escadariaMontepio é o extremo oposto, medido a 2026-07-27: o LTV não muda o preço de
// todo, de 50 % a 100 %. Um degrau só.
func escadariaMontepio() escadaria {
	return escadaria{{ate: ltv("1.00"), spread: pp("1.500")}}
}

func TestDescobrirDaOsTresSpreadsDaCGDNosTresPontosMedidos(t *testing.T) {
	t.Parallel()

	// É o critério de pronto da KAN-35: 66,50 %, 66,75 % e 68,00 % têm de
	// receber TRÊS spreads diferentes. Numa banda de 5 %, ou de 1 %, recebiam
	// dois — e quem caísse do lado errado pagava 0,70 p.p. a mais, cerca de
	// 79 € por mês em 200 000 € a 30 anos.
	d, err := grelha.Descobrir(context.Background(), grelha.Config{}, escadariaCGD().amostrar)
	if err != nil {
		t.Fatalf("Descobrir: %v", err)
	}

	for _, caso := range []struct{ ltv, spread string }{
		{"0.6650", "2"},
		{"0.6675", "2.05"},
		{"0.6800", "1.35"},
	} {
		spread, nota, err := d.Escala.SpreadEm(ltv(caso.ltv))
		if err != nil {
			t.Fatalf("SpreadEm(%s): %v", caso.ltv, err)
		}
		if nota != "" {
			t.Errorf("SpreadEm(%s): veio com nota de intervalo por resolver: %s", caso.ltv, nota)
		}
		if got := spread.String(); got != caso.spread {
			t.Errorf("SpreadEm(%s) = %s, esperava %s", caso.ltv, got, caso.spread)
		}
	}
}

func TestDescobrirNaoAfirmaUmSpreadOndeMediuOutro(t *testing.T) {
	t.Parallel()

	// A invariante que sustenta tudo o resto: em cada LTV que se chegou a
	// medir, a escala devolve o spread que o banco deu ali. Uma fronteira posta
	// no ponto de cima em vez do de baixo quebra isto — e quebra-o em silêncio,
	// que é o modo de falha da §7.4.
	escadas := escadariaCGD()
	var medidos []grelha.Medicao
	espiar := func(ctx context.Context, r dominio.Racio) (grelha.Medicao, error) {
		m, err := escadas.amostrar(ctx, r)
		if err == nil {
			medidos = append(medidos, m)
		}
		return m, err
	}

	d, err := grelha.Descobrir(context.Background(), grelha.Config{}, espiar)
	if err != nil {
		t.Fatalf("Descobrir: %v", err)
	}

	for _, m := range medidos {
		spread, _, err := d.Escala.SpreadEm(m.LTV)
		if err != nil {
			t.Fatalf("SpreadEm(%s): %v", m.LTV, err)
		}
		if !spread.Equal(m.Spread) {
			t.Errorf("mediu-se %s em LTV %s e a escala diz %s", m.Spread, m.LTV, spread)
		}
	}
}

func TestDescobrirEncontraOPatamarNaoMonotonoDaCGD(t *testing.T) {
	t.Parallel()

	// O 2,050 tem de existir como degrau próprio. É o achado que abriu a
	// KAN-35, e é o que uma descoberta que assuma monotonia — ou poucos degraus
	// — perde: entre 2,000 e 1,350 o preço sobe antes de descer.
	d, err := grelha.Descobrir(context.Background(), grelha.Config{}, escadariaCGD().amostrar)
	if err != nil {
		t.Fatalf("Descobrir: %v", err)
	}

	degraus := d.Escala.Degraus()
	var patamar *dominio.DegrauLTV
	for i := range degraus {
		if degraus[i].Spread.String() == "2.05" {
			patamar = &degraus[i]
		}
	}
	if patamar == nil {
		t.Fatalf("o patamar de 2,050 não está na escala; degraus: %s", desenhar(d.Escala))
	}

	// A largura medida é de ~1,3 p.p.: de 66,60 % a 67,90 %.
	perto(t, "início do patamar", patamar.De, ltv("0.6660"))
	perto(t, "fim do patamar", patamar.Ate, ltv("0.6790"))
}

func TestORefinamentoEncontraUmPatamarQueCaiuEntreDoisPontosDaFase1(t *testing.T) {
	t.Parallel()

	// Aqui a fase 1 vai a 5 p.p. de propósito, para o patamar de 2,050 da CGD
	// — 66,60 % a 67,90 % — cair INTEIRO entre dois pontos amostrados, o 65 e o
	// 70. Quem chegar lá tem de o fazer pelo refinamento.
	//
	// ⚠️ É o caso que separa a opção D da opção C da análise. Uma bissecção que
	// assuma uma fronteira só entre dois pontos encontra a primeira — o
	// 2,000 → 2,050 — e nunca procura a segunda, porque já «resolveu» o par. E
	// aí a escala dá 2,050 a quem tem 69 % de LTV, que paga 1,350.
	d, err := grelha.Descobrir(context.Background(), grelha.Config{
		De:    ltv("0.30"),
		Ate:   ltv("0.70"),
		Passo: ltv("0.05"),
	}, escadariaCGD().amostrar)
	if err != nil {
		t.Fatalf("Descobrir: %v", err)
	}

	for _, caso := range []struct{ ltv, spread string }{
		{"0.6600", "2"},
		{"0.6700", "2.05"},
		{"0.6900", "1.35"},
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
}

func TestDescobrirDaUmDegrauSoAUmBancoDePrecoConstante(t *testing.T) {
	t.Parallel()

	// O Montepio, medido: o LTV não lhe muda o preço. A grelha por intervalos
	// representa-o com UMA linha — que é o argumento de que a representação
	// exacta é também a mais pequena.
	d, err := grelha.Descobrir(context.Background(), grelha.Config{De: ltv("0.50")}, escadariaMontepio().amostrar)
	if err != nil {
		t.Fatalf("Descobrir: %v", err)
	}

	if n := len(d.Escala.Degraus()); n != 1 {
		t.Fatalf("um preço constante deu %d degraus, esperava 1: %s", n, desenhar(d.Escala))
	}
	// Sem fronteiras não há refinamento: o custo é o da fase 1 e mais nada.
	if d.Amostras != 51 {
		t.Errorf("um banco sem fronteiras custou %d amostras, esperava as 51 da fase 1", d.Amostras)
	}
}

func TestFronteiraPorResolverServeOLadoMaisCaroComNota(t *testing.T) {
	t.Parallel()

	// O refinamento não chega ao fim — aqui porque o banco recusa tudo o que
	// não seja um LTV inteiro, que é a versão de laboratório de «a rede caiu a
	// meio». A regra é a do Anexo I, Parte II, alínea (d) da MCD: serve-se o
	// mais alto dos dois, com nota a dizer que é um limite superior.
	//
	// Corre-se nos dois sentidos de propósito: um preço que SOBE na fronteira e
	// um que DESCE. É a descida que apanha quem escolher o lado por ordem de
	// chegada em vez de por preço.
	for _, caso := range []struct {
		nome           string
		escada         escadaria
		alto, baixo    string
		dentroDoBuraco string
	}{
		{
			nome: "o preço sobe",
			escada: escadaria{
				{ate: ltv("0.6650"), spread: pp("2.000")},
				{ate: ltv("1.00"), spread: pp("2.050")},
			},
			alto: "2.05", baixo: "2", dentroDoBuraco: "0.665",
		},
		{
			nome: "o preço desce",
			escada: escadaria{
				{ate: ltv("0.6790"), spread: pp("2.050")},
				{ate: ltv("1.00"), spread: pp("1.350")},
			},
			alto: "2.05", baixo: "1.35", dentroDoBuraco: "0.675",
		},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			t.Parallel()

			d, err := grelha.Descobrir(context.Background(), grelha.Config{}, soInteiros(caso.escada))
			if err != nil {
				t.Fatalf("Descobrir: %v", err)
			}

			spread, nota, err := d.Escala.SpreadEm(ltv(caso.dentroDoBuraco))
			if err != nil {
				t.Fatalf("SpreadEm: %v", err)
			}
			if got := spread.String(); got != caso.alto {
				t.Errorf("dentro do intervalo por resolver serviu-se %s, e o mais caro é %s", got, caso.alto)
			}
			if nota == "" {
				t.Fatal("serviu-se um limite superior sem nota — é a §7.4 ao contrário: um número errado com ar de certo")
			}
			// A nota tem de nomear os dois números; sem eles diria «é
			// aproximado», que não é informação.
			for _, numero := range []string{virgula(caso.alto), virgula(caso.baixo)} {
				if !strings.Contains(nota, numero) {
					t.Errorf("a nota não nomeia %s: %s", numero, nota)
				}
			}
		})
	}
}

func TestDescobrirCabeNoOrcamentoDaAnalise(t *testing.T) {
	t.Parallel()

	// A análise orçamentou a opção D em ~96 pedidos por banco — ~71 de fase 1
	// mais ~25 de refinamento —, e é desse número que saem os ~58 s por corrida
	// contra a CGD. Um refinamento que rebentasse o orçamento seria uma decisão
	// nova sobre a carga que pomos num simulador alheio, e não um detalhe.
	d, err := grelha.Descobrir(context.Background(), grelha.Config{}, escadariaCGD().amostrar)
	if err != nil {
		t.Fatalf("Descobrir: %v", err)
	}
	if d.Amostras > 96 {
		t.Errorf("custou %d amostras; a análise orçamentou ~96", d.Amostras)
	}
	t.Logf("CGD: %d amostras, %d degraus", d.Amostras, len(d.Escala.Degraus()))
}

func TestDescobrirConstroiSobreOLTVMedidoENaoOPedido(t *testing.T) {
	t.Parallel()

	// Um pedido põe-se em euros, e o montante arredonda ao cêntimo: pedir
	// 66,5625 % pode medir outra coisa. A escala tem de ficar com o que se
	// mediu.
	escadas := escadariaCGD()
	desviado := func(ctx context.Context, r dominio.Racio) (grelha.Medicao, error) {
		m, err := escadas.amostrar(ctx, r)
		if err != nil {
			return m, err
		}
		// O banco viu um LTV um milionésimo abaixo do pedido.
		m.LTV = r.Sub(ltv("0.000001"))
		m.Spread, err = escadas.spreadEm(m.LTV)
		return m, err
	}

	d, err := grelha.Descobrir(context.Background(), grelha.Config{}, desviado)
	if err != nil {
		t.Fatalf("Descobrir: %v", err)
	}

	degraus := d.Escala.Degraus()
	if topo := degraus[len(degraus)-1].Ate; !topo.Equal(ltv("0.999999")) {
		t.Errorf("a escala acaba em %s; pediu-se 1,00 e mediu-se 0,999999", topo)
	}
	if base := degraus[0].De; !base.Equal(ltv("0.299999")) {
		t.Errorf("a escala começa em %s; pediu-se 0,30 e mediu-se 0,299999", base)
	}
}

func TestDescobrirRecusaUmPlanoQueDesligaORefinamentoSemODizer(t *testing.T) {
	t.Parallel()

	// Uma tolerância maior ou igual ao passo faz toda a fronteira nascer «já
	// dentro da tolerância»: a fase 2 nunca corre e a escala fica com a
	// resolução do passo — que é a banda fixa que a KAN-35 matou, com outro
	// nome.
	_, err := grelha.Descobrir(context.Background(), grelha.Config{
		Passo:      ltv("0.01"),
		Tolerancia: ltv("0.01"),
	}, escadariaCGD().amostrar)
	if !errors.Is(err, grelha.ErrDominioInvalido) {
		t.Fatalf("erro = %v, esperava ErrDominioInvalido", err)
	}
}

func TestDescobrirSemNadaMedidoNaoInventaEscala(t *testing.T) {
	t.Parallel()

	// Um banco em baixo não dá uma escala vazia nem uma escala de um degrau: dá
	// um erro. Servir de uma escala construída sobre zero medições seria
	// exactamente o número inventado com ar de oficial que o v1 aprendeu a não
	// dar.
	emBaixo := func(context.Context, dominio.Racio) (grelha.Medicao, error) {
		return grelha.Medicao{}, errors.New("ligação recusada")
	}

	d, err := grelha.Descobrir(context.Background(), grelha.Config{}, emBaixo)
	if !errors.Is(err, grelha.ErrSemMedicoes) {
		t.Fatalf("erro = %v, esperava ErrSemMedicoes", err)
	}
	if d.Falhas != d.Amostras || d.Amostras == 0 {
		t.Errorf("contou %d falhas em %d amostras", d.Falhas, d.Amostras)
	}
}

func TestDescobrirParaQuandoOContextoAcaba(t *testing.T) {
	t.Parallel()

	// O varrimento cancela-se, e a descoberta não fica a bater no banco depois
	// disso.
	ctx, cancelar := context.WithCancel(context.Background())
	escadas := escadariaCGD()
	contadas := 0
	aoDecimo := func(c context.Context, r dominio.Racio) (grelha.Medicao, error) {
		contadas++
		if contadas == 10 {
			cancelar()
		}
		return escadas.amostrar(c, r)
	}

	_, err := grelha.Descobrir(ctx, grelha.Config{}, aoDecimo)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("erro = %v; um varrimento cancelado ao décimo ponto não dá meia escala", err)
	}
	if contadas > 10 {
		t.Errorf("continuou a pedir depois do cancelamento: %d amostras", contadas)
	}
}

// ---------------------------------------------------------------- utilitários

// escadaria é uma função de preço em degraus: o primeiro degrau cujo `ate`
// alcança o rácio manda. É a mesma convenção do dominio.DegrauLTV — fechada em
// cima —, que é a que está medida no Novo Banco.
type escadaria []struct {
	ate    dominio.Racio
	spread dominio.Taxa
}

func (e escadaria) spreadEm(r dominio.Racio) (dominio.Taxa, error) {
	for _, d := range e {
		if r.Cmp(d.ate) <= 0 {
			return d.spread, nil
		}
	}
	return dominio.Taxa{}, errors.New("LTV acima do que este banco financia")
}

func (e escadaria) amostrar(_ context.Context, r dominio.Racio) (grelha.Medicao, error) {
	s, err := e.spreadEm(r)
	if err != nil {
		return grelha.Medicao{}, err
	}
	return grelha.Medicao{LTV: r, Spread: s}, nil
}

// soInteiros é um banco que só responde em LTV inteiro. É a versão determinista
// de «o refinamento não chegou ao fim»: a fase 1 mede, a fase 2 leva com uma
// recusa ao primeiro ponto médio.
func soInteiros(e escadaria) grelha.Amostrar {
	return func(ctx context.Context, r dominio.Racio) (grelha.Medicao, error) {
		emPontos := r.Decimal().Shift(2)
		if !emPontos.Equal(emPontos.Truncate(0)) {
			return grelha.Medicao{}, errors.New("este banco só simula LTV inteiro")
		}
		return e.amostrar(ctx, r)
	}
}

func perto(t *testing.T, oQue string, medido, esperado dominio.Racio) {
	t.Helper()
	// A tolerância da descoberta é de 0,05 p.p.; a fronteira medida tem de cair
	// a menos disso da verdadeira.
	folga := ltv("0.0005")
	if medido.Sub(esperado).Decimal().Abs().GreaterThan(folga.Decimal()) {
		t.Errorf("%s: medido %s, verdadeiro %s — mais de %s de erro", oQue, medido, esperado, folga)
	}
}

func desenhar(e dominio.EscalaDeLTV) string {
	var b strings.Builder
	for _, d := range e.Degraus() {
		b.WriteString("(" + d.De.String() + "; " + d.Ate.String() + "] → " + d.Spread.String() + "  ")
	}
	return b.String()
}

func virgula(s string) string { return strings.Replace(s, ".", ",", 1) }

func ltv(s string) dominio.Racio {
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		panic(err)
	}
	return r
}

func pp(s string) dominio.Taxa {
	t, err := dominio.TaxaDeTexto(s)
	if err != nil {
		panic(err)
	}
	return t
}
