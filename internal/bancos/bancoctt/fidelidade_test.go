//go:build rede

package bancoctt_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/bancoctt"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Fidelidade: a observação que guardamos é o que o banco respondeu?
//
// É a mesma pergunta estreita que se fez à CGD, ao Novo Banco e ao Montepio, e
// a mesma resposta: **entre o corpo que chegou pela rede e o dominio.Oferta que
// sai do Simular, perdeu-se ou torceu-se alguma coisa?** Não é «o número está
// certo» — isso depende do preçário do Banco CTT.
//
//	go test -tags rede -timeout 60m -run TestFidelidade ./internal/bancos/bancoctt/ -v
//
// O tamanho da amostra vem de BANCOCTT_AMOSTRAS (por omissão 250).
//
// ⚠️ **O que dá valor a cada amostra não é ser mais uma: é o espelho.** Cada
// resposta é lida uma segunda vez por um caminho escrito de propósito para não
// se parecer com o nosso — `map[string]any` e `float64` em vez de struct com
// tags, `json.Number` e leitura de número em formato português. Se os dois
// concordarem, ou ambos estão certos ou erraram da mesma maneira; e errar da
// mesma maneira por dois desenhos diferentes é muito menos provável do que
// errar uma vez.
//
// ⚠️ **Corre-se em hora morta.** São centenas de pedidos a um simulador público
// alheio, e o USO-RESPONSAVEL.md aplica-se-lhe inteiro. Dois trabalhadores, como
// no varrimento.

func TestFidelidadeDoQueGuardamosFaceAoQueOBancoRespondeu(t *testing.T) {
	alvo := alvoDeAmostras(t)
	t.Logf("alvo: %d ofertas conferidas", alvo)

	var (
		conferidas atomic.Int64
		campos     atomic.Int64
		recusadas  atomic.Int64
		falhas     atomic.Int64
		bytesLidos atomic.Int64
		pedidos    atomic.Int64
	)

	const trabalhadores = 2

	inicio := time.Now()
	entrada := make(chan casoCTT)
	var wg sync.WaitGroup

	for range trabalhadores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Cada trabalhador com o seu gravador: partilhar um seria comparar a
			// oferta de um pedido com o corpo de outro.
			grav := &gravadorCTT{real: transporte.NovoCliente(nil)}
			banco := bancoctt.Novo(grav)

			for c := range entrada {
				grav.reiniciar()

				ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
				oferta, err := banco.Simular(ctx, c.pedido)
				cancelar()

				pedidos.Add(grav.pedidos())
				bytesLidos.Add(grav.bytes())

				corpo := grav.ultimo()
				if corpo == nil {
					// Não chegou a haver resposta — nem o banco recusou, nem nós
					// lemos nada. Não é amostra de fidelidade da leitura.
					recusadas.Add(1)
					continue
				}
				if err != nil {
					if !recusaFielCTT(corpo, err) {
						falhas.Add(1)
						t.Errorf("%s: o erro não corresponde ao corpo %q: %v", c.nome, primeiros(corpo), err)
					}
					recusadas.Add(1)
					continue
				}

				n, err := conferirContraOEspelhoCTT(corpo, oferta, c.pedido)
				conferidas.Add(1)
				campos.Add(int64(n))
				if err != nil {
					falhas.Add(1)
					t.Errorf("%s: %v", c.nome, err)
				}
			}
		}()
	}

	// ⚠️ Alimenta-se até haver **ofertas conferidas** que cheguem, e não até
	// haver pedidos feitos: um caso recusado pelo banco não é amostra de
	// fidelidade da leitura. O tecto é a guarda contra um dia em que quase tudo
	// seja recusado.
	gerados := 0
	for _, c := range formasDeRespostaCTT(t) {
		entrada <- c
		gerados++
	}
	r := rand.New(rand.NewPCG(20260728, 12))
	for conferidas.Load() < int64(alvo) && gerados < alvo*3 {
		entrada <- casoAleatorioCTT(t, r, gerados)
		gerados++
	}
	close(entrada)
	wg.Wait()

	demorou := time.Since(inicio)
	n, f := conferidas.Load(), falhas.Load()
	t.Logf("%d pedidos gerados para %d ofertas conferidas", gerados, n)
	t.Logf("%d ofertas conferidas, %d campos comparados, %d recusas, %d falhas, em %s",
		n, campos.Load(), recusadas.Load(), f, demorou.Round(time.Second))
	t.Logf("custo para o Banco CTT: %d pedidos HTTP, %.1f MB, %s por simulação",
		pedidos.Load(), float64(bytesLidos.Load())/(1<<20),
		(demorou / time.Duration(max(gerados, 1))).Round(time.Millisecond))

	// ⚠️ A conclusão escreve-se com a regra dos três: com zero falhas em n
	// amostras, o limite superior do erro a 95 % de confiança é 3/n. Não é
	// «acertámos 100 %» — é «se errássemos mais do que isto, era pouco provável
	// não termos visto».
	if f == 0 && n > 0 {
		t.Logf("MEDIDO: 0 falhas em %d ofertas → erro de leitura ≤ %.3f %% (95 %% de confiança, regra dos três)",
			n, 300.0/float64(n))
		if n < 3000 {
			t.Logf("⚠️ para sustentar «≤ 0,1 %%» são precisas 3000 ofertas conferidas; esta corrida tem %d. "+
				"Correr com BANCOCTT_AMOSTRAS=3000.", n)
		}
	}
}

