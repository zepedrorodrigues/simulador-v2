//go:build rede

package montepio_test

import (
	"bytes"
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

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/montepio"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Fidelidade: a observação que guardamos é o que o banco respondeu?
//
// É a mesma pergunta estreita que se fez à CGD e ao Novo Banco, e a mesma
// resposta: **entre o corpo que chegou pela rede e o dominio.Oferta que sai do
// Simular, perdeu-se ou torceu-se alguma coisa?** Não é «o número está certo» —
// isso depende do preçário do Montepio.
//
//	go test -tags rede -timeout 180m -run TestFidelidade ./internal/bancos/montepio/ -v
//
// O tamanho da amostra vem de MONTEPIO_AMOSTRAS (por omissão 250) e a
// concorrência de MONTEPIO_TRABALHADORES (por omissão 2).
//
// ⚠️ **O que dá valor a cada amostra não é ser mais uma: é o espelho.** Cada
// resposta é lida uma segunda vez por um caminho escrito de propósito para não
// se parecer com o nosso — `map[string]any` e `float64` em vez de struct com
// tags e `json.Number`. Se os dois concordarem, ou ambos estão certos ou erraram
// da mesma maneira; e errar da mesma maneira por dois desenhos diferentes é
// muito menos provável do que errar uma vez.
//
// ⚠️ Este banco é **o mais caro dos três a sondar**, e não por acaso: são dois
// pedidos por simulação — o arranque que fixa os cookies e o cálculo. O arranque
// não se partilha entre simulações de propósito (ver ClienteComSessao).
func TestFidelidadeDoQueGuardamosFaceAoQueOBancoRespondeu(t *testing.T) {
	alvo := alvoDeAmostrasMP(t)
	trabalhadores := trabalhadoresMP(t)
	t.Logf("alvo: %d ofertas conferidas, com %d trabalhadores", alvo, trabalhadores)

	var (
		conferidas   atomic.Int64
		campos       atomic.Int64
		recusadas    atomic.Int64
		semRede      atomic.Int64
		falhas       atomic.Int64
		bytesLidos   atomic.Int64
		pedidosHTTP  atomic.Int64
		arranquesFtn atomic.Int64
	)

	inicio := time.Now()
	entrada := make(chan casoMP)
	var wg sync.WaitGroup

	for range trabalhadores {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// ⚠️ Um cliente com sessão por trabalhador, e não um por simulação: é o
			// que o varrimento fará, e é o caminho que interessa vigiar. O jar é
			// dele e não se cruza com o do outro trabalhador.
			real, err := transporte.NovoClienteComSessao(nil)
			if err != nil {
				t.Errorf("montar o cliente com sessão: %v", err)
				return
			}
			grav := &gravadorMP{real: real}
			banco := montepio.Novo(grav)

			for c := range entrada {
				grav.reiniciar()

				ctx, cancelar := context.WithTimeout(context.Background(), 60*time.Second)
				oferta, err := banco.Simular(ctx, c.pedido)
				cancelar()

				pedidosHTTP.Add(grav.pedidos())
				arranquesFtn.Add(grav.arranques())
				bytesLidos.Add(grav.bytes())

				corpo := grav.ultimo()
				if corpo == nil {
					// ⚠️ Não é uma recusa do banco: é o pedido.go a impor prazo,
					// período ou modalidade **antes** de gastar um pedido. Conta-se
					// à parte, porque é comportamento desejado e não amostra de
					// fidelidade da leitura.
					semRede.Add(1)
					continue
				}
				if err != nil {
					// O banco recusou. Confirma-se que a recusa é mesmo dele, e não
					// uma leitura falhada de uma resposta boa.
					if !recusaFielMP(corpo, err) {
						falhas.Add(1)
						t.Errorf("%s: o erro não corresponde ao corpo %q: %v", c.nome, primeirosMP(corpo), err)
					}
					recusadas.Add(1)
					continue
				}

				n, err := conferirContraOEspelhoMP(c.nome, corpo, oferta, c.pedido)
				conferidas.Add(1)
				campos.Add(int64(n))
				if err != nil {
					falhas.Add(1)
					t.Errorf("%s: %v", c.nome, err)
				}
			}
		}()
	}

	// ⚠️ Alimenta-se até haver **ofertas conferidas** que cheguem, e não até haver
	// pedidos feitos: um caso recusado pelo banco, ou travado por nós antes da
	// rede, não é amostra de fidelidade da leitura.
	gerados := 0
	for _, c := range formasDeRespostaMP() {
		entrada <- c
		gerados++
	}
	r := rand.New(rand.NewPCG(20260727, 11))
	for conferidas.Load() < int64(alvo) && gerados < alvo*3 {
		entrada <- casoAleatorioMP(r, gerados)
		gerados++
	}
	close(entrada)
	wg.Wait()

	demorou := time.Since(inicio)
	n, f := conferidas.Load(), falhas.Load()
	t.Logf("%d pedidos gerados para %d ofertas conferidas", gerados, n)
	t.Logf("%d conferidas, %d campos comparados, %d recusas do banco, %d travados antes da rede, %d falhas, em %s",
		n, campos.Load(), recusadas.Load(), semRede.Load(), f, demorou.Round(time.Second))
	t.Logf("custo para o Montepio: %d arranques + %d cálculos, %.1f MB, %s por simulação",
		arranquesFtn.Load(), pedidosHTTP.Load(), float64(bytesLidos.Load())/(1<<20),
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
				"Correr com MONTEPIO_AMOSTRAS=3000.", n)
		}
	}
}

