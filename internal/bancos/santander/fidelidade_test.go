//go:build rede

package santander_test

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

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/santander"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Fidelidade: a observação que guardamos é o que o banco respondeu?
//
// É a mesma pergunta estreita dos outros quatro bancos: **entre o corpo que
// chegou pela rede e o dominio.Oferta que sai do Simular, perdeu-se ou torceu-se
// alguma coisa?** Não é «o número está certo» — isso depende do preçário.
//
//	go test -tags rede -timeout 120m -run TestFidelidade ./internal/bancos/santander/ -v
//
// O tamanho da amostra vem de SANTANDER_AMOSTRAS (por omissão 250).
//
// ⚠️ **O que dá valor a cada amostra é o espelho**: cada resposta é lida uma
// segunda vez por um caminho escrito de propósito para não se parecer com o
// nosso — `map[string]any` e `float64` em vez de structs e decimal.
//
// ⚠️ **E este banco custa QUATRO pedidos por simulação**, não um: configuração,
// limites, catálogo e a simulação. É o mais caro de sondar dos cinco, e o
// contador de pedidos no fim di-lo.

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
	entrada := make(chan casoSantander)
	var wg sync.WaitGroup

	for range trabalhadores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			grav := &gravadorSantander{real: transporte.NovoCliente(nil)}
			banco := santander.Novo(grav)

			for c := range entrada {
				grav.reiniciar()

				ctx, cancelar := context.WithTimeout(context.Background(), 60*time.Second)
				oferta, err := banco.Simular(ctx, c.pedido)
				cancelar()

				pedidos.Add(grav.pedidos())
				bytesLidos.Add(grav.bytes())

				corpo := grav.ultimaSimulacao()
				if corpo == nil {
					recusadas.Add(1)
					continue
				}
				if err != nil {
					if !recusaFielSantander(corpo, err) {
						falhas.Add(1)
						t.Errorf("%s: o erro não corresponde ao corpo %q: %v", c.nome, primeiros(corpo), err)
					}
					recusadas.Add(1)
					continue
				}

				n, err := conferirContraOEspelho(corpo, oferta, c.pedido)
				conferidas.Add(1)
				campos.Add(int64(n))
				if err != nil {
					falhas.Add(1)
					t.Errorf("%s: %v", c.nome, err)
				}
			}
		}()
	}

	gerados := 0
	for _, c := range formasDeResposta(t) {
		entrada <- c
		gerados++
	}
	r := rand.New(rand.NewPCG(20260728, 18))
	for conferidas.Load() < int64(alvo) && gerados < alvo*3 {
		entrada <- casoAleatorio(t, r, gerados)
		gerados++
	}
	close(entrada)
	wg.Wait()

	demorou := time.Since(inicio)
	n, f := conferidas.Load(), falhas.Load()
	t.Logf("%d simulações geradas para %d ofertas conferidas", gerados, n)
	t.Logf("%d ofertas conferidas, %d campos comparados, %d recusas, %d falhas, em %s",
		n, campos.Load(), recusadas.Load(), f, demorou.Round(time.Second))
	t.Logf("custo para o Santander: %d pedidos HTTP, %.1f MB, %s por simulação",
		pedidos.Load(), float64(bytesLidos.Load())/(1<<20),
		(demorou / time.Duration(max(gerados, 1))).Round(time.Millisecond))

	// ⚠️ Com zero falhas em n amostras, o limite superior do erro a 95 % de
	// confiança é 3/n — regra dos três. Não é «acertámos 100 %».
	if f == 0 && n > 0 {
		t.Logf("MEDIDO: 0 falhas em %d ofertas → erro de leitura ≤ %.3f %% (95 %% de confiança)",
			n, 300.0/float64(n))
		if n < 3000 {
			t.Logf("⚠️ para sustentar «≤ 0,1 %%» são precisas 3000 ofertas; esta corrida tem %d. "+
				"Correr com SANTANDER_AMOSTRAS=3000.", n)
		}
	}
}

// --- o espelho ------------------------------------------------------------------

