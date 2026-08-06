//go:build latencia

package aovivo_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/aovivo"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
)

// A latência por banco — o primeiro dos três números de que a Fase 6 depende e
// que o `PLAN.md` diz que não temos.
//
// Corre-se à mão, e nunca no portão:
//
//	go test -tags latencia -timeout 30m ./internal/aplicacao/aovivo/
//
// ⚠️ **Mede-se o que se serve, e não o banco em bruto.** A medição atravessa o
// `aovivo.Pedir` — o mesmo caminho do `POST /api/v1/ofertas/{banco}` —, porque o
// número que interessa é quanto espera uma pessoa, e não quanto demora um GET.
//
// ⚠️ **Sequencial, com pausa, e nunca em paralelo.** É medição de latência e não
// de carga: um banco de cada vez, uma amostra de cada vez. Disparar as amostras
// juntas media outra coisa — e media-a carregando o simulador público de um
// terceiro, que é precisamente o que o `ARQUITETURA.md` manda reduzir ao mínimo
// que funciona.
//
// ⚠️ **A hora faz parte da medição.** Um timeout dimensionado com números de
// madrugada corta clientes às 11h. Corre-se em hora de expediente de propósito,
// e a hora fica escrita no relatório.

// amostrasPorBanco é quantas simulações se fazem a cada banco.
//
// ⚠️ Cinco é o que chega para um **máximo observado**, e não para um percentil.
// Um p95 honesto pede dezenas de amostras, e dezenas × 5 bancos × até 4 pedidos
// por simulação é carga a sério contra terceiros. O que este teste produz é um
// limite inferior do que o banco pode demorar — que é quanto basta para
// desmontar um timeout escolhido no ar.
var amostrasPorBanco = leInteiro("LATENCIA_AMOSTRAS", 5)

// pausaEntreAmostras separa duas perguntas ao mesmo banco.
//
// ⚠️ Existe para a medição não ser um mini-ataque: cinco pedidos seguidos sem
// pausa são um pico, e um pico mede o comportamento do banco sob pico.
var pausaEntreAmostras = 3 * time.Second

// prazoDaMedicao é generoso de propósito: aqui quer-se saber quanto o banco
// demora, e um prazo curto media o nosso próprio corte.
const prazoDaMedicao = 60 * time.Second

func TestMedirALatenciaDeCadaBanco(t *testing.T) {
	registo := bancos.Predefinido()
	ids := registo.IDs()
	if len(ids) == 0 {
		t.Fatal("o registo não tem bancos — a medição não teria a quem perguntar")
	}

	inicio := time.Now()
	t.Logf("medição começada a %s (hora local), %d amostras por banco, %d bancos",
		inicio.Format("2006-01-02 15:04"), amostrasPorBanco, len(ids))

	type resultado struct {
		id       string
		amostras []time.Duration
		falhas   []string
	}
	var todos []resultado

	for _, id := range ids {
		r := resultado{id: id}

		for i := range amostrasPorBanco {
			if i > 0 {
				time.Sleep(pausaEntreAmostras)
			}

			// ⚠️ Transportes novos a cada amostra, como em produção: o servidor
			// constrói-os por pedido, e reaproveitá-los aqui media uma sessão
			// quente que o cliente real nunca tem.
			banco, err := construir(registo, id)
			if err != nil {
				r.falhas = append(r.falhas, fmt.Sprintf("amostra %d: montar o banco: %v", i+1, err))
				continue
			}

			antes := time.Now()
			oferta := aovivo.Pedir(context.Background(), banco, pedido(t), time.Now, prazoDaMedicao)
			demorou := time.Since(antes)

			if !oferta.Sucesso() {
				r.falhas = append(r.falhas, fmt.Sprintf(
					"amostra %d (%s): %s — %s", i+1, demorou.Round(time.Millisecond),
					oferta.Erro.Codigo, oferta.Erro.Mensagem))
				continue
			}
			r.amostras = append(r.amostras, demorou)
		}
		todos = append(todos, r)

		t.Logf("%-12s %s", id, resumo(r.amostras, len(r.falhas)))
		for _, f := range r.falhas {
			t.Logf("%-12s   ⚠️ %s", "", f)
		}
	}

	// --- o relatório, que é o produto deste teste ---------------------------

	t.Log("")
	t.Logf("=== latência por banco, medida a %s ===", inicio.Format("2006-01-02 15:04 MST"))
	t.Logf("%-12s %8s %8s %8s %8s %6s", "banco", "min", "mediana", "max", "n", "falhas")

	var piorDeTodos time.Duration
	var piorBanco string
	for _, r := range todos {
		if len(r.amostras) == 0 {
			t.Logf("%-12s %8s %8s %8s %8d %6d", r.id, "—", "—", "—", 0, len(r.falhas))
			continue
		}
		mn, med, mx := estatisticas(r.amostras)
		t.Logf("%-12s %8s %8s %8s %8d %6d",
			r.id, arredondar(mn), arredondar(med), arredondar(mx), len(r.amostras), len(r.falhas))
		if mx > piorDeTodos {
			piorDeTodos, piorBanco = mx, r.id
		}
	}

	if piorDeTodos == 0 {
		t.Fatal("nenhum banco respondeu — não há medição nenhuma, e um relatório vazio " +
			"não é um resultado")
	}

	// ⚠️ **Isto é uma leitura, não uma recomendação automática.** O máximo
	// observado em poucas amostras é um limite inferior do pior caso; quem fixa o
	// timeout multiplica-o por uma margem e escreve porquê.
	t.Logf("")
	t.Logf("máximo observado: %s, no %s", arredondar(piorDeTodos), piorBanco)
	t.Logf("a medição inteira demorou %s", time.Since(inicio).Round(time.Second))
}

// --- ajudantes ---------------------------------------------------------------

func construir(registo *bancos.Registo, id string) (bancos.Banco, error) {
	comSessao, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		return nil, err
	}
	return registo.Construir(id, bancos.Transportes{
		HTTP:      transporte.NovoCliente(nil),
		ComSessao: comSessao,
	})
}

func estatisticas(as []time.Duration) (mn, mediana, mx time.Duration) {
	ordenadas := append([]time.Duration(nil), as...)
	sort.Slice(ordenadas, func(i, j int) bool { return ordenadas[i] < ordenadas[j] })
	return ordenadas[0], ordenadas[len(ordenadas)/2], ordenadas[len(ordenadas)-1]
}

func resumo(as []time.Duration, falhas int) string {
	if len(as) == 0 {
		return fmt.Sprintf("sem amostra boa (%d falhas)", falhas)
	}
	mn, med, mx := estatisticas(as)
	return fmt.Sprintf("min %s · mediana %s · max %s · n=%d · falhas=%d",
		arredondar(mn), arredondar(med), arredondar(mx), len(as), falhas)
}

func arredondar(d time.Duration) string { return d.Round(10 * time.Millisecond).String() }

func leInteiro(chave string, omissao int) int {
	if v := os.Getenv(chave); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return omissao
}