// --- o espelho ----------------------------------------------------------------

// conferirContraOEspelhoMP lê o corpo por um caminho independente e compara tudo
// o que a Oferta transporta. Devolve quantos campos comparou.
func conferirContraOEspelhoMP(nome string, corpo []byte, o dominio.Oferta, pedido dominio.Pedido) (int, error) {
	var cru map[string]any
	if err := json.Unmarshal(corpo, &cru); err != nil {
		return 0, fmt.Errorf("o corpo não é JSON: %w", err)
	}
	res, _ := cru["Result"].(map[string]any)
	if res == nil {
		return 0, fmt.Errorf("a resposta não traz Result, mas o Simular devolveu uma oferta")
	}
	fases, _ := res["PeriodInstallment"].([]any)
	if len(fases) == 0 {
		return 0, fmt.Errorf("a resposta não traz PeriodInstallment, mas o Simular devolveu uma oferta")
	}
	primeira, _ := fases[0].(map[string]any)

	var problemas []string
	comparados := 0

	verTaxa := func(campo string, bruto any, veio *dominio.Taxa) {
		comparados++
		if err := igualTaxaMP(bruto, veio); err != nil {
			problemas = append(problemas, fmt.Sprintf("%s: %v", campo, err))
		}
	}
	verDinheiro := func(campo string, bruto any, veio *dominio.Dinheiro) {
		comparados++
		if err := igualDinheiroMP(bruto, veio); err != nil {
			problemas = append(problemas, fmt.Sprintf("%s: %v", campo, err))
		}
	}

	// ⚠️ A TAN e a prestação publicadas são as da **primeira** fase, que é a que
	// a pessoa paga primeiro. Numa mista, tirá-las da segunda seria publicar a
	// projecção do banco em vez do preço contratado do período fixo.
	verTaxa("PeriodInstallment[0].TAN", primeira["TAN"], o.TAN)
	verDinheiro("PeriodInstallment[0].Installment", primeira["Installment"], o.Prestacao)
	verTaxa("TAEG", res["TAEG"], o.TAEG)
	verTaxa("Spread", res["Spread"], o.Spread)
	verDinheiro("MTIC", res["MTIC"], o.MTIC)

	// O indexante lê-se da resposta, e o código não é o óbvio: EH2 é a de 12
	// meses. Um mapa a partir da família pedida acertaria por acaso.
	comparados += 2
	if err := conferirIndexanteMP(fases, o); err != nil {
		problemas = append(problemas, err.Error())
	}

	comparados += 2
	if err := conferirFasesMP(res, fases, o); err != nil {
		problemas = append(problemas, err.Error())
	}

	comparados++
	if err := conferirPrazoDeclaradoMP(res, o, pedido); err != nil {
		problemas = append(problemas, err.Error())
	}

	// ⚠️ As duas notas que tornam esta oferta comparável com as dos outros
	// bancos. Prosa também se confere: é o único sítio onde estas duas coisas
	// existem.
	comparados += 2
	if err := conferirNotaDoImpostoMP(res, o); err != nil {
		problemas = append(problemas, err.Error())
	}
	if err := conferirNotaDasContrapartidasMP(res, o, pedido); err != nil {
		problemas = append(problemas, err.Error())
	}

	if len(problemas) > 0 {
		return comparados, fmt.Errorf("%s divergiu do corpo:\n    - %s", nome, strings.Join(problemas, "\n    - "))
	}
	return comparados, nil
}