// --- o espelho ------------------------------------------------------------------

// conferirContraOEspelhoCTT lê o corpo por um caminho independente e compara
// tudo o que a Oferta transporta. Devolve quantos campos comparou.
func conferirContraOEspelhoCTT(corpo []byte, o dominio.Oferta, p dominio.Pedido) (int, error) {
	var cru map[string]any
	if err := json.Unmarshal(corpo, &cru); err != nil {
		return 0, fmt.Errorf("o corpo não é JSON: %w", err)
	}
	dados, _ := cru["data"].(map[string]any)
	if dados == nil {
		return 0, fmt.Errorf("a resposta não traz data, mas o Simular devolveu uma oferta")
	}

	// A coluna que o pedido escolheu. ⚠️ É a mesma decisão do parser, feita aqui
	// pelo sufixo do nome do campo em vez de por um método — se o parser
	// escolhesse a coluna errada, isto discordava.
	sufixoTaxa, sufixoPrestacao := "WithoutBenefits", "WithoutBonification"
	if len(p.ProdutosDoBanco("bancoctt")) > 0 {
		sufixoTaxa, sufixoPrestacao = "WithBenefits", "WithBonification"
	}

	var problemas []string
	comparados := 0

	verTaxa := func(campo string, veio *dominio.Taxa) {
		comparados++
		if err := igualTaxaCTT(dados[campo], veio); err != nil {
			problemas = append(problemas, fmt.Sprintf("%s: %v", campo, err))
		}
	}
	verDinheiro := func(campo string, veio *dominio.Dinheiro) {
		comparados++
		if err := igualDinheiroCTT(dados[campo], veio); err != nil {
			problemas = append(problemas, fmt.Sprintf("%s: %v", campo, err))
		}
	}

	verTaxa("TAN"+sufixoTaxa, o.TAN)
	verTaxa("TAEG"+sufixoTaxa, o.TAEG)
	verDinheiro("MTIC"+sufixoTaxa, o.MTIC)
	verDinheiro("MonthlyInstallment"+sufixoPrestacao, o.Prestacao)

	// ⚠️ A afirmação que mais interessa deste banco: qual dos dois `Spread` é o
	// contratual. Na variável é o de topo; na mista é o `VariableSpread`; e numa
	// fixa pura não há nenhum — o campo de topo vem igual à TAN, e publicá-lo
	// punha o Banco CTT com 4,650 de spread ao lado dos 1,350 dos outros.
	comparados += 3
	switch p.TipoTaxa {
	case dominio.TaxaVariavel:
		if err := igualTaxaCTT(dados["Spread"+sufixoTaxa], o.Spread); err != nil {
			problemas = append(problemas, fmt.Sprintf("Spread da variável: %v", err))
		}
		if err := igualTaxaCTT(dados["IndexRate"], o.EuriborValor); err != nil {
			problemas = append(problemas, fmt.Sprintf("IndexRate: %v", err))
		}
		if err := indexanteBate(textoBrutoCTT(dados["IndexRateDescription"]), o.Indexante); err != nil {
			problemas = append(problemas, fmt.Sprintf("IndexRateDescription: %v", err))
		}

	case dominio.TaxaMista:
		if err := igualTaxaCTT(dados["VariableSpread"+sufixoTaxa], o.Spread); err != nil {
			problemas = append(problemas, fmt.Sprintf("VariableSpread da mista: %v", err))
		}
		if err := igualTaxaCTT(dados["VariableIndexRate"], o.EuriborValor); err != nil {
			problemas = append(problemas, fmt.Sprintf("VariableIndexRate: %v", err))
		}
		if err := indexanteBate(textoBrutoCTT(dados["VariableIndexRateDescription"]), o.Indexante); err != nil {
			problemas = append(problemas, fmt.Sprintf("VariableIndexRateDescription: %v", err))
		}

	case dominio.TaxaFixa:
		if o.Spread != nil || o.EuriborValor != nil || o.Indexante != "" {
			problemas = append(problemas, fmt.Sprintf(
				"a fixa pura publicou spread %v, Euribor %v e indexante %q, e o campo `Spread` do banco vem igual à TAN",
				o.Spread, o.EuriborValor, o.Indexante))
		}
	}

	// As fases: o plano tem de cobrir o prazo que o banco devolveu, e a última
	// tem de ser a da coluna certa.
	comparados++
	if err := fasesBatem(dados, o, p, sufixoTaxa, sufixoPrestacao); err != nil {
		problemas = append(problemas, err.Error())
	}

	if len(problemas) > 0 {
		return comparados, fmt.Errorf("%d divergência(s): %s", len(problemas), strings.Join(problemas, "; "))
	}
	return comparados, nil
}

