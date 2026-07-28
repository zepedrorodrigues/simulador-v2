//go:build rede

package varrimento_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/cgd"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// E2E: o varrimento inteiro contra um banco a sério.
//
// Corre-se à mão e **não entra no portão** — depende de a CGD estar de pé:
//
//	go test -race -tags rede -run E2E -v ./internal/aplicacao/varrimento/
//
// ⚠️ O que aqui se afirma não são os números do dia. Um preçário muda, e um
// teste que fixasse 3,946 % reprovaria amanhã sem nada estar partido. O que se
// afirma são **relações entre respostas** — e essas têm de valer com qualquer
// preçário. Comparar uma resposta consigo própria não prova nada; comparar
// catorze umas com as outras prova que o modelo é coerente.
//
// As comparações que valem a pena estão em três famílias:
//
//  1. cada oferta consigo mesma (TAN = Euribor + spread; a prestação bate com a
//     amortização francesa; as fases fecham no prazo);
//  2. cenários entre si (mais prazo, prestação menor; fixa mais longa, TAN
//     maior; a Euribor do dia é a mesma em todos);
//  3. o que **não** muda quando devia poder mudar — e é aqui que se mede uma
//     hipótese da §4 do ARQUITETURA.md em vez de se assumir.

// grelhaDeEnsaio são os pontos que se varrem. Poucos e escolhidos: cada um
// existe para uma comparação, e nenhum está cá por simetria.
//
// ⚠️ Não é a grelha do varrimento a sério — essa decide-se em KAN-16. É o
// conjunto mínimo que deixa comparar prazo, montante, banda de LTV, tipo de
// taxa e período fixo, um de cada vez.
func grelhaDeEnsaio() []varrimento.Ponto {
	base := func(cenario string, valorImovel, montante int64, prazo int, taxa dominio.TipoTaxa, periodo *int) varrimento.Ponto {
		return varrimento.Ponto{
			Cenario: cenario,
			Pedido: dominio.Pedido{
				ValorImovel:     dominio.DinheiroDeInteiro(valorImovel),
				Montante:        dominio.DinheiroDeInteiro(montante),
				PrazoAnos:       prazo,
				TipoTaxa:        taxa,
				PeriodoFixoAnos: periodo,
				Finalidade:      dominio.FinalidadePropria,
				Localizacao:     dominio.LocalizacaoContinente,
				// Titular fictício e neutro: a CGD não pergunta por ele, e a
				// idade só entra no prazo máximo. 35 anos deixa os 40 livres.
				Titulares: []dominio.Titular{{DataNascimento: nascidoHaAnos(35)}},
			},
		}
	}
	anos := func(n int) *int { return &n }

	return []varrimento.Ponto{
		// Prazo, com tudo o resto igual — para a monotonia da prestação.
		base("variavel/ltv80/10a", 250_000, 200_000, 10, dominio.TaxaVariavel, nil),
		base("variavel/ltv80/20a", 250_000, 200_000, 20, dominio.TaxaVariavel, nil),
		base("variavel/ltv80/30a", 250_000, 200_000, 30, dominio.TaxaVariavel, nil),
		base("variavel/ltv80/40a", 250_000, 200_000, 40, dominio.TaxaVariavel, nil),

		// Banda de LTV, com o mesmo montante — para ver se o spread é degrau.
		base("variavel/ltv50/30a", 400_000, 200_000, 30, dominio.TaxaVariavel, nil),
		base("variavel/ltv90/30a", 250_000, 225_000, 30, dominio.TaxaVariavel, nil),

		// Montante, com a mesma banda — para ver se o preço depende do tamanho.
		base("variavel/ltv80/30a/pequeno", 125_000, 100_000, 30, dominio.TaxaVariavel, nil),
		base("variavel/ltv80/30a/grande", 500_000, 400_000, 30, dominio.TaxaVariavel, nil),

		// ⚠️ Os três pontos que partiram o modelo de bandas de 5 %: caíam todos
		// na banda 70 e têm três spreads diferentes (KAN-35). Ver o subteste
		// «a banda de 5 % não chega para a CGD».
		base("variavel/ltv66/30a", 303_030, 200_000, 30, dominio.TaxaVariavel, nil),
		base("variavel/ltv67/30a", 298_507, 200_000, 30, dominio.TaxaVariavel, nil),
		base("variavel/ltv68/30a", 294_117, 200_000, 30, dominio.TaxaVariavel, nil),

		// Período fixo da mista — para a monotonia da taxa base.
		base("mista/5a/ltv80/30a", 250_000, 200_000, 30, dominio.TaxaMista, anos(5)),
		base("mista/10a/ltv80/30a", 250_000, 200_000, 30, dominio.TaxaMista, anos(10)),
		base("mista/20a/ltv80/30a", 250_000, 200_000, 30, dominio.TaxaMista, anos(20)),

		// Taxa fixa: na CGD é ao prazo todo, por isso o prazo é o período.
		base("fixa/10a/ltv80", 250_000, 200_000, 10, dominio.TaxaFixa, nil),
		base("fixa/20a/ltv80", 250_000, 200_000, 20, dominio.TaxaFixa, nil),
		base("fixa/30a/ltv80", 250_000, 200_000, 30, dominio.TaxaFixa, nil),
	}
}

