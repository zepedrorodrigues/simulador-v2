//go:build medicao

package varrimento_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/cgd"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O produto cartesiano da CGD: a medição que valida (ou não) o desenho da
// grelha inteiro.
//
//	make medicao
//
// ⚠️ A tag é `medicao` e não `rede` desde a KAN-47: por trás da `rede` este
// teste era disparado pelo `make teste-rede`, que ninguém corre com a intenção
// de mandar mil pedidos à CGD.
//
// ⚠️ **Corre-se em hora morta, e a hora escolhe-se antes de o disparar.** São
// mais de mil pedidos a um simulador público alheio, ao longo de dezenas de
// minutos. É o único teste deste repositório que carrega um sistema de terceiros
// durante mais de uma hora.
//
// # A pergunta
//
// A KAN-16 diz que quantos pontos são precisos **não se decide por argumento**.
// A grelha guarda o PARÂMETRO e não o resultado (§4), e isso só funciona se a
// função de preço for separável — spread por intervalo de LTV, mais uma
// observação por período fixo, mais desvios aditivos. Se não for, são milhares
// de pontos por banco e o desenho não se aguenta.
//
// Duas hipóteses, e cada uma tem aqui um subteste:
//
//	H1 — o spread da fase indexada depende SÓ do LTV. Não do prazo, não do
//	     montante. Se depender, a grelha precisa de mais uma dimensão.
//	H2 — a taxa da fase fixa depende SÓ do período fixo, como a §4 afirma ao
//	     dizer que é «a curva de funding do banco sobre o período fixo». O
//	     DOSSIE-BANCOS.md tem o indício contrário do Novo Banco («a mista a 25 e
//	     30 dá o preço de 20»), e é o que aqui se mede na CGD.
//
// # Porque é a CGD
//
// É «o mais simples dos dez» e **ignora dados pessoais por completo**
// (DOSSIE-BANCOS.md), logo o varrimento reproduz a resposta dela exactamente,
// sem estimar nada.
//
// ⚠️ **E é por isso que vai fazer isto parecer mais fácil do que é.** É o único
// banco onde não há erro de estimativa nenhum. O Novo Banco, que recebe data de
// nascimento e rendimento a sério, é que traz o problema do TAEG. Não tirar
// daqui a conclusão de que está resolvido.

// O plano do cartesiano, e a aritmética do seu custo.
//
// 121 LTV × 10 famílias = 1 210 pedidos. A ~1,1 s por pedido com um de cada vez
// (medido a 2026-07-26 contra a CGD) e com dois em paralelo, são cerca de 11
// minutos, mais o minuto da escala.
//
// ⚠️ O passo de 0,5 p.p. é metade do da descoberta (1 p.p.): tem de ser mais
// fino do que a grelha que está a validar, senão o cartesiano só confirma os
// pontos que a grelha já viu.
const (
	cartesianoDe    = "0.30"
	cartesianoAte   = "0.90" // a CGD financia até 90 % na própria
	cartesianoPasso = "0.005"

	// TectoDePedidos trava antes de tocar na rede. Um plano que cresça por
	// engano — um passo mal escrito, uma família a mais — não se descobre a
	// meio de uma corrida contra um banco a sério.
	tectoDePedidos = 1500

	// cartesianoPorBanco é quantos pedidos concorrentes se fazem à CGD. É o
	// mesmo 2 do varrimento a sério, e pela mesma razão: quadruplicar a carga
	// num simulador público para poupar tempo a uma medição que ninguém espera
	// não é uma troca que se faça por poder fazer.
	cartesianoPorBanco = 2
)

// familia é uma coluna do cartesiano: tudo igual excepto o LTV.
type familia struct {
	nome    string
	tipo    dominio.TipoTaxa
	periodo *int
	prazo   int
}