// conferirIndexanteMP confirma que o indexante e o seu valor saem da fase que o
// banco marcou como indexada — e não do que se pediu.
func conferirIndexanteMP(fases []any, o dominio.Oferta) error {
	esperado := ""
	var valor any
	for _, bruta := range fases {
		f, _ := bruta.(map[string]any)
		taxa, _ := f["Rate"].(map[string]any)
		switch textoBrutoMP(taxa["BaseRateCode"]) {
		case "EH3":
			esperado, valor = "3m", taxa["BaseRateValue"]
		case "EH6":
			esperado, valor = "6m", taxa["BaseRateValue"]
		case "EH2":
			esperado, valor = "12m", taxa["BaseRateValue"]
		}
		if esperado != "" {
			break
		}
	}

	if esperado == "" {
		// Fixa ao prazo todo: só há SWAP, e não há Euribor nenhuma a declarar.
		if o.Indexante != "" {
			return fmt.Errorf("o corpo não traz fase indexada e a oferta declara o indexante %q", o.Indexante)
		}
		if o.EuriborValor != nil {
			return fmt.Errorf("o corpo não traz fase indexada e a oferta publica uma Euribor de %s", o.EuriborValor)
		}
		return nil
	}
	if string(o.Indexante) != esperado {
		return fmt.Errorf("o corpo traz a Euribor a %s e a oferta declara %q", esperado, o.Indexante)
	}
	if err := igualTaxaMP(valor, o.EuriborValor); err != nil {
		return fmt.Errorf("valor da Euribor: %w", err)
	}
	return nil
}

// conferirFasesMP: com **uma** fase há plano e ele cobre o prazo todo; com duas
// não há plano nenhum, e a razão vai por nota.
//
// ⚠️ É a afirmação central deste banco. A segunda fase repete a TAN e a
// prestação da primeira, e publicá-la seria dar por contratada uma taxa que o
// banco não contrata depois do período fixo.
func conferirFasesMP(res map[string]any, fases []any, o dominio.Oferta) error {
	meses := inteiroBrutoMP(res["Term"])

	if len(fases) != 1 {
		if len(o.Fases) != 0 {
			return fmt.Errorf("o corpo traz %d fases (a indexada repete a prestação da fixa) e a oferta publica %d",
				len(fases), len(o.Fases))
		}
		// Sem plano, a cauda tem de estar dita em prosa.
		for _, nota := range o.Notas() {
			if strings.Contains(nota, "Depois deles o Banco Montepio declara") {
				return nil
			}
		}
		return fmt.Errorf("mista sem plano de fases e sem nota que diga o que vem depois do período fixo: %q",
			strings.Join(o.Notas(), " | "))
	}

	if len(o.Fases) != 1 {
		return fmt.Errorf("o corpo traz uma fase a cobrir o contrato e a oferta publica %d", len(o.Fases))
	}
	f := o.Fases[0]
	if f.AteMes != meses {
		return fmt.Errorf("a fase acaba ao mês %d e o Term do corpo é de %d meses", f.AteMes, meses)
	}
	unica, _ := fases[0].(map[string]any)
	if err := igualTaxaMP(unica["TAN"], &f.Taxa); err != nil {
		return fmt.Errorf("taxa da fase: %w", err)
	}
	if err := igualDinheiroMP(unica["Installment"], &f.Prestacao); err != nil {
		return fmt.Errorf("prestação da fase: %w", err)
	}
	return nil
}