func TestE2EAGrelhaInteiraPelaCGD(t *testing.T) {
	obs := varrerAoVivo(t, 1)

	t.Run("cada oferta é coerente consigo mesma", func(t *testing.T) {
		for _, o := range obs {
			t.Run(o.Ponto.Cenario, func(t *testing.T) {
				verFasesFecham(t, o)
				verTANEEuriborMaisSpread(t, o)
				verPrestacaoBateComAFrancesa(t, o)
			})
		}
	})

	t.Run("mais prazo, prestação menor", func(t *testing.T) {
		// A mesma dívida esticada por mais tempo paga menos por mês. Se esta
		// relação se inverter, ou o parser trocou campos ou o prazo que
		// mandámos não é o que a CGD aplicou.
		anteriores := ""
		for _, cenario := range []string{
			"variavel/ltv80/10a", "variavel/ltv80/20a", "variavel/ltv80/30a", "variavel/ltv80/40a",
		} {
			if anteriores == "" {
				anteriores = cenario
				continue
			}
			antes, agora := prestacaoDe(t, obs, anteriores), prestacaoDe(t, obs, cenario)
			if agora.Cmp(antes) >= 0 {
				t.Errorf("%s paga %s e %s paga %s — esticar o prazo não devia encarecer a prestação",
					anteriores, antes, cenario, agora)
			}
			anteriores = cenario
		}
	})

	t.Run("fixa mais longa, TAN maior", func(t *testing.T) {
		// É a curva de funding do banco, e é a razão por que a §4 diz que a
		// taxa fixa é consulta e não cálculo: não é Euribor + spread.
		dez, vinte, trinta := tanDe(t, obs, "fixa/10a/ltv80"), tanDe(t, obs, "fixa/20a/ltv80"), tanDe(t, obs, "fixa/30a/ltv80")
		if dez.Cmp(vinte) >= 0 || vinte.Cmp(trinta) >= 0 {
			t.Errorf("a curva da fixa não é crescente: 10a %s, 20a %s, 30a %s", dez, vinte, trinta)
		}
		t.Logf("curva da taxa fixa: 10a %s, 20a %s, 30a %s", dez, vinte, trinta)
	})

	t.Run("mista mais longa no fixo, taxa base maior", func(t *testing.T) {
		cinco, dez, vinte := tanDe(t, obs, "mista/5a/ltv80/30a"), tanDe(t, obs, "mista/10a/ltv80/30a"), tanDe(t, obs, "mista/20a/ltv80/30a")
		if cinco.Cmp(dez) >= 0 || dez.Cmp(vinte) >= 0 {
			t.Errorf("a curva da mista não é crescente: 5a %s, 10a %s, 20a %s", cinco, dez, vinte)
		}
		t.Logf("curva da mista (TAN da fase fixa): 5a %s, 10a %s, 20a %s", cinco, dez, vinte)
	})

	t.Run("a Euribor do dia é a mesma em todos os cenários", func(t *testing.T) {
		// Um indexante que mudasse de cenário para cenário seria sinal de que
		// estamos a ler o campo errado — foi assim que o v1 apanhou o
		// IndexValue a passar por Euribor.
		var referencia *dominio.Taxa
		var deQuem string
		for _, o := range obs {
			if o.Oferta.EuriborValor == nil {
				continue
			}
			if referencia == nil {
				referencia, deQuem = o.Oferta.EuriborValor, o.Ponto.Cenario
				continue
			}
			if !o.Oferta.EuriborValor.Equal(*referencia) {
				t.Errorf("%s diz Euribor %s e %s diz %s — no mesmo dia e no mesmo tenor",
					deQuem, referencia, o.Ponto.Cenario, o.Oferta.EuriborValor)
			}
		}
		if referencia == nil {
			t.Fatal("nenhuma oferta trouxe Euribor — e há cenários variáveis na grelha")
		}
		t.Logf("Euribor 6M a %s: %s", time.Now().Format("2006-01-02"), referencia)
	})

	t.Run("a taxa fixa não tem Euribor nenhuma", func(t *testing.T) {
		for _, cenario := range []string{"fixa/10a/ltv80", "fixa/20a/ltv80", "fixa/30a/ltv80"} {
			o := observacaoDe(t, obs, cenario)
			if o.Oferta.EuriborValor != nil {
				t.Errorf("%s: uma fixa não tem indexante, veio %s", cenario, o.Oferta.EuriborValor)
			}
		}
	})

	t.Run("o desconto dos packs é o mesmo em todos os cenários", func(t *testing.T) {
		// ⚠️ Lê-se da **nota**, e não de um campo, porque o dominio.Pedido não
		// transporta selecção de produtos (KAN-33): hoje a segunda coluna de
		// preço da CGD só existe em português. Este teste é também a medida de
		// quanto é que isso custa.
		descontos := map[string][]string{}
		for _, o := range obs {
			d := descontoDosPacks(o)
			if d == "" {
				t.Errorf("%s: a oferta não diz o que os packs valem", o.Ponto.Cenario)
				continue
			}
			descontos[d] = append(descontos[d], o.Ponto.Cenario)
		}
		if len(descontos) != 1 {
			t.Errorf("o desconto dos packs não é único: %v", descontos)
		}
		for d, cenarios := range descontos {
			t.Logf("packs: -%s p.p. em %d cenários", d, len(cenarios))
		}
	})

	t.Run("o que o spread faz — e não faz", func(t *testing.T) {
		// ⚠️ Isto mede uma hipótese do ARQUITETURA.md §4, que diz que o spread
		// é «por banda de LTV» e que a grelha tem essa dimensão. Na CGD pode
		// não ser assim, e é preciso saber: se o spread não depender da banda,
		// a grelha da CGD tem menos uma dimensão (KAN-16).
		porCenario := map[string]string{}
		for _, cenario := range []string{
			"variavel/ltv50/30a", "variavel/ltv80/30a", "variavel/ltv90/30a",
			"variavel/ltv80/30a/pequeno", "variavel/ltv80/30a/grande",
			"variavel/ltv80/10a", "variavel/ltv80/40a",
		} {
			o := observacaoDe(t, obs, cenario)
			if o.Oferta.Spread == nil {
				t.Fatalf("%s: veio sem spread", cenario)
			}
			porCenario[cenario] = o.Oferta.Spread.String()
		}
		t.Logf("spread por cenário: %v", porCenario)

		distintos := map[string]bool{}
		for _, s := range porCenario {
			distintos[s] = true
		}
		// Medido a 2026-07-26: o prazo e o montante não mexem no spread; o LTV
		// mexe, e muito — 2,000 aos 50 % contra 1,350 aos 80 %. Mais barato em
		// cima, ao contrário do que se esperaria.
		if porCenario["variavel/ltv80/10a"] != porCenario["variavel/ltv80/40a"] {
			t.Errorf("o spread mudou com o prazo: 10a %s, 40a %s",
				porCenario["variavel/ltv80/10a"], porCenario["variavel/ltv80/40a"])
		}
		if porCenario["variavel/ltv80/30a/pequeno"] != porCenario["variavel/ltv80/30a/grande"] {
			t.Errorf("o spread mudou com o montante: 100 000 € %s, 400 000 € %s",
				porCenario["variavel/ltv80/30a/pequeno"], porCenario["variavel/ltv80/30a/grande"])
		}
		if len(distintos) == 1 {
			t.Errorf("o spread é %s em todos os cenários, incluindo LTV de 50 %% e de 90 %% — "+
				"medido a 2026-07-26 ele mudava com a banda; ou a CGD mudou de preçário, ou lemos o campo errado",
				porCenario["variavel/ltv80/30a"])
		}
	})

	// ⚠️ O achado mais importante deste E2E. Foi ele que obrigou a §4 do
	// ARQUITETURA.md a mudar.
	//
	// A §4 dizia que o spread se guardava «por banda de LTV», e o
	// dominio.BandaLTV implementava-o com vinte degraus de 5 %. Medido a
	// 2026-07-26, o spread da CGD muda **dentro** de um desses degraus: os LTV
	// de 66 %, 67 % e 68 % caíam todos na banda 70 e têm 2,000, 2,050 e 1,350
	// p.p. — 0,70 p.p. de diferença entre os extremos.
	//
	// Resolvido na KAN-35: o spread passou a guardar-se por intervalo com
	// fronteiras medidas (dominio.EscalaDeLTV), e o BandaLTV saiu do domínio.
	// Este teste continua aqui como sentinela contra a rede — é o que dá pela
	// CGD mudar de preçário e tornar a medição obsoleta.
	t.Run("a banda de 5 % não chega para a CGD", func(t *testing.T) {
		spreads := map[string]string{}
		for _, cenario := range []string{"variavel/ltv66/30a", "variavel/ltv67/30a", "variavel/ltv68/30a"} {
			o := observacaoDe(t, obs, cenario)
			if o.Oferta.Spread == nil {
				t.Fatalf("%s: veio sem spread", cenario)
			}
			spreads[cenario] = o.Oferta.Spread.String()
		}
		t.Logf("dentro da antiga banda 70: 66%% → %s, 67%% → %s, 68%% → %s",
			spreads["variavel/ltv66/30a"], spreads["variavel/ltv67/30a"], spreads["variavel/ltv68/30a"])

		if spreads["variavel/ltv66/30a"] == spreads["variavel/ltv68/30a"] {
			t.Logf("✅ os LTV de 66 %% e 68 %% passaram a ter o mesmo spread — a CGD mudou de preçário " +
				"desde 2026-07-26, e as fronteiras medidas que a KAN-35 fixou têm de ser varridas de novo")
			return
		}
		t.Logf("⚠️ CONFIRMADO, e continua a ser a razão de ser da EscalaDeLTV: os três LTV têm preços " +
			"diferentes dentro de uma banda de 5 %%. A grelha da KAN-16 guarda-os por intervalo medido.")
	})
}