func familiasDoCartesiano() []familia {
	anos := func(n int) *int { return &n }
	return []familia{
		// Variável a quatro prazos: é aqui que H1 se mede. O spread não devia
		// mexer-se com o prazo.
		{"variavel/10a", dominio.TaxaVariavel, nil, 10},
		{"variavel/20a", dominio.TaxaVariavel, nil, 20},
		{"variavel/30a", dominio.TaxaVariavel, nil, 30},
		{"variavel/40a", dominio.TaxaVariavel, nil, 40},

		// Mista: a fase indexada leva o mesmo spread da variável, ou não.
		{"mista/5a", dominio.TaxaMista, anos(5), 30},
		{"mista/10a", dominio.TaxaMista, anos(10), 30},
		{"mista/20a", dominio.TaxaMista, anos(20), 30},

		// Fixa: é aqui que H2 se mede. Na CGD a fixa é ao prazo todo, por isso
		// o prazo É o período.
		{"fixa/10a", dominio.TaxaFixa, nil, 10},
		{"fixa/20a", dominio.TaxaFixa, nil, 20},
		{"fixa/30a", dominio.TaxaFixa, nil, 30},
	}
}

// ponto é um pedido do cartesiano e o que dele saiu.
type ponto struct {
	familia familia
	ltv     dominio.Racio
	oferta  dominio.Oferta
	erro    error
}

func (p ponto) ok() bool { return p.erro == nil && p.oferta.Sucesso() }

// spreadIndexado é o spread da fase indexada — o que a escala prevê.
func (p ponto) spreadIndexado() (dominio.Taxa, bool) {
	if !p.ok() || p.oferta.Spread == nil {
		return dominio.Taxa{}, false
	}
	return *p.oferta.Spread, true
}

func TestCartesianoDaCGD(t *testing.T) {
	ctx := context.Background()
	hoje := dominio.DataDeInstante(time.Now())
	banco := cgd.Novo(transporte.NovoCliente(nil))

	ltvs := escadaDeLTV(t)
	familias := familiasDoCartesiano()
	total := len(ltvs) * len(familias)
	if total > tectoDePedidos {
		t.Fatalf("o plano são %d pedidos e o tecto é %d — rever o passo ou as famílias antes de bater no banco",
			total, tectoDePedidos)
	}
	t.Logf("plano: %d LTV × %d famílias = %d pedidos à CGD", len(ltvs), len(familias), total)

	// 1. A grelha, medida como o varrimento a sério a mede.
	inicio := time.Now()
	descoberta, err := grelha.DescobrirBanco(ctx, banco, grelha.Referencia{}, hoje, grelha.Config{}, time.Now)
	if err != nil {
		t.Fatalf("medir a escala de LTV da CGD: %v", err)
	}
	t.Logf("escala medida em %s: %d degraus, %d amostras, %d falhas",
		time.Since(inicio).Round(time.Second),
		len(descoberta.Degraus), descoberta.Amostras, descoberta.Falhas)
	for _, d := range descoberta.Escala.Degraus() {
		t.Logf("  degrau (%s ; %s] → spread %s (resolvido: %t)", d.De, d.Ate, d.Spread, d.Resolvido())
	}

	// 2. O cartesiano.
	inicio = time.Now()
	pontos := correrCartesiano(ctx, t, banco, hoje, ltvs, familias)
	decorrido := time.Since(inicio)

	bons, recusas := 0, 0
	for _, p := range pontos {
		if p.ok() {
			bons++
			continue
		}
		recusas++
	}
	t.Logf("cartesiano: %d pedidos em %s (%s por pedido), %d respostas, %d recusas",
		len(pontos), decorrido.Round(time.Second),
		(decorrido / time.Duration(len(pontos))).Round(time.Millisecond), bons, recusas)

	if bons == 0 {
		t.Fatal("a CGD não respondeu a um único ponto do cartesiano")
	}

	// 3. O que a grelha reproduz.
	t.Run("H1: o spread da fase indexada sai só do LTV", func(t *testing.T) {
		verEscalaReproduzOSpread(t, descoberta.Escala, pontos)
	})

	t.Run("H2: a taxa da fase fixa é a base do período mais o spread do LTV", func(t *testing.T) {
		verTaxaFixaEBaseMaisSpread(t, descoberta.Escala, pontos)
	})

	t.Run("a prestação sai por cálculo local", func(t *testing.T) {
		verPrestacaoSaiDaFrancesa(t, pontos)
	})
}