func fasesBatem(dados map[string]any, o dominio.Oferta, p dominio.Pedido, sufixoTaxa, sufixoPrestacao string) error {
	meses, ok := dados["AmortizationPeriodMonths"].(float64)
	if !ok {
		return fmt.Errorf("a resposta não traz AmortizationPeriodMonths")
	}
	if len(o.Fases) == 0 {
		return fmt.Errorf("a oferta veio sem plano de fases")
	}
	if ultima := o.Fases[len(o.Fases)-1].AteMes; float64(ultima) != meses {
		return fmt.Errorf("o plano acaba ao mês %d e o banco devolveu %.0f meses", ultima, meses)
	}
	if p.TipoTaxa != dominio.TaxaMista {
		if len(o.Fases) != 1 {
			return fmt.Errorf("uma taxa %s tem uma fase, vieram %d", p.TipoTaxa, len(o.Fases))
		}
		return nil
	}

	if len(o.Fases) != 2 {
		return fmt.Errorf("a mista tem duas fases, vieram %d", len(o.Fases))
	}
	fixos, ok := dados["LoanFixedPeriodMonths"].(float64)
	if !ok {
		return fmt.Errorf("a mista veio sem LoanFixedPeriodMonths")
	}
	if float64(o.Fases[0].AteMes) != fixos {
		return fmt.Errorf("a fase fixa acaba ao mês %d e o banco diz %.0f", o.Fases[0].AteMes, fixos)
	}
	if err := igualTaxaCTT(dados["VariableTAN"+sufixoTaxa], &o.Fases[1].Taxa); err != nil {
		return fmt.Errorf("a TAN da fase indexada: %w", err)
	}
	if err := igualDinheiroCTT(
		dados["VariableMonthlyInstallmentAmount"+sufixoPrestacao], &o.Fases[1].Prestacao); err != nil {
		return fmt.Errorf("a prestação da fase indexada: %w", err)
	}
	return nil
}