// conferirPrazoDeclaradoMP exige que uma diferença entre o prazo pedido e o
// aplicado apareça como **um** ajuste, com os números certos lá dentro.
//
// ⚠️ Um ajuste, não o último de uma cadeia: é o defeito que as corridas da CGD e
// do Novo Banco apanharam, e aqui há dois sítios a encolher o prazo — o escalão
// de idade e o mínimo do banco.
func conferirPrazoDeclaradoMP(res map[string]any, o dominio.Oferta, pedido dominio.Pedido) error {
	aplicado := inteiroBrutoMP(res["Term"]) / 12

	var doPrazo []dominio.Ajuste
	for _, a := range o.Ajustes() {
		if a.Campo() == dominio.AjustadoPrazoAnos {
			doPrazo = append(doPrazo, a)
		}
	}

	if aplicado == pedido.PrazoAnos {
		if len(doPrazo) > 0 {
			return fmt.Errorf("o banco aplicou os %d anos pedidos e há na mesma um ajuste a dizer de %v para %v",
				aplicado, doPrazo[0].De(), doPrazo[0].Para())
		}
		return nil
	}
	if len(doPrazo) > 1 {
		return fmt.Errorf("%d ajustes ao prazo em cadeia — o segundo diz «Pediu %v anos» a quem pediu %d",
			len(doPrazo), doPrazo[1].De(), pedido.PrazoAnos)
	}
	if len(doPrazo) == 0 {
		return fmt.Errorf("pediram-se %d anos, o banco aplicou %d, e não há ajuste nenhum a declará-lo",
			pedido.PrazoAnos, aplicado)
	}
	a := doPrazo[0]
	if a.Para() != aplicado {
		return fmt.Errorf("o banco aplicou %d anos e o ajuste diz %v", aplicado, a.Para())
	}
	if a.De() != pedido.PrazoAnos {
		return fmt.Errorf("pediram-se %d anos e o ajuste diz que se pediram %v", pedido.PrazoAnos, a.De())
	}
	if a.Nota() == "" {
		return fmt.Errorf("o prazo mudou de %d para %d sem uma nota que o diga", pedido.PrazoAnos, aplicado)
	}
	return nil
}

// conferirNotaDoImpostoMP: quando o banco mete o Imposto do Selo dentro da
// prestação, a oferta tem de o dizer — e quando não mete, não pode dizê-lo.
//
// ⚠️ Sem esta nota, a prestação do arrendamento aparece ao lado das dos outros
// bancos 1,9 % mais cara e com a mesma TAN, sem razão visível.
func conferirNotaDoImpostoMP(res map[string]any, o dominio.Oferta) error {
	dentro := boolBrutoMP(res["HasIS"]) && !boolBrutoMP(res["IsISInstallmentOut"])

	diz := false
	for _, nota := range o.Notas() {
		if strings.Contains(nota, "Imposto do Selo sobre os juros") {
			diz = true
			break
		}
	}
	switch {
	case dentro && !diz:
		return fmt.Errorf("o corpo diz HasIS=true e IsISInstallmentOut=false, e nenhuma nota avisa que o imposto vai dentro da prestação: %q",
			strings.Join(o.Notas(), " | "))
	case !dentro && diz:
		return fmt.Errorf("o imposto fica fora da prestação (HasIS=%v, IsISInstallmentOut=%v) e a oferta diz que vai dentro",
			boolBrutoMP(res["HasIS"]), boolBrutoMP(res["IsISInstallmentOut"]))
	}
	return nil
}

// conferirNotaDasContrapartidasMP confirma que o desconto anunciado é a
// diferença entre os dois spreads que vieram no corpo, e que quem não as
// escolheu é avisado de que o preço é o base.
func conferirNotaDasContrapartidasMP(res map[string]any, o dominio.Oferta, pedido dominio.Pedido) error {
	escolhidas := pedido.TemProduto(montepio.ProdutoContrapartidas)

	if !escolhidas {
		if len(o.ProdutosAplicados) != 0 {
			return fmt.Errorf("não se escolheram contrapartidas e a oferta aplica %v", o.ProdutosAplicados)
		}
		for _, nota := range o.Notas() {
			if strings.Contains(nota, "não inclui as contrapartidas") {
				return nil
			}
		}
		return fmt.Errorf("sem contrapartidas escolhidas e sem nota que o diga: %q", strings.Join(o.Notas(), " | "))
	}

	if len(o.ProdutosAplicados) != 1 || o.ProdutosAplicados[0] != montepio.ProdutoContrapartidas {
		return fmt.Errorf("escolheram-se contrapartidas e a oferta aplica %v", o.ProdutosAplicados)
	}

	com, okCom := numeroDoEspelhoMP(res["Spread"])
	sem, okSem := numeroDoEspelhoMP(res["SpreadBase"])
	if !okCom || !okSem {
		return nil
	}
	diferenca := sem.Sub(com)
	if diferenca.IsZero() {
		return nil
	}
	querido := diferenca.String()
	for _, nota := range o.Notas() {
		if strings.Contains(nota, "descontam "+querido+" p.p.") {
			return nil
		}
	}
	return fmt.Errorf("o corpo dá um desconto de contrapartidas de %s p.p. e nenhuma nota o diz: %q",
		querido, strings.Join(o.Notas(), " | "))
}