// A concorrência que a CGD aguenta, medida em vez de escolhida.
//
// ⚠️ É o número que a KAN-7 deixou por decidir: o tecto por banco ficou em 1
// porque não havia medição, e este teste é a medição. Não é portão — é uma
// experiência que se corre à mão, com o resultado a ir para a issue.
func TestE2ETectoDeConcorrenciaDaCGD(t *testing.T) {
	pontos := grelhaDeEnsaio()[:8]

	for _, tecto := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("porBanco=%d", tecto), func(t *testing.T) {
			v := varredorAoVivo(t, tecto)

			inicio := time.Now()
			r := v.Varrer(context.Background(), pontos)
			demorou := time.Since(inicio)

			falhas := 0
			for _, o := range r.Observacoes {
				if !o.Sucesso() {
					falhas++
					t.Logf("  falhou %s: %s", o.Ponto.Cenario, o.Oferta.Erro)
				}
			}
			t.Logf("%d pontos com tecto %d: %s (%s por ponto), %d falhas",
				len(pontos), tecto, demorou.Round(time.Millisecond),
				(demorou / time.Duration(len(pontos))).Round(time.Millisecond), falhas)

			// ⚠️ O que reprova é a CGD começar a recusar, não a demora. Se
			// subir o tecto produzir falhas, o número está encontrado — e é o
			// anterior.
			if falhas > 0 {
				t.Errorf("com %d pedidos em paralelo a CGD falhou %d de %d — este tecto é alto demais",
					tecto, falhas, len(pontos))
			}
		})
	}
}