// igualTaxaCTT compara o texto em formato português com a taxa que saiu.
//
// ⚠️ O caminho é deliberadamente outro: aqui lê-se por strings.Replace e
// ParseFloat, e o parser lê por decimal. Dois caminhos que concordam é o que dá
// valor à amostra.
func igualTaxaCTT(bruto any, veio *dominio.Taxa) error {
	texto := textoBrutoCTT(bruto)
	if strings.TrimSpace(texto) == "" {
		if veio != nil {
			return fmt.Errorf("o banco não devolveu nada e a oferta traz %s", veio)
		}
		return nil
	}
	if veio == nil {
		return fmt.Errorf("o banco devolveu %q e a oferta não traz nada", texto)
	}
	esperado, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(texto), ",", "."), 64)
	if err != nil {
		return fmt.Errorf("o corpo traz %q, que não é um número: %w", texto, err)
	}
	obtido, _ := veio.Decimal().Float64()
	if !quaseIgual(esperado, obtido) {
		return fmt.Errorf("o corpo traz %q e a oferta %s", texto, veio)
	}
	return nil
}

func igualDinheiroCTT(bruto any, veio *dominio.Dinheiro) error {
	n, ok := bruto.(float64)
	if !ok {
		if veio != nil {
			return fmt.Errorf("o banco não devolveu número e a oferta traz %s", veio)
		}
		return nil
	}
	if veio == nil {
		return fmt.Errorf("o banco devolveu %v e a oferta não traz nada", n)
	}
	obtido, _ := veio.Decimal().Float64()
	if !quaseIgual(n, obtido) {
		return fmt.Errorf("o corpo traz %v e a oferta %s", n, veio)
	}
	return nil
}

// quaseIgual compara com a folga do float64, que é o tipo do ESPELHO e não o
// nosso. O parser não passa por float em lado nenhum.
func quaseIgual(a, b float64) bool {
	d := a - b
	return d < 0.0005 && d > -0.0005
}

func indexanteBate(descricao string, veio dominio.Indexante) error {
	texto := strings.ToUpper(descricao)
	switch {
	case strings.Contains(texto, "12M"):
		if veio != dominio.Euribor12M {
			return fmt.Errorf("o banco diz %q e a oferta traz %q", descricao, veio)
		}
	case strings.Contains(texto, "6M"):
		if veio != dominio.Euribor6M {
			return fmt.Errorf("o banco diz %q e a oferta traz %q", descricao, veio)
		}
	case strings.Contains(texto, "3M"):
		if veio != dominio.Euribor3M {
			return fmt.Errorf("o banco diz %q e a oferta traz %q", descricao, veio)
		}
	default:
		return fmt.Errorf("o banco etiquetou %q, e a oferta traz %q", descricao, veio)
	}
	return nil
}

func textoBrutoCTT(v any) string {
	s, _ := v.(string)
	return s
}

// recusaFielCTT confirma que uma recusa nossa corresponde ao que o corpo diz —
// e não a uma leitura falhada de uma resposta boa.
func recusaFielCTT(corpo []byte, err error) bool {
	var cru map[string]any
	if e := json.Unmarshal(corpo, &cru); e != nil {
		return true // corpo ilegível: recusar é o que se espera
	}
	sucesso, _ := cru["success"].(bool)
	if !sucesso {
		return true
	}
	// O banco disse que correu bem e nós recusámos: só é fiel se o erro for de
	// leitura, e nesse caso ele nomeia o que não se conseguiu ler.
	return strings.Contains(err.Error(), "Não se conseguiu ler")
}

// --- os casos --------------------------------------------------------------------

type casoCTT struct {
	nome   string
	pedido dominio.Pedido
}

