//go:build fidelidade

package novobanco_test

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

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/novobanco"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Fidelidade: a observação que guardamos é o que o banco respondeu?
//
// É a mesma pergunta estreita que se fez à CGD, e a mesma resposta: **entre o
// corpo que chegou pela rede e o dominio.Oferta que sai do Simular, perdeu-se
// ou torceu-se alguma coisa?** Não é «o número está certo» — isso depende do
// preçário do Novo Banco.
//
//	go test -tags fidelidade -timeout 60m -run TestFidelidade ./internal/bancos/novobanco/ -v
//
// O tamanho da amostra vem de NOVOBANCO_AMOSTRAS (por omissão 250).
//
// ⚠️ **O que dá valor a cada amostra não é ser mais uma: é o espelho.** Cada
// resposta é lida uma segunda vez por um caminho escrito de propósito para não
// se parecer com o nosso — `map[string]any` e `float64` em vez de struct com
// tags e `json.Number`. Se os dois concordarem, ou ambos estão certos ou
// erraram da mesma maneira; e errar da mesma maneira por dois desenhos
// diferentes é muito menos provável do que errar uma vez.
//
// ⚠️ Este banco é **mais barato de sondar do que a CGD**: um pedido por
// simulação, sem os 77 KB da página que a CGD puxa em cada fase fixa.

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

	// Dois trabalhadores, como no varrimento. Cada um com o seu gravador:
	// partilhar um seria comparar a oferta de um pedido com o corpo de outro.
	const trabalhadores = 2

	inicio := time.Now()
	entrada := make(chan casoNB)
	var wg sync.WaitGroup

	for range trabalhadores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			grav := &gravadorNB{real: transporte.NovoCliente(nil)}
			banco := novobanco.Novo(grav)

			for c := range entrada {
				grav.reiniciar()

				ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
				oferta, err := banco.Simular(ctx, c.pedido)
				cancelar()

				pedidos.Add(grav.pedidos())
				bytesLidos.Add(grav.bytes())

				corpo := grav.ultimo()
				if corpo == nil {
					recusadas.Add(1)
					continue
				}
				if err != nil {
					// O banco recusou. Confirma-se que a recusa é mesmo dele, e
					// não uma leitura falhada de uma resposta boa.
					if !recusaFielNB(corpo, err) {
						falhas.Add(1)
						t.Errorf("%s: o erro não corresponde ao corpo %q: %v", c.nome, primeiros(corpo), err)
					}
					recusadas.Add(1)
					continue
				}

				n, err := conferirContraOEspelhoNB(c.nome, corpo, oferta, c.pedido)
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
	for _, c := range formasDeRespostaNB() {
		entrada <- c
		gerados++
	}
	r := rand.New(rand.NewPCG(20260726, 10))
	for conferidas.Load() < int64(alvo) && gerados < alvo*3 {
		entrada <- casoAleatorioNB(r, gerados)
		gerados++
	}
	close(entrada)
	wg.Wait()

	demorou := time.Since(inicio)
	n, f := conferidas.Load(), falhas.Load()
	t.Logf("%d pedidos gerados para %d ofertas conferidas", gerados, n)
	t.Logf("%d ofertas conferidas, %d campos comparados, %d recusas, %d falhas, em %s",
		n, campos.Load(), recusadas.Load(), f, demorou.Round(time.Second))
	t.Logf("custo para o Novo Banco: %d pedidos HTTP, %.1f MB, %s por simulação",
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
				"Correr com NOVOBANCO_AMOSTRAS=3000.", n)
		}
	}
}

// --- o espelho ----------------------------------------------------------------