// --- ajudantes ---------------------------------------------------------------

// nascidoHaAnos dá uma data de nascimento que faz o titular ter exactamente
// esta idade hoje — sem congelar relógio nenhum.
func nascidoHaAnos(n int) dominio.Data {
	return dominio.DataDeInstante(time.Now().AddDate(-n, 0, 0))
}

func varredorAoVivo(t *testing.T, porBanco int) *varrimento.Varredor {
	t.Helper()
	v, err := varrimento.Novo(varrimento.Config{
		Bancos: []bancos.Banco{cgd.Novo(transporte.NovoCliente(nil))},
		Travao: novoTravaoFalso(),
		// ⚠️ Travão falso e não o de Postgres: o que aqui se mede é o banco, e
		// o travão a sério tem o seu teste de integração em internal/infra/travao.
		Prazos:   varrimento.Prazos{Barato: 60 * time.Second},
		PorBanco: porBanco,
	})
	if err != nil {
		t.Fatalf("montar o varredor: %v", err)
	}
	return v
}

// varrerAoVivo corre a grelha e devolve as observações, exigindo que tenham
// saído todas e que nenhuma tenha falhado.
func varrerAoVivo(t *testing.T, porBanco int) []varrimento.Observacao {
	t.Helper()

	pontos := grelhaDeEnsaio()
	inicio := time.Now()
	r := varredorAoVivo(t, porBanco).Varrer(context.Background(), pontos)
	t.Logf("%d pontos em %s", len(pontos), time.Since(inicio).Round(time.Millisecond))

	if len(r.Saltados) != 0 {
		t.Fatalf("nenhum banco devia ter sido saltado: %+v", r.Saltados)
	}
	if len(r.Observacoes) != len(pontos) {
		t.Fatalf("esperava %d observações, vieram %d", len(pontos), len(r.Observacoes))
	}
	falhou := false
	for _, o := range r.Observacoes {
		if !o.Sucesso() {
			t.Errorf("%s: %s", o.Ponto.Cenario, o.Oferta.Erro)
			falhou = true
		}
	}
	if falhou {
		t.FailNow()
	}
	return r.Observacoes
}