// recusaFielMP confirma que um erro nosso corresponde mesmo a uma recusa do
// banco.
//
// ⚠️ Aqui a recusa não traz código nenhum — o `Code` vem nulo nos quatro modos
// medidos —, por isso o que se exige é o `Status` não ser `Ok` ou não haver
// `Result`. Cobrar mais do que isto era exigir do banco o que ele não dá.
func recusaFielMP(corpo []byte, err error) bool {
	var cru map[string]any
	if json.Unmarshal(corpo, &cru) != nil {
		return true // corpo ilegível: o erro é legítimo
	}
	if !strings.EqualFold(textoBrutoMP(cru["Status"]), "Ok") {
		return err != nil
	}
	res, _ := cru["Result"].(map[string]any)
	if res == nil {
		return err != nil
	}
	// A resposta era boa — e nós devolvemos erro na mesma.
	return false
}

// --- conversão independente -----------------------------------------------------
//
// ⚠️ Escrita à mão de propósito e por outro caminho: aqui os números chegam como
// float64 do `encoding/json` genérico, e não como json.Number lido para decimal.
// Se o nosso caminho ganhar um defeito de conversão, este não o acompanha.

func textoBrutoMP(v any) string {
	s, _ := v.(string)
	return s
}

func inteiroBrutoMP(v any) int {
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return int(f)
}

func boolBrutoMP(v any) bool {
	b, _ := v.(bool)
	return b
}

func numeroDoEspelhoMP(v any) (decimal.Decimal, bool) {
	f, ok := v.(float64)
	if !ok {
		return decimal.Zero, false
	}
	return decimal.NewFromFloat(f), true
}

func igualTaxaMP(bruto any, veio *dominio.Taxa) error {
	q, existe := numeroDoEspelhoMP(bruto)
	if !existe {
		if veio != nil {
			return fmt.Errorf("o corpo não traz e a oferta diz %s", veio)
		}
		return nil
	}
	if veio == nil {
		return fmt.Errorf("o corpo traz %s e a oferta não traz nada", q)
	}
	if !veio.Decimal().Equal(q) {
		return fmt.Errorf("o corpo diz %s e a oferta diz %s", q, veio)
	}
	return nil
}

func igualDinheiroMP(bruto any, veio *dominio.Dinheiro) error {
	q, existe := numeroDoEspelhoMP(bruto)
	if !existe {
		if veio != nil {
			return fmt.Errorf("o corpo não traz e a oferta diz %s", veio)
		}
		return nil
	}
	if veio == nil {
		return fmt.Errorf("o corpo traz %s e a oferta não traz nada", q)
	}
	if !veio.Decimal().Equal(q) {
		return fmt.Errorf("o corpo diz %s e a oferta diz %s", q, veio)
	}
	return nil
}

func primeirosMP(b []byte) string {
	if len(b) > 160 {
		return string(b[:160]) + "…"
	}
	return string(b)
}

// --- o gravador -------------------------------------------------------------------

// gravadorMP passa tudo ao cliente real e fica com uma cópia do último corpo do
// **cálculo**. É como se obtém, do mesmo pedido, a oferta e o corpo que a
// originou.
//
// ⚠️ O arranque não passa por aqui: é o `Arrancar`, e o que ele devolve é HTML,
// não a resposta que a oferta lê. Conta-se à parte, para o custo declarado ser o
// custo verdadeiro.
type gravadorMP struct {
	real transporte.HTTPComSessao

	mu        sync.Mutex
	corpo     []byte
	nPedido   int64
	nArranque int64
	nBytes    int64
}

