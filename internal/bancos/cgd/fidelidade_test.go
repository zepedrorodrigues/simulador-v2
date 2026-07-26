//go:build rede

package cgd_test

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

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/cgd"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Fidelidade: a observação que guardamos é o que o banco respondeu?
//
// Esta é a pergunta estreita, e é a única das três que se consegue medir hoje.
// Não é «o número está certo» (isso depende do preçário da CGD) nem «a resposta
// ao cliente vai estar certa» (isso depende da reconstrução pela grelha, que
// ainda não existe). É: **entre o corpo que chegou pela rede e o dominio.Oferta
// que sai do Simular, perdeu-se ou torceu-se alguma coisa?**
//
//	go test -tags rede -timeout 60m -run TestFidelidade ./internal/bancos/cgd/ -v
//
// O tamanho da amostra vem de CGD_AMOSTRAS (por omissão 250, para uma corrida
// distraída não pesar). A corrida que sustenta uma afirmação de 99,9 % precisa
// de alguns milhares — ver o comentário do resumo, no fim do teste.
//
// ⚠️ **O que dá valor a cada amostra não é ser mais uma: é o espelho.** Cada
// resposta é lida uma segunda vez, por um caminho escrito de propósito para não
// se parecer com o nosso — `map[string]any` em vez de struct com tags, e uma
// conversão de número feita à mão em vez do numeroPT. Se os dois caminhos
// concordarem, ou ambos estão certos ou erraram da mesma maneira; e errar da
// mesma maneira por dois desenhos diferentes é muito menos provável do que
// errar uma vez. Sem isto, mil amostras provariam mil vezes a mesma coisa.

// TestFidelidadeDoQueGuardamosFaceAoQueOBancoRespondeu percorre a amostra e
// confronta, campo a campo, a oferta com o corpo em bruto que a originou.
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

	// Dois trabalhadores: é o PorBancoOmissao medido, e não se sobe só porque
	// esta corrida é longa. Cada um tem o seu gravador — partilhar um seria
	// comparar a oferta de um pedido com o corpo de outro.
	const trabalhadores = 2

	inicio := time.Now()
	entrada := make(chan caso)
	var wg sync.WaitGroup

	for range trabalhadores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			grav := &gravador{real: transporte.NovoCliente(nil)}
			banco := cgd.Novo(grav)

			for c := range entrada {
				// ⚠️ Reiniciar por caso, e não deixar isto ao transporte. Um
				// pedido recusado pelos limites nunca chega ao /calculate, e o
				// gravador ficava com o corpo do caso anterior: comparava-se
				// uma recusa com a resposta de outro pedido. Deu 28 falsos
				// alarmes em 2900 na primeira corrida.
				grav.reiniciar()

				ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
				oferta, err := banco.Simular(ctx, c.pedido)
				cancelar()

				pedidos.Add(grav.pedidos())
				bytesLidos.Add(grav.bytes())

				corpo := grav.ultimoCalculate()
				if corpo == nil {
					// Sem corpo não houve /calculate: o pedido foi recusado
					// pelos limites, antes da rede. Não é amostra de fidelidade.
					recusadas.Add(1)
					continue
				}
				if err != nil {
					// A CGD recusou. Confirma-se que a recusa é mesmo a dela.
					if !recusaFiel(corpo, err) {
						falhas.Add(1)
						t.Errorf("%s: o erro não corresponde ao corpo %q: %v", c.nome, corpo, err)
					}
					recusadas.Add(1)
					continue
				}

				n, err := conferirContraOEspelho(c.nome, corpo, oferta, c.pedido)
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
	// haver pedidos feitos que cheguem. Nem todo o pedido dá amostra: um caso
	// recusado pelos limites nunca chega ao /calculate e não tem corpo para
	// confrontar. Na corrida de 2026-07-26 foram 111 em 3000, e o intervalo
	// saiu a 0,104 % quando se queria 0,100 % — o botão media a coisa errada.
	gerados := 0
	for _, c := range formasDeResposta() {
		entrada <- c
		gerados++
	}
	r := rand.New(rand.NewPCG(20260726, 9))
	// O tecto é a guarda contra um dia em que quase tudo seja recusado: mais
	// vale sair com o intervalo pior do que ficar a bater no banco sem fim.
	for conferidas.Load() < int64(alvo) && gerados < alvo*2 {
		entrada <- casoAleatorio(r, gerados)
		gerados++
	}
	close(entrada)
	wg.Wait()
	t.Logf("%d pedidos feitos para %d ofertas conferidas", gerados, conferidas.Load())

	demorou := time.Since(inicio)
	n, f := conferidas.Load(), falhas.Load()
	t.Logf("%d ofertas conferidas, %d campos comparados, %d recusas, %d falhas, em %s",
		n, campos.Load(), recusadas.Load(), f, demorou.Round(time.Second))
	t.Logf("custo para a CGD: %d pedidos HTTP, %.1f MB, %s por simulação",
		pedidos.Load(), float64(bytesLidos.Load())/(1<<20),
		(demorou / time.Duration(max(gerados, 1))).Round(time.Millisecond))

	// ⚠️ A conclusão escreve-se com a regra dos três: com zero falhas em n
	// amostras, o limite superior do erro a 95 % de confiança é 3/n. Não é
	// «acertámos 100 %» — é «se errássemos mais do que isto, era pouco
	// provável não termos visto». Dizer mais do que isto seria dizer mais do
	// que se mediu.
	if f == 0 && n > 0 {
		t.Logf("MEDIDO: 0 falhas em %d ofertas → erro de leitura ≤ %.3f %% (95 %% de confiança, regra dos três)",
			n, 300.0/float64(n))
		if n < 3000 {
			t.Logf("⚠️ para sustentar «≤ 0,1 %%» são precisas 3000 ofertas conferidas; esta corrida tem %d. "+
				"Correr com CGD_AMOSTRAS=3000.", n)
		}
	}
}