// conferirContraOEspelhoNB lê o corpo por um caminho independente e compara
// tudo o que a Oferta transporta. Devolve quantos campos comparou.
func conferirContraOEspelhoNB(nome string, corpo []byte, o dominio.Oferta, pedido dominio.Pedido) (int, error) {
	var cru map[string]any
	if err := json.Unmarshal(corpo, &cru); err != nil {
		return 0, fmt.Errorf("o corpo não é JSON: %w", err)
	}
	dados, _ := cru["data"].(map[string]any)
	res, _ := dados["resultado"].(map[string]any)
	if res == nil {
		return 0, fmt.Errorf("a resposta não traz resultado, mas o Simular devolveu uma oferta")
	}
	taxas, _ := res["taxas"].(map[string]any)
	prestacao, _ := res["prestacao"].(map[string]any)

	var problemas []string
	comparados := 0

	verTaxa := func(campo string, bruto any, veio *dominio.Taxa) {
		comparados++
		if err := igualTaxaNB(bruto, veio); err != nil {
			problemas = append(problemas, fmt.Sprintf("%s: %v", campo, err))
		}
	}
	verDinheiro := func(campo string, bruto any, veio *dominio.Dinheiro) {
		comparados++
		if err := igualDinheiroNB(bruto, veio); err != nil {
			problemas = append(problemas, fmt.Sprintf("%s: %v", campo, err))
		}
	}

	verTaxa("taxas.tan", taxas["tan"], o.TAN)
	verTaxa("taxas.taeg", taxas["taeg"], o.TAEG)
	verTaxa("taxas.spread", taxas["spread"], o.Spread)
	verDinheiro("prestacao.base", prestacao["base"], o.Prestacao)
	verDinheiro("mtic", res["mtic"], o.MTIC)

	// ⚠️ A afirmação que mais interessa deste banco: `taxaIndexada` só é uma
	// Euribor na variável. Publicá-la na mista ou na fixa era inventar um
	// indexante que o banco não declarou.
	comparados += 2
	ehVariavel := textoBruto(res["tipoTaxa"]) == "INDEXADA"
	if ehVariavel {
		if err := igualTaxaNB(taxas["taxaIndexada"], o.EuriborValor); err != nil {
			problemas = append(problemas, fmt.Sprintf("taxas.taxaIndexada como Euribor: %v", err))
		}
		if o.Indexante == "" {
			problemas = append(problemas, "taxa variável sem indexante declarado")
		}
	} else {
		if o.EuriborValor != nil {
			problemas = append(problemas, fmt.Sprintf(
				"tipoTaxa %q e a oferta publica uma Euribor de %s", textoBruto(res["tipoTaxa"]), o.EuriborValor))
		}
		if o.Indexante != "" {
			problemas = append(problemas, fmt.Sprintf(
				"tipoTaxa %q e a oferta declara o indexante %q", textoBruto(res["tipoTaxa"]), o.Indexante))
		}
	}

	// As fases, e a regra que as governa neste banco.
	comparados += 2
	if err := conferirFasesNB(res, taxas, prestacao, o); err != nil {
		problemas = append(problemas, err.Error())
	}

	// ⚠️ O prazo que o banco aplicou tem de estar declarado, e **num só**
	// ajuste. Um plano de 10 anos guardado como se fossem os 30 pedidos é um
	// número certo com o rótulo errado.
	comparados++
	if err := conferirPrazoDeclaradoNB(res, o, pedido); err != nil {
		problemas = append(problemas, err.Error())
	}

	// O desconto das bonificações sai em prosa (KAN-33), e prosa também se
	// confere: é o único sítio onde a segunda coluna de preço existe.
	comparados++
	if err := conferirNotaDasBonificacoesNB(taxas, o); err != nil {
		problemas = append(problemas, err.Error())
	}

	if len(problemas) > 0 {
		return comparados, fmt.Errorf("%s divergiu do corpo:\n    - %s", nome, strings.Join(problemas, "\n    - "))
	}
	return comparados, nil
}

// conferirFasesNB: na variável e na fixa há uma fase que cobre o prazo todo; na
// mista não há fase nenhuma, porque o banco não diz o que se passa depois do
// período fixo.
func conferirFasesNB(res, taxas, prestacao map[string]any, o dominio.Oferta) error {
	prazo := inteiroBruto(res["prazo"])
	if textoBruto(res["tipoTaxa"]) == "MISTA" {
		if len(o.Fases) != 0 {
			return fmt.Errorf("mista com %d fases: o banco não publica o plano de depois do período fixo", len(o.Fases))
		}
		return nil
	}
	if len(o.Fases) != 1 {
		return fmt.Errorf("esperava uma fase a cobrir o prazo todo, vieram %d", len(o.Fases))
	}
	f := o.Fases[0]
	if f.AteMes != prazo*12 {
		return fmt.Errorf("a fase acaba ao mês %d e o prazo do corpo é de %d anos", f.AteMes, prazo)
	}
	if err := igualTaxaNB(taxas["tan"], &f.Taxa); err != nil {
		return fmt.Errorf("taxa da fase: %w", err)
	}
	if err := igualDinheiroNB(prestacao["base"], &f.Prestacao); err != nil {
		return fmt.Errorf("prestação da fase: %w", err)
	}
	return nil
}

