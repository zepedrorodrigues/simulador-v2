//go:build rede

package santander_test

import (
	"context"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/santander"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Este ficheiro bate no simulador a sério e **não corre no portão** — depende de
// um servidor de terceiros estar de pé. Corre-se à mão:
//
//	go test -race -tags rede -run AoVivo ./internal/bancos/santander/
//
// São quatro pedidos por simulação: configuração, limites, catálogo e a
// simulação em si.

// TestAoVivoOSantanderResponde é o passo 8 do CONTRATO-BANCO.md §7.
//
// ⚠️ Confrontado a 2026-07-28. Para 250 000 € de imóvel, 200 000 € financiados,
// 30 anos, titular de 30 anos, COM o plano bonificado:
//
//	variável 6M   TAN 3,096 %  TAEG 4,0 %  spread 0,800  Euribor 2,596  prestação   853,60 €
//	mista 3a      TAN 2,850 %  TAEG 3,9 %  spread 0,800  Euribor 2,596  prestação   827,11 €
//	fixa 30a      TAN 4,400 %  TAEG 5,1 %  sem spread nem Euribor       prestação 1 001,52 €
//
// Três coisas a reter destes números:
//
//   - **o spread publicado é 0,800 e a TAN de topo é 3,096** — que é 2,596 + 0,5,
//     o promocional. Não fecham, e é suposto: a TAN de topo é o que se começa a
//     pagar, e o spread é o que vigora a partir do mês 37;
//   - **a mista a 3 anos tem TAN 2,850 com spread 0,0** na fase fixa — o preço
//     dela é a taxa do banco (M28), não uma Euribor com margem;
//   - **na fixa, a bonificação não desconta**: a nota sai, e o MTIC é o mesmo
//     dos dois lados.
//
// ⚠️ O que este teste afirma **não são os números do dia** — um preçário muda. O
// que se afirma são as identidades que têm de valer com qualquer preçário: o
// plano fecha o contrato, a fase indexada leva Euribor + spread, e a fixa não
// publica indexante nenhum.
func TestAoVivoOSantanderResponde(t *testing.T) {
	anos := func(n int) *int { return &n }

	casos := []struct {
		nome     string
		pedido   func(dominio.Pedido) dominio.Pedido
		indexada bool
		fases    int
	}{
		{
			nome:     "variável 6M",
			pedido:   func(p dominio.Pedido) dominio.Pedido { return p },
			indexada: true,
			fases:    2, // ⚠️ duas: o promocional acaba ao mês 36
		},
		{
			nome: "mista 3 anos",
			pedido: func(p dominio.Pedido) dominio.Pedido {
				p.TipoTaxa = dominio.TaxaMista
				p.PeriodoFixoAnos = anos(3)
				return p
			},
			indexada: true,
			fases:    2,
		},
		{
			nome: "fixa 30 anos",
			pedido: func(p dominio.Pedido) dominio.Pedido {
				p.TipoTaxa = dominio.TaxaFixa
				return p
			},
			indexada: false,
			fases:    1,
		},
	}

	banco := santander.Novo(transporte.NovoCliente(nil))

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, cancelar := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancelar()

			p := c.pedido(pedidoBase(t))
			p.Produtos = []string{santander.ProdutoBonificado}

			oferta, err := banco.Simular(ctx, p)
			if err != nil {
				t.Fatalf("Simular ao vivo: %v", err)
			}
			if oferta.TAN == nil || oferta.TAEG == nil || oferta.Prestacao == nil || oferta.MTIC == nil {
				t.Fatalf("resposta sem campos nucleares: %+v", oferta)
			}
			if len(oferta.Fases) != c.fases {
				t.Errorf("esperava %d fase(s), vieram %d", c.fases, len(oferta.Fases))
			}
			if err := dominio.ValidarFases(oferta.Fases, prazoDe(oferta)); err != nil {
				t.Errorf("o plano não fecha o contrato: %v", err)
			}

			if !c.indexada {
				if oferta.Spread != nil || oferta.EuriborValor != nil || oferta.Indexante != "" {
					t.Errorf("a fixa publicou spread %v, Euribor %v e indexante %q",
						oferta.Spread, oferta.EuriborValor, oferta.Indexante)
				}
			} else {
				if oferta.Spread == nil || oferta.EuriborValor == nil {
					t.Fatalf("a fase indexada veio sem spread ou sem Euribor: %+v", oferta)
				}
				// ⚠️ A identidade fecha na ÚLTIMA fase, que é a indexada — e é
				// isso que confirma que se publicou o spread que vigora, e não o
				// promocional dos primeiros 36 meses.
				ultima := oferta.Fases[len(oferta.Fases)-1].Taxa
				if soma := oferta.EuriborValor.Add(*oferta.Spread); !ultima.Equal(soma) {
					t.Errorf("a última fase diz TAN %s, e Euribor %s + spread %s dá %s",
						ultima, oferta.EuriborValor, oferta.Spread, soma)
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

// TestAoVivoOPromocionalContinuaTemporario vigia a armadilha deste banco.
//
// ⚠️ Se o Santander deixar de partir a variável em duas fases, isto falha — e é
// isso que se quer: o dia em que o promocional passar a ser o preço do contrato
// é o dia em que o spread publicado tem de mudar.
func TestAoVivoOPromocionalContinuaTemporario(t *testing.T) {
	banco := santander.Novo(transporte.NovoCliente(nil))

	ctx, cancelar := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelar()

	p := pedidoBase(t)
	p.Produtos = []string{santander.ProdutoBonificado}

	oferta, err := banco.Simular(ctx, p)
	if err != nil {
		t.Fatalf("Simular ao vivo: %v", err)
	}
	if len(oferta.Fases) < 2 {
		t.Fatalf("a variável bonificada veio com %d fase(s): o promocional deixou de ser temporário?",
			len(oferta.Fases))
	}
	if oferta.Fases[0].Taxa.Cmp(oferta.Fases[1].Taxa) >= 0 {
		t.Errorf("a primeira fase (%s) não é mais barata do que a segunda (%s)",
			oferta.Fases[0].Taxa, oferta.Fases[1].Taxa)
	}
	t.Logf("promocional: %s até ao mês %d, depois %s",
		oferta.Fases[0].Taxa, oferta.Fases[0].AteMes, oferta.Fases[1].Taxa)
}

func prazoDe(o dominio.Oferta) int {
	if len(o.Fases) == 0 {
		return 0
	}
	return o.Fases[len(o.Fases)-1].AteMes / 12
}