// TestAFixaDaCGDEBaseMaisSpreadDoLTV é a H2 sozinha, e custa ~100 pedidos em vez
// dos 1 210 do cartesiano.
//
//	go test -race -tags medicao -timeout 20m -run FixaDaCGD -v ./internal/aplicacao/varrimento/
//
// Existe porque a relação que ela afirma foi **descoberta** pelo cartesiano de
// 2026-07-28, e uma relação descoberta tem de passar a ser vigiada: se a CGD
// deixar de preçar a fixa assim, isto falha por 100 pedidos e não por 1 210.
//
// Mede um LTV dentro de cada degrau da escala — é onde a relação se pode partir,
// e amostrar mais dentro do mesmo degrau só repetia o mesmo preço.
func TestAFixaDaCGDEBaseMaisSpreadDoLTV(t *testing.T) {
	ctx := context.Background()
	hoje := dominio.DataDeInstante(time.Now())
	banco := cgd.Novo(transporte.NovoCliente(nil))

	descoberta, err := grelha.DescobrirBanco(ctx, banco, grelha.Referencia{}, hoje, grelha.Config{}, time.Now)
	if err != nil {
		t.Fatalf("medir a escala de LTV da CGD: %v", err)
	}

	// Um ponto por degrau: o extremo de cima, que é onde o spread do degrau foi
	// efectivamente observado.
	var ltvs []dominio.Racio
	for _, d := range descoberta.Escala.Degraus() {
		ltvs = append(ltvs, d.Ate)
	}
	t.Logf("escala: %d degraus, %d amostras", len(ltvs), descoberta.Amostras)

	familias := []familia{
		{"fixa/10a", dominio.TaxaFixa, nil, 10},
		{"fixa/20a", dominio.TaxaFixa, nil, 20},
		{"fixa/30a", dominio.TaxaFixa, nil, 30},
	}

	pontos := correrCartesiano(ctx, t, banco, hoje, ltvs, familias)
	verTaxaFixaEBaseMaisSpread(t, descoberta.Escala, pontos)
}

// verEscalaReproduzOSpread é a H1: para cada ponto observado, o spread que a
// escala prevê a partir do LTV tem de ser o que o banco praticou.
//
// ⚠️ Um degrau POR RESOLVER prevê o lado mais caro, com nota (MCD, Anexo I,
// Parte II, alínea (d)). Aí a previsão está certa por construção e é conservadora
// — conta-se à parte, para não inflacionar nem os acertos nem os erros.
func verEscalaReproduzOSpread(t *testing.T, escala dominio.EscalaDeLTV, pontos []ponto) {
	t.Helper()

	var (
		medidos, batem, conservadores, fora int
		maiorDesvio                         decimal.Decimal
		ondeODesvioEMaior                   string
	)

	for _, p := range pontos {
		observado, temSpread := p.spreadIndexado()
		if !temSpread {
			continue // taxa fixa pura: não há fase indexada
		}
		medidos++

		previsto, nota, err := escala.SpreadEm(p.ltv)
		if err != nil {
			fora++
			if errors.Is(err, dominio.ErrLTVForaDaEscala) {
				continue
			}
			t.Errorf("SpreadEm(%s): %v", p.ltv, err)
			continue
		}

		desvio := previsto.Decimal().Sub(observado.Decimal()).Abs()
		switch {
		case desvio.IsZero():
			batem++
		case nota != "" && previsto.Cmp(observado) > 0:
			// Degrau por resolver, servido pelo lado caro: o cliente não paga
			// mais do que o anunciado, que é o que a nota promete.
			conservadores++
		default:
			if desvio.GreaterThan(maiorDesvio) {
				maiorDesvio = desvio
				ondeODesvioEMaior = fmt.Sprintf("%s em LTV %s: previsto %s, observado %s",
					p.familia.nome, p.ltv, previsto, observado)
			}
		}
	}

	errados := medidos - batem - conservadores - fora
	t.Logf("H1: %d observações com spread — %d exactas, %d conservadoras (degrau por resolver), %d fora da escala, %d erradas",
		medidos, batem, conservadores, fora, errados)

	if errados > 0 {
		t.Errorf("a escala de LTV não reproduz %d de %d observações. Maior desvio: %s p.p. — %s.\n"+
			"Se o desvio depende do prazo ou do montante, o spread não é função só do LTV e a grelha precisa de mais uma dimensão (§4)",
			errados, medidos, maiorDesvio.StringFixed(3), ondeODesvioEMaior)
	}
}