// conferirContraOEspelho lê o corpo por um caminho independente e compara.
func conferirContraOEspelho(corpo []byte, o dominio.Oferta, p dominio.Pedido) (int, error) {
	var cru []map[string]any
	if err := json.Unmarshal(corpo, &cru); err != nil {
		return 0, fmt.Errorf("o corpo não é JSON: %w", err)
	}

	var valida map[string]any
	for _, e := range cru {
		if b, _ := e["isValid"].(bool); b {
			valida = e
			break
		}
	}
	if valida == nil {
		return 0, fmt.Errorf("nenhuma entrada válida no corpo, e o Simular devolveu uma oferta")
	}

	// A coluna que o pedido escolheu — a mesma decisão do parser, feita aqui
	// pelo nome do campo em vez de por um método.
	chave := "nonBonifiedPlanDetails"
	if len(p.ProdutosDoBanco("santander")) > 0 {
		chave = "bonifiedPlanDetails"
	}
	plano, _ := valida[chave].(map[string]any)
	if plano == nil {
		return 0, fmt.Errorf("a entrada válida não trouxe o plano %q", chave)
	}

	var problemas []string
	comparados := 0

	ver := func(campo string, bruto any, veio *float64) {
		comparados++
		esperado, ok := bruto.(float64)
		if !ok {
			problemas = append(problemas, fmt.Sprintf("%s: o corpo traz %T", campo, bruto))
			return
		}
		if veio == nil {
			problemas = append(problemas, fmt.Sprintf("%s: o corpo traz %v e a oferta não traz nada", campo, esperado))
			return
		}
		if !quaseIgual(esperado, *veio) {
			problemas = append(problemas, fmt.Sprintf("%s: o corpo traz %v e a oferta %v", campo, esperado, *veio))
		}
	}

	ver("taeg", plano["taeg"], comoFloat(o.TAEG))
	ver("mtic", plano["mtic"], comoFloatDinheiro(o.MTIC))

	trocos, _ := plano["installments"].([]any)
	if len(trocos) == 0 {
		problemas = append(problemas, "o plano veio sem installments")
	} else {
		primeiro, _ := trocos[0].(map[string]any)
		ver("installments[0].tan", primeiro["tan"], comoFloat(o.TAN))
		ver("installments[0].monthlyPayment", primeiro["monthlyPayment"], comoFloatDinheiro(o.Prestacao))

		// ⚠️ A afirmação que mais interessa deste banco: o spread publicado é o
		// da ÚLTIMA fase indexada, e não o promocional da primeira.
		comparados++
		if err := spreadEODaUltimaIndexada(trocos, o); err != nil {
			problemas = append(problemas, err.Error())
		}

		// E as fases: tantas quantos os troços, a acabar no prazo do banco.
		comparados++
		if len(o.Fases) != len(trocos) {
			problemas = append(problemas, fmt.Sprintf(
				"o banco descreveu %d troço(s) e a oferta tem %d fase(s)", len(trocos), len(o.Fases)))
		} else if anos, ok := valida["loanDurationYears"].(float64); ok {
			if ultima := o.Fases[len(o.Fases)-1].AteMes; float64(ultima) != anos*12 {
				problemas = append(problemas, fmt.Sprintf(
					"o plano acaba ao mês %d e o banco devolveu %.0f anos", ultima, anos))
			}
		}
	}

	if len(problemas) > 0 {
		return comparados, fmt.Errorf("%d divergência(s): %s", len(problemas), strings.Join(problemas, "; "))
	}
	return comparados, nil
}

// spreadEODaUltimaIndexada confirma a regra que este banco obrigou a inventar.
func spreadEODaUltimaIndexada(trocos []any, o dominio.Oferta) error {
	ultimo := map[string]any(nil)
	for _, bruto := range trocos {
		t, _ := bruto.(map[string]any)
		if codigo, _ := t["referenceRateCode"].(string); codigo == "6EM" {
			ultimo = t
		}
	}
	if ultimo == nil {
		// Fixa pura: não há fase indexada, e publicar spread seria inventar.
		if o.Spread != nil || o.EuriborValor != nil {
			return fmt.Errorf("sem troço indexado, a oferta publicou spread %v e Euribor %v",
				o.Spread, o.EuriborValor)
		}
		return nil
	}
	esperado, _ := ultimo["spread"].(float64)
	if o.Spread == nil {
		return fmt.Errorf("há um troço indexado com spread %v e a oferta não traz spread", esperado)
	}
	obtido, _ := o.Spread.Decimal().Float64()
	if !quaseIgual(esperado, obtido) {
		return fmt.Errorf("o spread da última fase indexada é %v e a oferta traz %v — o promocional é o da primeira",
			esperado, obtido)
	}
	return nil
}

func quaseIgual(a, b float64) bool {
	d := a - b
	return d < 0.0005 && d > -0.0005
}

func comoFloat(t *dominio.Taxa) *float64 {
	if t == nil {
		return nil
	}
	v, _ := t.Decimal().Float64()
	return &v
}

func comoFloatDinheiro(d *dominio.Dinheiro) *float64 {
	if d == nil {
		return nil
	}
	v, _ := d.Decimal().Float64()
	return &v
}