// --- o espelho ---------------------------------------------------------------

// conferirContraOEspelho lê o corpo por um caminho independente e compara tudo
// o que a Oferta transporta. Devolve quantos campos comparou.
func conferirContraOEspelho(nome string, corpo []byte, o dominio.Oferta, pedido dominio.Pedido) (int, error) {
	var cru map[string]any
	if err := json.Unmarshal(corpo, &cru); err != nil {
		return 0, fmt.Errorf("o corpo não é JSON: %w", err)
	}
	dados, _ := cru["data"].(map[string]any)
	base, _ := dados["BaseResult"].(map[string]any)
	if base == nil {
		return 0, fmt.Errorf("a resposta não traz BaseResult, mas o Simular devolveu uma oferta")
	}

	var problemas []string
	comparados := 0

	taxa := func(campo string, veio *dominio.Taxa) {
		comparados++
		esperado := textoDe(base[campo])
		if err := igualTaxa(esperado, veio); err != nil {
			problemas = append(problemas, fmt.Sprintf("%s: %v", campo, err))
		}
	}
	dinheiro := func(campo string, veio *dominio.Dinheiro) {
		comparados++
		esperado := textoDe(base[campo])
		if err := igualDinheiro(esperado, veio); err != nil {
			problemas = append(problemas, fmt.Sprintf("%s: %v", campo, err))
		}
	}

	taxa("AnualNominalRate", o.TAN)
	taxa("APR", o.TAEG)
	taxa("Spread", o.Spread)
	taxa("VariableIndexRate", o.EuriborValor)
	dinheiro("Instalment", o.Prestacao)
	dinheiro("TotalPayableAmount", o.MTIC)

	// O indexante declara-se exactamente quando há fase indexada.
	comparados++
	temIndexada := textoDe(base["VariableIndexRate"]) != ""
	if temIndexada != (o.Indexante != "") {
		problemas = append(problemas, fmt.Sprintf(
			"indexante %q com VariableIndexRate %q", o.Indexante, textoDe(base["VariableIndexRate"])))
	}

	// As fases: durações, taxas e prestações, e o fecho no prazo aplicado.
	comparados += 3
	if err := conferirFases(base, o); err != nil {
		problemas = append(problemas, err.Error())
	}

	// ⚠️ O prazo que a CGD aplicou tem de estar declarado. É a fidelidade que
	// mais importa das que aqui estão: um plano de 10 anos guardado como se
	// fossem os 30 que se pediram é um número certo com o rótulo errado — foi
	// exactamente o que o v1 fazia na taxa fixa.
	comparados++
	if err := conferirPrazoDeclarado(base, o, pedido); err != nil {
		problemas = append(problemas, err.Error())
	}

	// O desconto dos packs sai em prosa (KAN-33), e prosa também se confere:
	// enquanto for o único sítio onde a segunda coluna de preço existe, tem de
	// bater com a diferença entre as duas variantes do corpo.
	comparados++
	if desconto, _ := dados["DiscountedResult"].(map[string]any); desconto != nil {
		if err := conferirNotaDosPacks(base, desconto, o); err != nil {
			problemas = append(problemas, err.Error())
		}
	}

	if len(problemas) > 0 {
		return comparados, fmt.Errorf("%s divergiu do corpo:\n    - %s", nome, strings.Join(problemas, "\n    - "))
	}
	return comparados, nil
}