// verTaxaFixaEBaseMaisSpread é a H2, reescrita pelo que a corrida de 2026-07-28
// mediu.
//
// ⚠️ **A hipótese original estava errada, e o cartesiano matou-a.** A §4 dizia
// que «basta uma observação por período da lista» porque a taxa fixa era «a curva
// de funding do banco sobre o período fixo». Não é só isso: na CGD, a TAN da fixa
// **muda com o LTV**, e muda nas MESMAS fronteiras da variável e com a MESMA
// altura de degrau.
//
// O que se mediu, nos 121 LTV × 3 prazos de fixa:
//
//	                 LTV 0,30   0,335    0,67    0,68
//	escala (spread)     1,950   2,000   2,050   1,350
//	fixa a 10 anos      5,450   5,500   5,550   4,850   → base 3,500
//	fixa a 20 anos      5,600   5,650   5,700   5,000   → base 3,650
//	fixa a 30 anos      5,850   5,900   5,950   5,250   → base 3,900
//
// **TAN_fixa(período, ltv) = base(período) + spread(ltv)**, e a base é constante
// ao cêntimo em todos os degraus. A queda de 0,70 p.p. aos 68 % é a mesma altura
// de degrau que a variável tem no mesmo sítio.
//
// ⚠️ Consequência para a grelha, e é boa notícia: **não multiplica**. O que se
// guarda por período continua a ser UMA observação — mas o que dela se extrai é
// a BASE, e não a TAN. Quem responde soma o spread do intervalo do cliente, tal
// como já faz na variável com a Euribor. Uma grelha que guardasse a TAN servia o
// preço do LTV a que a mediu a toda a gente.
func verTaxaFixaEBaseMaisSpread(t *testing.T, escala dominio.EscalaDeLTV, pontos []ponto) {
	t.Helper()

	type referencia struct {
		base decimal.Decimal
		ltv  dominio.Racio
	}
	primeira := map[string]referencia{}
	medidos := map[string]int{}

	for _, p := range pontos {
		if p.familia.tipo != dominio.TaxaFixa || !p.ok() || p.oferta.TAN == nil {
			continue
		}
		spread, _, err := escala.SpreadEm(p.ltv)
		if err != nil {
			continue // fora da escala: não há spread com que descontar
		}

		base := p.oferta.TAN.Decimal().Sub(spread.Decimal())
		medidos[p.familia.nome]++

		ref, visto := primeira[p.familia.nome]
		if !visto {
			primeira[p.familia.nome] = referencia{base: base, ltv: p.ltv}
			continue
		}
		if !base.Equal(ref.base) {
			t.Errorf("%s: TAN menos spread dá %s em LTV %s e %s em LTV %s — "+
				"a taxa fixa deixou de ser base + spread do LTV, e a grelha passa a precisar de mais uma dimensão",
				p.familia.nome, ref.base.StringFixed(3), ref.ltv, base.StringFixed(3), p.ltv)
			delete(primeira, p.familia.nome)
		}
	}

	for nome, ref := range primeira {
		t.Logf("H2: %s tem base %s p.p. constante em %d LTV medidos", nome, ref.base.StringFixed(3), medidos[nome])
	}
}

// verPrestacaoSaiDaFrancesa confirma, sobre mil e duzentos pontos, o que a §4
// diz na coluna «sai por cálculo local»: a prestação não se guarda, calcula-se.
//
// É o resíduo da §7.4 aplicado ao cartesiano inteiro, e é a afirmação mais larga
// que este repositório faz sobre a CGD.
func verPrestacaoSaiDaFrancesa(t *testing.T, pontos []ponto) {
	t.Helper()

	var (
		medidos            int
		maior              decimal.Decimal
		ondeOResiduoEMaior string
	)
	for _, p := range pontos {
		if !p.ok() || len(p.oferta.Fases) == 0 {
			continue
		}
		medidos++

		primeira := p.oferta.Fases[0]
		meses := p.oferta.Fases[len(p.oferta.Fases)-1].AteMes
		esperada, err := dominio.PrestacaoFrancesa(montanteDoPonto(p), primeira.Taxa, meses)
		if err != nil {
			t.Errorf("%s em LTV %s: %v", p.familia.nome, p.ltv, err)
			continue
		}

		residuo := primeira.Prestacao.Sub(esperada).Abs().Decimal()
		if residuo.GreaterThan(maior) {
			maior, ondeOResiduoEMaior = residuo, fmt.Sprintf("%s em LTV %s", p.familia.nome, p.ltv)
		}
	}

	t.Logf("resíduo da prestação em %d observações: maior %s € (%s)",
		medidos, maior.StringFixed(4), ondeOResiduoEMaior)

	// O arredondamento aos cêntimos do próprio banco. Acima disto não é
	// arredondamento — é o modelo a não descrever o que a CGD faz.
	if maior.GreaterThan(decimal.NewFromFloat(0.05)) {
		t.Errorf("a prestação da CGD diverge da amortização francesa em %s € (%s): "+
			"a §4 diz que a prestação sai por CÁLCULO e não por consulta",
			maior.StringFixed(2), ondeOResiduoEMaior)
	}
}