// recusaFielSantander confirma que uma recusa nossa corresponde ao corpo.
func recusaFielSantander(corpo []byte, err error) bool {
	var cru []map[string]any
	if e := json.Unmarshal(corpo, &cru); e != nil {
		return true
	}
	for _, e := range cru {
		if b, _ := e["isValid"].(bool); b {
			// O banco simulou e nós recusámos: só é fiel se o erro for de
			// leitura, e aí ele nomeia o que não se conseguiu ler.
			return strings.Contains(err.Error(), "Não se conseguiu ler")
		}
	}
	return true
}

// --- os casos --------------------------------------------------------------------

type casoSantander struct {
	nome   string
	pedido dominio.Pedido
}

// formasDeResposta são as formas que a lógica distingue, e correm todas antes
// dos aleatórios.
func formasDeResposta(t *testing.T) []casoSantander {
	t.Helper()
	anos := func(n int) *int { return &n }

	base := pedidoBase(t)
	var casos []casoSantander

	comProdutos := base
	comProdutos.Produtos = []string{santander.ProdutoBonificado}
	casos = append(casos,
		casoSantander{"variável sem produtos", base},
		casoSantander{"variável bonificada", comProdutos},
	)

	for _, periodo := range []int{2, 3, 4} {
		p := comProdutos
		p.TipoTaxa = dominio.TaxaMista
		p.PeriodoFixoAnos = anos(periodo)
		casos = append(casos, casoSantander{fmt.Sprintf("mista %da", periodo), p})
	}
	for _, prazo := range []int{10, 20, 30} {
		p := comProdutos
		p.TipoTaxa = dominio.TaxaFixa
		p.PrazoAnos = prazo
		casos = append(casos, casoSantander{fmt.Sprintf("fixa %da", prazo), p})
	}
	return casos
}

func casoAleatorio(t *testing.T, r *rand.Rand, i int) casoSantander {
	t.Helper()
	anos := func(n int) *int { return &n }

	p := pedidoBase(t)
	p.ValorImovel = dominio.DinheiroDeInteiro(int64(100_000 + r.IntN(60)*10_000))
	ltv := 30 + r.IntN(61)
	p.Montante = dominio.DinheiroDeInteiro(p.ValorImovel.Decimal().IntPart() * int64(ltv) / 100)
	p.PrazoAnos = 10 + r.IntN(31)

	switch r.IntN(3) {
	case 0:
		p.TipoTaxa = dominio.TaxaVariavel
	case 1:
		p.TipoTaxa = dominio.TaxaMista
		p.PeriodoFixoAnos = anos([]int{2, 3, 4}[r.IntN(3)])
	default:
		p.TipoTaxa = dominio.TaxaFixa
		p.PrazoAnos = []int{10, 20, 30}[r.IntN(3)]
	}
	if r.IntN(2) == 0 {
		p.Produtos = []string{santander.ProdutoBonificado}
	}
	return casoSantander{fmt.Sprintf("aleatório %d (%s, LTV %d %%, %d anos)", i, p.TipoTaxa, ltv, p.PrazoAnos), p}
}

func alvoDeAmostras(t *testing.T) int {
	t.Helper()
	if v := os.Getenv("SANTANDER_AMOSTRAS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("SANTANDER_AMOSTRAS=%q não é um número de amostras", v)
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

// gravadorSantander guarda o corpo da SIMULAÇÃO, e não o do último pedido.
//
// ⚠️ São quatro pedidos por simulação, e o último a passar é o `/get_by_rates` —
// mas guardar «o último» era ficar refém dessa ordem. Guarda-se pelo caminho.
type gravadorSantander struct {
	real transporte.HTTPSimples

	mu        sync.Mutex
	simulacao []byte
	nPedido   int64
	nBytes    int64
}

func (g *gravadorSantander) Fazer(ctx context.Context, req *http.Request) (*http.Response, error) {
	caminho := req.URL.Path
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
	if strings.HasSuffix(caminho, "/get_by_rates") {
		g.simulacao = lido
	}
	g.nPedido++
	g.nBytes += int64(len(lido))
	g.mu.Unlock()

	resp.Body = io.NopCloser(strings.NewReader(string(lido)))
	return resp, nil
}

func (g *gravadorSantander) reiniciar() {
	g.mu.Lock()
	g.simulacao = nil
	g.mu.Unlock()
}

func (g *gravadorSantander) ultimaSimulacao() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.simulacao
}

func (g *gravadorSantander) pedidos() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nPedido
	g.nPedido = 0
	return n
}

func (g *gravadorSantander) bytes() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nBytes
	g.nBytes = 0
	return n
}
