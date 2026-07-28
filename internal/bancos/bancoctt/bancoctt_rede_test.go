//go:build rede

package bancoctt_test

import (
	"context"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/bancoctt"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Este ficheiro bate no simulador a sério e **não corre no portão** — depende de
// um servidor de terceiros estar de pé, e um portão que amarela por isso deixa
// de ser lido. Corre-se à mão:
//
//	go test -race -tags rede -run AoVivo ./internal/bancos/bancoctt/
//
// São três pedidos, um por modalidade. É o que confirma que o parser ainda
// corresponde ao que o Banco CTT devolve hoje.

// TestAoVivoOBancoCTTResponde é o passo 8 do CONTRATO-BANCO.md §7: confrontar
// com o banco e registar a data.
//
// ⚠️ Confrontado a 2026-07-28. Para 250 000 € de imóvel, 200 000 € financiados,
// 30 anos de prazo, habitação própria, titular de 35 anos e **sem** vendas
// associadas:
//
//	variável 12M  TAN 4,148 %  TAEG 4,7 %  spread 1,350  Euribor 2,798  prestação  971,97 €  MTIC 368 333,50 €
//	mista 5a      TAN 4,000 %  TAEG 4,6 %  spread 1,350  Euribor 2,339  prestação  954,83 €  MTIC 363 401,50 €
//	fixa 30a      TAN 4,650 %  TAEG 5,3 %  sem spread nem Euribor       prestação 1 031,27 €  MTIC 390 919,90 €
//
// Três coisas a reter destes números:
//
//   - **na mista, o spread é 1,350 e a TAN da fase fixa é 4,000.** O campo
//     `Spread` de topo do banco traz 4,000 — igual à TAN —, e é essa a armadilha
//     que o parser fecha. Publicá-la punha o Banco CTT a 4,000 de spread ao lado
//     dos 1,350 dos outros;
//   - **a Euribor da mista é 2,339 e a da variável é 2,798.** São tenores
//     diferentes — 3M contra 12M —, e nenhum dos dois é escolha nossa;
//   - **as vendas associadas descontam 0,60 p.p.**: medido no mesmo dia, o
//     spread da variável passa de 1,350 para 0,750.
//
// ⚠️ O que este teste afirma **não são os números do dia** — um preçário muda, e
// um teste que fixasse 4,148 % reprovaria amanhã sem nada estar partido. O que
// se afirma são as identidades que têm de valer com qualquer preçário: a fase
// indexada fecha em Euribor + spread, o plano cobre o contrato, e o spread da
// mista não é a TAN da fase fixa.
func TestAoVivoOBancoCTTResponde(t *testing.T) {
	anos := func(n int) *int { return &n }

	casos := []struct {
		nome    string
		pedido  func(dominio.Pedido) dominio.Pedido
		indexad bool
		fases   int
	}{
		{
			nome:    "variável 12M",
			pedido:  func(p dominio.Pedido) dominio.Pedido { return p },
			indexad: true,
			fases:   1,
		},
		{
			nome: "mista 5 anos",
			pedido: func(p dominio.Pedido) dominio.Pedido {
				p.TipoTaxa = dominio.TaxaMista
				p.PeriodoFixoAnos = anos(5)
				return p
			},
			indexad: true,
			fases:   2,
		},
		{
			// ⚠️ Numa fixa pura não há indexante nem spread sobre ele, e as
			// colunas ficam nulas. O banco preenche `Spread` com a própria TAN —
			// lê-lo era publicar um spread de 4,650 ao lado dos 1,350 dos outros.
			nome: "fixa 30 anos",
			pedido: func(p dominio.Pedido) dominio.Pedido {
				p.TipoTaxa = dominio.TaxaFixa
				return p
			},
			indexad: false,
			fases:   1,
		},
	}

	// Sem Timeout no cliente, de propósito: o prazo é o do ctx, e dois prazos a
	// competir tornam difícil explicar quem desistiu primeiro.
	banco := bancoctt.Novo(transporte.NovoCliente(nil))

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelar()

			oferta, err := banco.Simular(ctx, c.pedido(pedidoBase(t)))
			if err != nil {
				t.Fatalf("Simular ao vivo: %v", err)
			}

			if oferta.TAN == nil || oferta.TAEG == nil || oferta.Prestacao == nil || oferta.MTIC == nil {
				t.Fatalf("o Banco CTT respondeu sem TAN, TAEG, prestação ou MTIC: %+v", oferta)
			}
			if len(oferta.Fases) != c.fases {
				t.Errorf("esperava %d fase(s), vieram %+v", c.fases, oferta.Fases)
			}

			if !c.indexad {
				// Numa fixa pura, publicar spread ou Euribor é inventar.
				if oferta.Spread != nil || oferta.EuriborValor != nil || oferta.Indexante != "" {
					t.Errorf("a fixa pura trouxe spread %v, Euribor %v e indexante %q — e não tem fase indexada",
						oferta.Spread, oferta.EuriborValor, oferta.Indexante)
				}
			} else {
				if oferta.Spread == nil || oferta.EuriborValor == nil {
					t.Fatalf("a fase indexada veio sem spread ou sem Euribor: %+v", oferta)
				}
				// ⚠️ A identidade que apanha o campo trocado: se o parser lesse o
				// `Spread` de topo na mista (que vem igual à TAN da fase fixa),
				// esta soma deixava de fechar.
				indexada := oferta.Fases[len(oferta.Fases)-1].Taxa
				if soma := oferta.EuriborValor.Add(*oferta.Spread); !indexada.Equal(soma) {
					t.Errorf("a fase indexada diz TAN %s, e Euribor %s + spread %s dá %s",
						indexada, oferta.EuriborValor, oferta.Spread, soma)
				}
			}

			t.Logf("%s: TAN %s, TAEG %s, spread %v, Euribor %v, prestação %s, MTIC %s",
				c.nome, oferta.TAN, oferta.TAEG, oferta.Spread, oferta.EuriborValor,
				oferta.Prestacao, oferta.MTIC)
			for _, n := range oferta.Notas() {
				t.Logf("  nota: %s", n)
			}
		})
	}
}

// TestAoVivoAsDuasColunasSaemDoMesmoPedido confirma o que torna este banco
// barato de varrer: as vendas associadas são escolha de LEITURA, não de pedido.
//
// ⚠️ Se um dia deixarem de vir as duas colunas no mesmo corpo, isto falha — e é
// a diferença entre um pedido por linha do catálogo e dois.
func TestAoVivoAsDuasColunasSaemDoMesmoPedido(t *testing.T) {
	banco := bancoctt.Novo(transporte.NovoCliente(nil))

	ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelar()

	sem, err := banco.Simular(ctx, pedidoBase(t))
	if err != nil {
		t.Fatalf("sem produtos: %v", err)
	}

	comProdutos := pedidoBase(t)
	comProdutos.Produtos = []string{bancoctt.ProdutoVendasAssociadas}
	com, err := banco.Simular(ctx, comProdutos)
	if err != nil {
		t.Fatalf("com vendas associadas: %v", err)
	}

	if sem.Spread == nil || com.Spread == nil {
		t.Fatalf("uma das ofertas veio sem spread: sem=%v com=%v", sem.Spread, com.Spread)
	}
	if com.Spread.Cmp(*sem.Spread) >= 0 {
		t.Errorf("as vendas associadas não descontaram nada: sem %s, com %s", sem.Spread, com.Spread)
	}
	t.Logf("spread sem vendas associadas %s, com %s", sem.Spread, com.Spread)
}