// formasDeRespostaCTT são as formas que a lógica distingue, e correm todas antes
// dos aleatórios: cada tipo de taxa, cada período da mista, cada prazo da fixa,
// com e sem produtos.
func formasDeRespostaCTT(t *testing.T) []casoCTT {
	t.Helper()
	anos := func(n int) *int { return &n }

	base := pedidoBase(t)
	var casos []casoCTT

	comProdutos := base
	comProdutos.Produtos = []string{bancoctt.ProdutoVendasAssociadas}
	casos = append(casos,
		casoCTT{"variável sem produtos", base},
		casoCTT{"variável com vendas associadas", comProdutos},
	)

	for _, periodo := range []int{1, 2, 3, 5} {
		p := base
		p.TipoTaxa = dominio.TaxaMista
		p.PeriodoFixoAnos = anos(periodo)
		casos = append(casos, casoCTT{fmt.Sprintf("mista %da", periodo), p})
	}
	for _, prazo := range []int{30, 34} {
		p := base
		p.TipoTaxa = dominio.TaxaFixa
		p.PrazoAnos = prazo
		casos = append(casos, casoCTT{fmt.Sprintf("fixa %da", prazo), p})
	}
	return casos
}

// casoAleatorioCTT varia montante, imóvel, prazo, modalidade e produtos.
//
// ⚠️ O titular tem sempre idade para o prazo: o que se está a medir é a
// fidelidade da LEITURA, e uma recusa por idade não é amostra dela.
func casoAleatorioCTT(t *testing.T, r *rand.Rand, i int) casoCTT {
	t.Helper()
	anos := func(n int) *int { return &n }

	p := pedidoBase(t)
	p.ValorImovel = dominio.DinheiroDeInteiro(int64(100_000 + r.IntN(80)*10_000))
	// Entre 30 % e 90 % do imóvel.
	ltv := 30 + r.IntN(61)
	p.Montante = dominio.DinheiroDeDecimal(
		p.ValorImovel.Decimal().Mul(decimal.NewFromInt(int64(ltv))).Div(decimal.NewFromInt(100)).Round(2))
	p.PrazoAnos = 10 + r.IntN(31)

	switch r.IntN(3) {
	case 0:
		p.TipoTaxa = dominio.TaxaVariavel
	case 1:
		p.TipoTaxa = dominio.TaxaMista
		p.PeriodoFixoAnos = anos([]int{1, 2, 3, 5}[r.IntN(4)])
	default:
		p.TipoTaxa = dominio.TaxaFixa
		p.PrazoAnos = []int{30, 34}[r.IntN(2)]
	}
	if r.IntN(2) == 0 {
		p.Produtos = []string{bancoctt.ProdutoVendasAssociadas}
	}

	return casoCTT{fmt.Sprintf("aleatório %d (%s, LTV %d %%, %d anos)", i, p.TipoTaxa, ltv, p.PrazoAnos), p}
}

func alvoDeAmostras(t *testing.T) int {
	t.Helper()
	if v := os.Getenv("BANCOCTT_AMOSTRAS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("BANCOCTT_AMOSTRAS=%q não é um número de amostras", v)
		}
		return n
	}
	return 250
}

func primeiros(b []byte) string {
	const n = 160
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// --- o gravador ------------------------------------------------------------------

// gravadorCTT é um transporte que deixa passar e guarda o último corpo lido.
//
// ⚠️ Guarda uma CÓPIA e repõe o corpo, porque ler um corpo esvazia-o: sem isso o
// banco recebia uma resposta vazia e a fidelidade media outra coisa.
type gravadorCTT struct {
	real transporte.HTTPSimples

	mu      sync.Mutex
	corpo   []byte
	nPedido int64
	nBytes  int64
}

func (g *gravadorCTT) Fazer(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := g.real.Fazer(ctx, req)
	if err != nil {
		return nil, err
	}
	lido, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}

	g.mu.Lock()
	g.corpo = lido
	g.nPedido++
	g.nBytes += int64(len(lido))
	g.mu.Unlock()

	resp.Body = io.NopCloser(strings.NewReader(string(lido)))
	return resp, nil
}

func (g *gravadorCTT) reiniciar() {
	g.mu.Lock()
	g.corpo = nil
	g.mu.Unlock()
}

func (g *gravadorCTT) ultimo() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.corpo
}

func (g *gravadorCTT) pedidos() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nPedido
	g.nPedido = 0
	return n
}

func (g *gravadorCTT) bytes() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nBytes
	g.nBytes = 0
	return n
}