// conferirPrazoDeclaradoNB exige que uma diferença entre o prazo pedido e o
// aplicado apareça como **um** ajuste, com os números certos lá dentro.
func conferirPrazoDeclaradoNB(res map[string]any, o dominio.Oferta, pedido dominio.Pedido) error {
	aplicado := inteiroBruto(res["prazo"])

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

	// ⚠️ Exige-se **um** ajuste, e não o último de uma cadeia. Uma cadeia
	// fabrica um «Pediu N anos» que ninguém pediu.
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

// conferirNotaDasBonificacoesNB confirma que o desconto anunciado é a diferença
// entre os dois spreads que vieram no corpo.
func conferirNotaDasBonificacoesNB(taxas map[string]any, o dominio.Oferta) error {
	com, okCom := numeroDoEspelhoNB(taxas["spread"])
	sem, okSem := numeroDoEspelhoNB(taxas["spreadSemBonificacao"])
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
	return fmt.Errorf("o corpo dá um desconto de bonificações de %s p.p. e nenhuma nota o diz: %q",
		querido, strings.Join(o.Notas(), " | "))
}

// recusaFielNB confirma que um erro nosso corresponde mesmo a uma recusa do
// banco.
func recusaFielNB(corpo []byte, err error) bool {
	var cru map[string]any
	if json.Unmarshal(corpo, &cru) != nil {
		return true // corpo ilegível: o erro é legítimo
	}
	estado, _ := cru["status"].(map[string]any)
	if estado == nil {
		// Sem status, a resposta era boa — e nós devolvemos erro na mesma.
		return false
	}
	return textoBruto(estado["code"]) != "" && err != nil
}

// --- conversão independente -----------------------------------------------------
//
// ⚠️ Escrita à mão de propósito e por outro caminho: aqui os números chegam como
// float64 do `encoding/json` genérico, e não como json.Number lido para decimal.
// Se o nosso caminho ganhar um defeito de conversão, este não o acompanha.

func textoBruto(v any) string {
	s, _ := v.(string)
	return s
}

func inteiroBruto(v any) int {
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return int(f)
}

func numeroDoEspelhoNB(v any) (decimal.Decimal, bool) {
	f, ok := v.(float64)
	if !ok {
		return decimal.Zero, false
	}
	return decimal.NewFromFloat(f), true
}

func igualTaxaNB(bruto any, veio *dominio.Taxa) error {
	q, existe := numeroDoEspelhoNB(bruto)
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

func igualDinheiroNB(bruto any, veio *dominio.Dinheiro) error {
	q, existe := numeroDoEspelhoNB(bruto)
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

func primeiros(b []byte) string {
	if len(b) > 160 {
		return string(b[:160]) + "…"
	}
	return string(b)
}

// --- o gravador -------------------------------------------------------------------

// gravadorNB passa tudo ao cliente real e fica com uma cópia do último corpo. É
// como se obtém, do mesmo pedido, a oferta e o corpo que a originou.
//
// ⚠️ Guarda o **último**, e não o primeiro, de propósito: quando o V159 obriga a
// repetir com outro prazo, a oferta que sai é a da segunda resposta.
type gravadorNB struct {
	real transporte.HTTPSimples

	mu      sync.Mutex
	corpo   []byte
	nPedido int64
	nBytes  int64
}

func (g *gravadorNB) reiniciar() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.corpo = nil
}

func (g *gravadorNB) Fazer(ctx context.Context, req *http.Request) (*http.Response, error) {
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

func (g *gravadorNB) ultimo() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.corpo
}

func (g *gravadorNB) pedidos() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nPedido
	g.nPedido = 0
	return n
}

func (g *gravadorNB) bytes() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nBytes
	g.nBytes = 0
	return n
}

// --- a amostra ---------------------------------------------------------------------

type casoNB struct {
	nome   string
	pedido dominio.Pedido
}

func alvoDeAmostras(t *testing.T) int {
	t.Helper()
	alvo := 250
	if v := os.Getenv("NOVOBANCO_AMOSTRAS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("NOVOBANCO_AMOSTRAS inválido: %q", v)
		}
		alvo = n
	}
	return alvo
}