func observacaoDe(t *testing.T, obs []varrimento.Observacao, cenario string) varrimento.Observacao {
	t.Helper()
	for _, o := range obs {
		if o.Ponto.Cenario == cenario {
			return o
		}
	}
	t.Fatalf("não há observação para o cenário %q", cenario)
	return varrimento.Observacao{}
}

func prestacaoDe(t *testing.T, obs []varrimento.Observacao, cenario string) dominio.Dinheiro {
	t.Helper()
	o := observacaoDe(t, obs, cenario)
	if o.Oferta.Prestacao == nil {
		t.Fatalf("%s: veio sem prestação", cenario)
	}
	return *o.Oferta.Prestacao
}

func tanDe(t *testing.T, obs []varrimento.Observacao, cenario string) dominio.Taxa {
	t.Helper()
	o := observacaoDe(t, obs, cenario)
	if o.Oferta.TAN == nil {
		t.Fatalf("%s: veio sem TAN", cenario)
	}
	return *o.Oferta.TAN
}

// verFasesFecham confirma que o plano cobre o contrato inteiro. A validação já
// corre dentro do parser; repete-se aqui porque ao vivo o prazo aplicado pode
// não ser o pedido — e é justamente isso que se quer ver declarado.
func verFasesFecham(t *testing.T, o varrimento.Observacao) {
	t.Helper()
	if len(o.Oferta.Fases) == 0 {
		t.Fatal("oferta sem plano de fases")
	}
	ultima := o.Oferta.Fases[len(o.Oferta.Fases)-1]
	prazoPedido := o.Ponto.Pedido.PrazoAnos * 12
	if ultima.AteMes != prazoPedido && len(o.Oferta.Ajustes()) == 0 {
		t.Errorf("o plano acaba ao mês %d, pediram-se %d meses, e não há ajuste nenhum a explicá-lo",
			ultima.AteMes, prazoPedido)
	}
}

// verTANEEuriborMaisSpread afirma a identidade que define uma fase indexada.
//
// ⚠️ É a comparação que apanha o campo trocado. Se o parser lesse o spread da
// fase fixa (a armadilha do Banco CTT e do Santander) ou a taxa base como
// Euribor, esta soma deixava de fechar.
func verTANEEuriborMaisSpread(t *testing.T, o varrimento.Observacao) {
	t.Helper()
	if o.Oferta.EuriborValor == nil || o.Oferta.Spread == nil {
		return // taxa fixa: não há fase indexada onde a identidade faça sentido
	}

	// Na variável a TAN da oferta é a da fase indexada; na mista, a fase
	// indexada é a última.
	indexada := o.Oferta.Fases[len(o.Oferta.Fases)-1].Taxa
	soma := o.Oferta.EuriborValor.Add(*o.Oferta.Spread)
	if !indexada.Equal(soma) {
		t.Errorf("a fase indexada diz TAN %s, e Euribor %s + spread %s dá %s",
			indexada, o.Oferta.EuriborValor, o.Oferta.Spread, soma)
	}
}