func (g *gravadorMP) reiniciar() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.corpo = nil
}

func (g *gravadorMP) Fazer(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := g.real.Fazer(ctx, req)
	if err != nil {
		return nil, err
	}
	corpo, lerErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if lerErr != nil {
		return nil, lerErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(corpo))

	g.mu.Lock()
	g.nPedido++
	g.nBytes += int64(len(corpo))
	g.corpo = corpo
	g.mu.Unlock()
	return resp, nil
}

func (g *gravadorMP) Arrancar(ctx context.Context, url string) (transporte.Sessao, error) {
	s, err := g.real.Arrancar(ctx, url)
	g.mu.Lock()
	g.nArranque++
	g.nBytes += int64(len(s.HTML))
	g.mu.Unlock()
	return s, err
}

func (g *gravadorMP) ultimo() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.corpo
}

func (g *gravadorMP) pedidos() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nPedido
	g.nPedido = 0
	return n
}

func (g *gravadorMP) arranques() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nArranque
	g.nArranque = 0
	return n
}

func (g *gravadorMP) bytes() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nBytes
	g.nBytes = 0
	return n
}

// --- a amostra ---------------------------------------------------------------------

type casoMP struct {
	nome   string
	pedido dominio.Pedido
}

func alvoDeAmostrasMP(t *testing.T) int {
	t.Helper()
	return inteiroDoAmbienteMP(t, "MONTEPIO_AMOSTRAS", 250)
}

func trabalhadoresMP(t *testing.T) int {
	t.Helper()
	return inteiroDoAmbienteMP(t, "MONTEPIO_TRABALHADORES", 2)
}

func inteiroDoAmbienteMP(t *testing.T, chave string, omissao int) int {
	t.Helper()
	v := os.Getenv(chave)
	if v == "" {
		return omissao
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		t.Fatalf("%s inválido: %q", chave, v)
	}
	return n
}

// formasDeRespostaMP é a varredura exaustiva do que é discreto.
//
// ⚠️ A distinção é deliberada, como nos outros dois. O que muda a **forma** da
// resposta é discreto — tipo de taxa, período, finalidade, tenor da Euribor,
// contrapartidas — e varre-se todo. O que muda só os **números** é contínuo —
// montante, imóvel, prazo, idade — e amostra-se.
func formasDeRespostaMP() []casoMP {
	var casos []casoMP
	acrescentar := func(nome string, p dominio.Pedido) {
		casos = append(casos, casoMP{nome: nome, pedido: p})
	}

	base := func(prazo int, taxa dominio.TipoTaxa, periodo *int, f dominio.Finalidade, idx dominio.Indexante) dominio.Pedido {
		return dominio.Pedido{
			ValorImovel:     dominio.DinheiroDeInteiro(250_000),
			Montante:        dominio.DinheiroDeInteiro(200_000),
			PrazoAnos:       prazo,
			TipoTaxa:        taxa,
			PeriodoFixoAnos: periodo,
			Indexante:       idx,
			Finalidade:      f,
			Localizacao:     dominio.LocalizacaoContinente,
			// 30 anos: o escalão que permite os 40 de prazo, para nenhuma forma
			// morrer no limite de idade antes de exercer a sua.
			Titulares: []dominio.Titular{{DataNascimento: nascidoHaAnosMP(30)}},
		}
	}

	finalidades := []dominio.Finalidade{
		dominio.FinalidadePropria, dominio.FinalidadeSecundaria, dominio.FinalidadeArrendamento,
	}
	tenores := []dominio.Indexante{dominio.Euribor3M, dominio.Euribor6M, dominio.Euribor12M}
	periodos := []int{2, 5, 7, 10, 15, 25, 30}

	for _, f := range finalidades {
		// Variável: os três tenores, e é onde o EH2 se exerce.
		for _, idx := range tenores {
			acrescentar(fmt.Sprintf("variavel/%s/%s", idx, f), base(30, dominio.TaxaVariavel, nil, f, idx))
		}
		// Mista: os períodos, todos abaixo de um prazo de 40.
		for _, periodo := range periodos {
			p := periodo
			acrescentar(fmt.Sprintf("mista/%da/%s", periodo, f), base(40, dominio.TaxaMista, &p, f, ""))
		}
		// Fixa: nos períodos praticados sai uma fase só; fora deles vira mista.
		for _, anos := range append([]int{}, append(periodos, 20, 35)...) {
			acrescentar(fmt.Sprintf("fixa/%da/%s", anos, f), base(anos, dominio.TaxaFixa, nil, f, ""))
		}
		// Com contrapartidas: a outra coluna de preço, e a nota que a explica.
		p := base(30, dominio.TaxaVariavel, nil, f, dominio.Euribor3M)
		p.Produtos = []string{montepio.ProdutoContrapartidas}
		acrescentar(fmt.Sprintf("variavel/contrapartidas/%s", f), p)

		// Dois titulares: manda o mais velho, e o preço não muda com o número.
		d := base(30, dominio.TaxaVariavel, nil, f, dominio.Euribor3M)
		d.Titulares = []dominio.Titular{
			{DataNascimento: nascidoHaAnosMP(28)},
			{DataNascimento: nascidoHaAnosMP(34)},
		}
		acrescentar(fmt.Sprintf("variavel/dois-titulares/%s", f), d)
	}
	return casos
}