// formasDeRespostaNB é a varredura exaustiva do que é discreto.
//
// ⚠️ A distinção é deliberada, como na CGD. O que muda a **forma** da resposta é
// discreto — tipo de taxa, período, finalidade, tenor da Euribor — e varre-se
// todo. O que muda só os **números** é contínuo — montante, imóvel, prazo,
// idade — e amostra-se.
func formasDeRespostaNB() []casoNB {
	var casos []casoNB
	acrescentar := func(nome string, p dominio.Pedido) {
		casos = append(casos, casoNB{nome: nome, pedido: p})
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
			// morrer no V159 antes de exercer a sua.
			Titulares: []dominio.Titular{{
				DataNascimento:   nascidoHaAnosNB(30),
				RendimentoMensal: dominio.DinheiroDeInteiro(2000),
			}},
		}
	}

	finalidades := []dominio.Finalidade{
		dominio.FinalidadePropria, dominio.FinalidadeSecundaria, dominio.FinalidadeArrendamento,
	}
	tenores := []dominio.Indexante{dominio.Euribor3M, dominio.Euribor6M, dominio.Euribor12M}
	periodos := []int{2, 3, 4, 5, 10, 15, 20, 25, 30}

	for _, f := range finalidades {
		// Variável: os três tenores, que é o que distingue este banco.
		for _, idx := range tenores {
			acrescentar(fmt.Sprintf("variavel/%s/%s", idx, f),
				base(30, dominio.TaxaVariavel, nil, f, idx))
		}
		// Mista: os períodos que cabem num prazo de 40 (todos, menos o de 40).
		for _, periodo := range periodos {
			p := periodo
			acrescentar(fmt.Sprintf("mista/%da/%s", periodo, f),
				base(40, dominio.TaxaMista, &p, f, ""))
		}
		// Fixa: o prazo é o período, e são os nove.
		for _, anos := range periodos {
			acrescentar(fmt.Sprintf("fixa/%da/%s", anos, f),
				base(anos, dominio.TaxaFixa, nil, f, ""))
		}
		// Localização: não muda o preço, mas muda o payload — e o que aqui se
		// vigia é a leitura, que tem de aguentar as três.
		for _, l := range []dominio.Localizacao{dominio.LocalizacaoAcores, dominio.LocalizacaoMadeira} {
			p := base(30, dominio.TaxaVariavel, nil, f, dominio.Euribor12M)
			p.Localizacao = l
			acrescentar(fmt.Sprintf("variavel/%s/%s", l, f), p)
		}
	}
	return casos
}

// casoAleatorioNB sorteia um ponto sobre os contínuos.
//
// ⚠️ Os montantes vão de 5 000 € a 900 000 € de propósito: é a gama onde a
// prestação passa e não passa o tecto do V118, e onde o MTIC ganha e perde
// casas — e o que este teste vigia é precisamente a leitura do número.
//
// ⚠️ As idades vão até aos 60 de propósito: é o que faz o V159 disparar e
// exercer o caminho de reaplicação, que é o mais frágil deste banco.
func casoAleatorioNB(r *rand.Rand, i int) casoNB {
	finalidades := []dominio.Finalidade{
		dominio.FinalidadePropria, dominio.FinalidadeSecundaria, dominio.FinalidadeArrendamento,
	}
	f := finalidades[r.IntN(len(finalidades))]

	ltv := 15 + r.IntN(86) // 15 % a 100 %: o banco preça tudo isto
	montante := int64(5_000 + r.IntN(895_000))
	// Arredonda para cima, para o rácio não sair uns milésimos acima do alvo.
	valorImovel := (montante*100 + int64(ltv) - 1) / int64(ltv)

	prazo := 1 + r.IntN(40)
	idade := 25 + r.IntN(36)

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
		validos := []int{2, 3, 4, 5, 10, 15, 20, 25, 30}
		p := validos[r.IntN(len(validos))]
		periodo = &p
		// A mista precisa de prazo maior do que o período.
		if prazo <= p {
			prazo = p + 1
		}
	case dominio.TaxaFixa:
		if prazo < 2 {
			prazo = 2
		}
	case dominio.TaxaVariavel:
		tenores := []dominio.Indexante{dominio.Euribor3M, dominio.Euribor6M, dominio.Euribor12M, ""}
		idx = tenores[r.IntN(len(tenores))]
	}

	return casoNB{
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
			Titulares: []dominio.Titular{{
				DataNascimento:   nascidoHaAnosNB(idade),
				RendimentoMensal: dominio.DinheiroDeInteiro(int64(1000 + r.IntN(5000))),
			}},
		},
	}
}

// nascidoHaAnosNB dá uma data de nascimento que faz o titular ter exactamente
// esta idade hoje — sem congelar relógio nenhum.
func nascidoHaAnosNB(n int) dominio.Data {
	return dominio.DataDeInstante(time.Now().AddDate(-n, 0, 0))
}