// conferirPrazoDeclarado exige que uma diferença entre o prazo pedido e o
// aplicado apareça como ajuste, com o número lá dentro.
func conferirPrazoDeclarado(base map[string]any, o dominio.Oferta, pedido dominio.Pedido) error {
	aplicado := inteiroDe(base["TotalDuration"])
	if aplicado == pedido.PrazoAnos {
		return nil
	}
	// ⚠️ Exige-se **um** ajuste ao prazo, e não o último de uma cadeia. Uma
	// cadeia fabrica um «Pediu N anos» que ninguém pediu: foi o que a corrida
	// de 2026-07-26 apanhou, e a correcção foi colapsá-la num só.
	var doPrazo []dominio.Ajuste
	for _, a := range o.Ajustes() {
		if a.Campo() == dominio.AjustadoPrazoAnos {
			doPrazo = append(doPrazo, a)
		}
	}
	if len(doPrazo) > 1 {
		return fmt.Errorf("%d ajustes ao prazo em cadeia — o segundo diz «Pediu %v anos» a quem pediu %d",
			len(doPrazo), doPrazo[1].De(), pedido.PrazoAnos)
	}
	if len(doPrazo) == 1 {
		a := doPrazo[0]
		if a.Para() != aplicado {
			return fmt.Errorf("a CGD aplicou %d anos e o ajuste diz %v", aplicado, a.Para())
		}
		if a.De() != pedido.PrazoAnos {
			return fmt.Errorf("pediram-se %d anos e o ajuste diz que se pediram %v", pedido.PrazoAnos, a.De())
		}
		if a.Nota() == "" {
			return fmt.Errorf("o prazo mudou de %d para %d sem uma nota que o diga", pedido.PrazoAnos, aplicado)
		}
		return nil
	}
	return fmt.Errorf("pediram-se %d anos, a CGD aplicou %d, e não há ajuste nenhum a declará-lo",
		pedido.PrazoAnos, aplicado)
}

// conferirNotaDosPacks confirma que o desconto anunciado na nota é a diferença
// entre os dois spreads que vieram no corpo.
func conferirNotaDosPacks(base, desconto map[string]any, o dominio.Oferta) error {
	spreadBase, err := numeroDoEspelho(textoDe(base["Spread"]))
	if err != nil {
		return nil // sem spread não há desconto a anunciar
	}
	spreadPacks, err := numeroDoEspelho(textoDe(desconto["Spread"]))
	if err != nil {
		return nil
	}
	diferenca := spreadBase.Sub(spreadPacks)
	if diferenca.IsZero() {
		return nil
	}

	querido := diferenca.String()
	for _, nota := range o.Notas() {
		if strings.Contains(nota, "desceria "+querido+" p.p.") {
			return nil
		}
	}
	return fmt.Errorf("o corpo dá um desconto de packs de %s p.p. e nenhuma nota o diz: %q",
		querido, strings.Join(o.Notas(), " | "))
}