// --- a corrida ------------------------------------------------------------------

// correrCartesiano faz os pedidos, no máximo cartesianoPorBanco de cada vez.
func correrCartesiano(
	ctx context.Context, t *testing.T, banco bancos.Banco,
	hoje dominio.Data, ltvs []dominio.Racio, familias []familia,
) []ponto {
	t.Helper()

	pontos := make([]ponto, 0, len(ltvs)*len(familias))
	for _, f := range familias {
		for _, ltv := range ltvs {
			pontos = append(pontos, ponto{familia: f, ltv: ltv})
		}
	}

	indices := make(chan int)
	var wg sync.WaitGroup
	for range cartesianoPorBanco {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indices {
				p := &pontos[i]
				pedido, err := pedidoDoPonto(*p, hoje)
				if err != nil {
					p.erro = err
					continue
				}
				// Prazo por pedido: um ponto lento não segura a corrida toda.
				ctxPonto, cancelar := context.WithTimeout(ctx, 60*time.Second)
				p.oferta, p.erro = banco.Simular(ctxPonto, pedido)
				cancelar()
			}
		}()
	}
	for i := range pontos {
		indices <- i
	}
	close(indices)
	wg.Wait()

	return pontos
}

// pedidoDoPonto monta o pedido, com o montante que dá o LTV pedido.
//
// ⚠️ Valida antes de ir à rede, pela mesma razão que o amostrador: um pedido que
// o próprio domínio recusa gasta um pedido ao banco para receber um erro nosso.
func pedidoDoPonto(p ponto, hoje dominio.Data) (dominio.Pedido, error) {
	valorImovel := grelha.ValorImovelOmissao
	pedido := dominio.Pedido{
		ValorImovel:     valorImovel,
		Montante:        dominio.MontanteParaLTV(valorImovel, p.ltv),
		PrazoAnos:       p.familia.prazo,
		TipoTaxa:        p.familia.tipo,
		PeriodoFixoAnos: p.familia.periodo,
		Finalidade:      grelha.FinalidadeDeReferencia,
		Localizacao:     dominio.LocalizacaoContinente,
		Titulares: []dominio.Titular{
			{DataNascimento: nascidoHaAnos(grelha.IdadeOmissao)},
		},
	}
	if err := pedido.Validar(hoje); err != nil {
		return dominio.Pedido{}, fmt.Errorf("%s em LTV %s: %w", p.familia.nome, p.ltv, err)
	}
	return pedido, nil
}

func montanteDoPonto(p ponto) dominio.Dinheiro {
	return dominio.MontanteParaLTV(grelha.ValorImovelOmissao, p.ltv)
}

// escadaDeLTV são os LTV a varrer, de cartesianoDe a cartesianoAte ao passo.
func escadaDeLTV(t *testing.T) []dominio.Racio {
	t.Helper()

	de, ate := racioDoTeste(t, cartesianoDe), racioDoTeste(t, cartesianoAte)
	passo := racioDoTeste(t, cartesianoPasso)

	var ltvs []dominio.Racio
	for r := de; r.Cmp(ate) <= 0; r = r.Add(passo) {
		ltvs = append(ltvs, r)
	}
	return ltvs
}

func racioDoTeste(t *testing.T, s string) dominio.Racio {
	t.Helper()
	r, err := dominio.RacioDeTexto(s)
	if err != nil {
		t.Fatalf("rácio inválido %q: %v", s, err)
	}
	return r
}