// casoAleatorioMP sorteia um ponto sobre os contínuos.
//
// ⚠️ Os prazos e as idades vão de propósito até onde os limites mordem — o
// escalão por idade e o fim aos 76 —, porque o caminho do ajuste ao prazo é o
// mais frágil deste banco: é o único que este banco impõe **antes** da rede, sem
// o banco lho dizer.
func casoAleatorioMP(r *rand.Rand, i int) casoMP {
	finalidades := []dominio.Finalidade{
		dominio.FinalidadePropria, dominio.FinalidadeSecundaria, dominio.FinalidadeArrendamento,
	}
	f := finalidades[r.IntN(len(finalidades))]

	ltv := 15 + r.IntN(86) // 15 % a 100 %: o banco preça tudo isto
	montante := int64(10_000 + r.IntN(890_000))
	valorImovel := (montante*100 + int64(ltv) - 1) / int64(ltv)

	prazo := 5 + r.IntN(36) // 5 a 40: a gama que o banco pratica
	idade := 25 + r.IntN(41)

	var taxa dominio.TipoTaxa
	switch sorte := r.IntN(10); {
	case sorte < 5:
		taxa = dominio.TaxaVariavel
	case sorte < 8:
		taxa = dominio.TaxaMista
	default:
		taxa = dominio.TaxaFixa
	}

	var periodo *int
	var idx dominio.Indexante
	switch taxa {
	case dominio.TaxaMista:
		validos := []int{2, 5, 7, 10, 15, 25, 30}
		p := validos[r.IntN(len(validos))]
		periodo = &p
	case dominio.TaxaVariavel:
		tenores := []dominio.Indexante{dominio.Euribor3M, dominio.Euribor6M, dominio.Euribor12M, ""}
		idx = tenores[r.IntN(len(tenores))]
	}

	var produtos []string
	if r.IntN(4) == 0 {
		produtos = []string{montepio.ProdutoContrapartidas}
	}

	return casoMP{
		nome: fmt.Sprintf("aleatorio/%d/%s/ltv%d/%da/%dan/%s", i, taxa, ltv, prazo, idade, f),
		pedido: dominio.Pedido{
			ValorImovel:     dominio.DinheiroDeInteiro(valorImovel),
			Montante:        dominio.DinheiroDeInteiro(montante),
			PrazoAnos:       prazo,
			TipoTaxa:        taxa,
			PeriodoFixoAnos: periodo,
			Indexante:       idx,
			Finalidade:      f,
			Localizacao:     dominio.LocalizacaoContinente,
			Produtos:        produtos,
			Titulares:       []dominio.Titular{{DataNascimento: nascidoHaAnosMP(idade)}},
		},
	}
}

// nascidoHaAnosMP dá uma data de nascimento que faz o titular ter exactamente
// esta idade hoje — sem congelar relógio nenhum.
func nascidoHaAnosMP(n int) dominio.Data {
	return dominio.DataDeInstante(time.Now().AddDate(-n, 0, 0))
}