func conferirFases(base map[string]any, o dominio.Oferta) error {
	type esperada struct {
		meses     int
		taxa      string
		prestacao string
	}
	var querido []esperada
	if m := inteiroDe(base["FixedDurationMonths"]); m > 0 {
		querido = append(querido, esperada{m, textoDe(base["FixedAnualNominalRate"]), textoDe(base["FixedInstalment"])})
	}
	if m := inteiroDe(base["VariableDurationMonths"]); m > 0 {
		querido = append(querido, esperada{m, textoDe(base["VariableAnualNominalRate"]), textoDe(base["VariableInstalment"])})
	}

	if len(querido) != len(o.Fases) {
		return fmt.Errorf("o corpo tem %d fases e a oferta %d", len(querido), len(o.Fases))
	}

	acumulado := 0
	for i, q := range querido {
		acumulado += q.meses
		f := o.Fases[i]
		if f.AteMes != acumulado {
			return fmt.Errorf("fase %d acaba ao mês %d e o corpo dá %d", i+1, f.AteMes, acumulado)
		}
		if err := igualTaxa(q.taxa, &f.Taxa); err != nil {
			return fmt.Errorf("taxa da fase %d: %w", i+1, err)
		}
		if err := igualDinheiro(q.prestacao, &f.Prestacao); err != nil {
			return fmt.Errorf("prestação da fase %d: %w", i+1, err)
		}
	}

	if meses := inteiroDe(base["TotalDuration"]) * 12; acumulado != meses {
		return fmt.Errorf("as fases somam %d meses e o TotalDuration diz %d", acumulado, meses)
	}
	return nil
}

// recusaFiel confirma que um erro nosso corresponde mesmo a uma recusa da CGD,
// e não a uma leitura falhada de uma resposta boa.
func recusaFiel(corpo []byte, err error) bool {
	var cru map[string]any
	if json.Unmarshal(corpo, &cru) != nil {
		return true // corpo ilegível: o erro é legítimo
	}
	sucesso, _ := cru["success"].(bool)
	return !sucesso && err != nil
}

// --- conversão independente --------------------------------------------------
//
// ⚠️ Escrita à mão de propósito, sem tocar no numeroPT. O separador é o espaço
// não-quebrável (U+00A0) e o decimal é a vírgula; aqui isso trata-se por
// substituição explícita de runas, e não por um Replacer partilhado. Se o
// numeroPT ganhar um defeito, este não o acompanha.

func textoDe(v any) string {
	s, _ := v.(string)
	return s
}

func inteiroDe(v any) int {
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return int(f)
}

func numeroDoEspelho(s string) (decimal.Decimal, error) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ',':
			b.WriteRune('.')
		case r == ' ', r == ' ', r == ' ', r == ' ', r == '.':
			// separadores de milhares: caem fora
		default:
			return decimal.Zero, fmt.Errorf("carácter inesperado %q em %q", r, s)
		}
	}
	limpo := b.String()
	if limpo == "" {
		return decimal.Zero, fmt.Errorf("sem dígitos em %q", s)
	}
	f, err := strconv.ParseFloat(limpo, 64)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(f), nil
}

func igualTaxa(esperado string, veio *dominio.Taxa) error {
	if esperado == "" {
		if veio != nil {
			return fmt.Errorf("o corpo não traz e a oferta diz %s", veio)
		}
		return nil
	}
	if veio == nil {
		return fmt.Errorf("o corpo traz %q e a oferta não traz nada", esperado)
	}
	q, err := numeroDoEspelho(esperado)
	if err != nil {
		return err
	}
	if !veio.Decimal().Equal(q) {
		return fmt.Errorf("o corpo diz %q e a oferta diz %s", esperado, veio)
	}
	return nil
}

func igualDinheiro(esperado string, veio *dominio.Dinheiro) error {
	if esperado == "" {
		if veio != nil {
			return fmt.Errorf("o corpo não traz e a oferta diz %s", veio)
		}
		return nil
	}
	if veio == nil {
		return fmt.Errorf("o corpo traz %q e a oferta não traz nada", esperado)
	}
	q, err := numeroDoEspelho(esperado)
	if err != nil {
		return err
	}
	if !veio.Decimal().Equal(q) {
		return fmt.Errorf("o corpo diz %q e a oferta diz %s", esperado, veio)
	}
	return nil
}

// --- o gravador --------------------------------------------------------------

// gravador é um transporte que passa tudo ao cliente real e fica com uma cópia
// do último corpo do /calculate. É como se obtém, do mesmo pedido, a oferta e o
// corpo que a originou.
type gravador struct {
	real transporte.HTTPSimples

	mu      sync.Mutex
	ultimo  []byte
	nPedido int64
	nBytes  int64
}