// verPrestacaoBateComAFrancesa recalcula a prestação por amortização francesa e
// compara com a que o banco devolveu.
//
// ⚠️ É o resíduo da §7.4 em ponto pequeno, e é a comparação mais valiosa de
// todas: apanha o montante trocado, o prazo trocado, a taxa em unidades erradas
// e a prestação de outra fase. Se a CGD mudar a forma de calcular, aparece aqui
// como número — e não como silêncio.
func verPrestacaoBateComAFrancesa(t *testing.T, o varrimento.Observacao) {
	t.Helper()

	// ⚠️ A primeira fase mede-se pelo varrimento.Residuo — o mesmo código que
	// grava a coluna residuo_prestacao em cada linha do catálogo. Até 2026-07-28
	// esta conta existia só aqui, atrás de `//go:build rede`, e era por isso que
	// a §7.4 não tinha travão nenhum: a comparação que decide se um banco mudou
	// só corria quando alguém corria os testes de rede desse banco.
	residuo, ok := varrimento.Residuo(o)
	if !ok {
		t.Fatal("a observação não deu resíduo nenhum, e tem plano de fases")
	}
	if residuo.Abs().Decimal().GreaterThan(decimal.NewFromFloat(0.05)) {
		t.Errorf("a primeira prestação é %s e diverge da francesa em %s €",
			o.Oferta.Fases[0].Prestacao, residuo.Decimal().StringFixed(2))
	}

	if len(o.Oferta.Fases) < 2 {
		return
	}
	// A segunda fase amortiza o que sobrou, ao ritmo da taxa nova.
	primeira, segunda := o.Oferta.Fases[0], o.Oferta.Fases[1]
	emDivida, err := dominio.CapitalEmDivida(
		o.Ponto.Pedido.Montante, primeira.Taxa, mesesTotais(o), primeira.AteMes)
	if err != nil {
		t.Fatalf("capital em dívida ao fim da fase fixa: %v", err)
	}
	esperadaSegunda, err := dominio.PrestacaoFrancesa(
		emDivida, segunda.Taxa, segunda.AteMes-primeira.AteMes)
	if err != nil {
		t.Fatalf("prestação da fase indexada: %v", err)
	}

	desvioSegunda := segunda.Prestacao.Sub(esperadaSegunda).Abs()
	// Tolerância maior: o banco amortiza com a prestação já arredondada aos
	// cêntimos, e sessenta meses desse arredondamento movem o capital em dívida.
	// É por isso que o resíduo da §7.4 é o da PRIMEIRA fase e não o desta.
	if desvioSegunda.Decimal().GreaterThan(decimal.NewFromInt(2)) {
		t.Errorf("a prestação da fase indexada é %s e a francesa sobre %s € dá %s (desvio de %s €)",
			segunda.Prestacao, emDivida.Decimal().StringFixed(2),
			esperadaSegunda.Decimal().StringFixed(2), desvioSegunda.Decimal().StringFixed(2))
	}
}

// mesesTotais é o prazo que o banco aplicou, lido do plano — e não o pedido.
func mesesTotais(o varrimento.Observacao) int {
	return o.Oferta.Fases[len(o.Oferta.Fases)-1].AteMes
}

// descontoDosPacks extrai da nota quanto valem os packs.
//
// ⚠️ Ler um número de uma frase é feio, e é feio por uma razão: enquanto o
// dominio.Pedido não transportar produtos (KAN-33), a segunda coluna de preço
// da CGD só existe em português. Este ajudante desaparece quando essa issue
// fechar.
var descontoNaNota = regexp.MustCompile(`desceria ([\d.,]+) p\.p\.`)

func descontoDosPacks(o varrimento.Observacao) string {
	for _, nota := range o.Oferta.Notas() {
		if m := descontoNaNota.FindStringSubmatch(nota); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}