func (g *gravador) reiniciar() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ultimo = nil
}

func (g *gravador) Fazer(ctx context.Context, req *http.Request) (*http.Response, error) {
	ehCalculate := req.URL.Path == "/calculate"

	resp, err := g.real.Fazer(ctx, req)
	if err != nil {
		return nil, err
	}

	corpo, lerErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if lerErr != nil {
		return nil, lerErr
	}
	// O corpo volta a estar por ler, para quem o receba a seguir.
	resp.Body = io.NopCloser(bytes.NewReader(corpo))

	g.mu.Lock()
	g.nPedido++
	g.nBytes += int64(len(corpo))
	if ehCalculate {
		g.ultimo = corpo
	}
	g.mu.Unlock()

	return resp, nil
}

func (g *gravador) ultimoCalculate() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ultimo
}

func (g *gravador) pedidos() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nPedido
	g.nPedido = 0
	return n
}

func (g *gravador) bytes() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nBytes
	g.nBytes = 0
	return n
}

// --- a amostra ---------------------------------------------------------------

type caso struct {
	nome   string
	pedido dominio.Pedido
}

// amostra monta os pedidos a fazer: primeiro a cobertura exaustiva das formas
// de resposta que a CGD sabe produzir, depois pontos aleatórios sobre os
// contínuos.
//
// ⚠️ A distinção é deliberada. O que muda a **forma** da resposta é discreto —
// tipo de taxa, período fixo, finalidade, Medida Jovem — e isso varre-se todo.
// O que muda só os **números** é contínuo — montante, valor do imóvel, prazo —
// e isso amostra-se. Mil pedidos aleatórios sobre a mesma forma provam mil
// vezes a mesma coisa; um pedido por forma prova quarenta e quatro coisas.
func alvoDeAmostras(t *testing.T) int {
	t.Helper()
	alvo := 250
	if v := os.Getenv("CGD_AMOSTRAS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("CGD_AMOSTRAS inválido: %q", v)
		}
		alvo = n
	}
	return alvo
}

// formasDeResposta é a varredura exaustiva do que é discreto.
func formasDeResposta() []caso {
	var casos []caso
	acrescentar := func(nome string, p dominio.Pedido) {
		casos = append(casos, caso{nome: nome, pedido: p})
	}

	base := func(prazo int, taxa dominio.TipoTaxa, periodo *int, f dominio.Finalidade, mj bool) dominio.Pedido {
		valor, montante := int64(250_000), int64(200_000) // LTV 80 %, válido em todas as finalidades
		if mj {
			montante = 225_000 // LTV 90 %: a Medida Jovem exige ≥ 85 %
		}
		return dominio.Pedido{
			ValorImovel:     dominio.DinheiroDeInteiro(valor),
			Montante:        dominio.DinheiroDeInteiro(montante),
			PrazoAnos:       prazo,
			TipoTaxa:        taxa,
			PeriodoFixoAnos: periodo,
			Finalidade:      f,
			Localizacao:     dominio.LocalizacaoContinente,
			GarantiaPublica: mj,
			Titulares:       []dominio.Titular{{DataNascimento: nascidoHaAnos(30)}},
		}
	}

	finalidades := []dominio.Finalidade{
		dominio.FinalidadePropria, dominio.FinalidadeSecundaria, dominio.FinalidadeArrendamento,
	}
	// A secundária e o arrendamento têm prazo máximo de 30; a própria, 40.
	prazoDe := func(f dominio.Finalidade, querido int) int {
		if f != dominio.FinalidadePropria && querido > 30 {
			return 30
		}
		return querido
	}

	for _, f := range finalidades {
		for _, mj := range []bool{false, true} {
			// A Medida Jovem só se combina com LTV ≥ 85 %, e aí a secundária e
			// o arrendamento levam LTV máximo de 80 % sem ela — com ela sobem.
			etiqueta := fmt.Sprintf("%s/mj=%v", f, mj)

			acrescentar("variavel/"+etiqueta, base(prazoDe(f, 30), dominio.TaxaVariavel, nil, f, mj))

			// Mista: os sete períodos que a CGD vende.
			for _, periodo := range []int{5, 10, 15, 20, 25, 30, 35} {
				prazo := prazoDe(f, 40)
				if periodo > prazo {
					continue
				}
				p := periodo
				acrescentar(fmt.Sprintf("mista/%da/%s", periodo, etiqueta),
					base(prazo, dominio.TaxaMista, &p, f, mj))
			}

			// Fixa: todos os anos de 5 a 40 que a finalidade permite. É o
			// código do período que manda no prazo, por isso o prazo é o
			// período.
			for anos := 5; anos <= 40; anos++ {
				if anos > prazoDe(f, 40) {
					continue
				}
				acrescentar(fmt.Sprintf("fixa/%da/%s", anos, etiqueta),
					base(anos, dominio.TaxaFixa, nil, f, mj))
			}
		}
	}
	return casos
}

// casoAleatorio sorteia um ponto sobre os contínuos, dentro do que a CGD vende.
//
// ⚠️ Os montantes vão de 5 000 € a 900 000 € de propósito: é a gama onde o
// separador de milhares aparece e desaparece, e o formato do número é
// precisamente o que este teste existe para vigiar.
func casoAleatorio(r *rand.Rand, i int) caso {
	finalidades := []dominio.Finalidade{
		dominio.FinalidadePropria, dominio.FinalidadeSecundaria, dominio.FinalidadeArrendamento,
	}
	f := finalidades[r.IntN(len(finalidades))]

	mj := r.IntN(4) == 0
	ltvMax := 90
	if f != dominio.FinalidadePropria {
		ltvMax = 80
	}
	ltvMin := 15
	if mj {
		ltvMin, ltvMax = 85, 100
	}
	ltv := ltvMin + r.IntN(ltvMax-ltvMin+1)

	montante := int64(5_000 + r.IntN(895_000))
	if mj && montante > 450_000 {
		montante = 450_000
	}
	// ⚠️ Arredonda para cima: a divisão inteira truncava o valor do imóvel e
	// punha o rácio uns milésimos ACIMA do alvo — o que fazia o nosso próprio
	// código recusar, com razão, um caso que se queria dentro dos limites.
	valorImovel := (montante*100 + int64(ltv) - 1) / int64(ltv)

	prazoMax := 40
	if f != dominio.FinalidadePropria {
		prazoMax = 30
	}
	prazo := 1 + r.IntN(prazoMax)

	// ⚠️ A taxa não se sorteia em três iguais: 60 % variável, 20 % mista, 20 %
	// fixa. E a razão é de custo, não de estatística, por isso escreve-se.
	//
	// Cada simulação com fase fixa obriga o Simular a puxar outra vez os 77 KB
	// da página da CGD para ler os períodos (KAN-36). As **formas** de resposta
	// da mista e da fixa já estão varridas todas na cobertura exaustiva; o que
	// estes pontos aleatórios acrescentam são números, e o risco que eles
	// vigiam — o formato — é o mesmo nas três. Pesar a amostra para o lado que
	// não repete a descarga tira 160 MB a esta corrida sem tirar informação.
	var taxa dominio.TipoTaxa
	switch sorte := r.IntN(10); {
	case sorte < 6:
		taxa = dominio.TaxaVariavel
	case sorte < 8:
		taxa = dominio.TaxaMista
	default:
		taxa = dominio.TaxaFixa
	}

	var periodo *int
	switch taxa {
	case dominio.TaxaMista:
		validos := []int{5, 10, 15, 20, 25, 30, 35}
		p := validos[r.IntN(len(validos))]
		periodo = &p
	case dominio.TaxaFixa:
		if prazo < 5 {
			prazo = 5
		}
	case dominio.TaxaVariavel:
	}

	return caso{
		nome: fmt.Sprintf("aleatorio/%d/%s/ltv%d/%da/%s", i, taxa, ltv, prazo, f),
		pedido: dominio.Pedido{
			ValorImovel:     dominio.DinheiroDeInteiro(valorImovel),
			Montante:        dominio.DinheiroDeInteiro(montante),
			PrazoAnos:       prazo,
			TipoTaxa:        taxa,
			PeriodoFixoAnos: periodo,
			Finalidade:      f,
			Localizacao:     dominio.LocalizacaoContinente,
			GarantiaPublica: mj,
			Titulares:       []dominio.Titular{{DataNascimento: nascidoHaAnos(25 + r.IntN(20))}},
		},
	}
}
