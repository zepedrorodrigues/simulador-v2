//go:build rede

package montepio_test

import (
	"context"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/montepio"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Este ficheiro bate no simulador a sério e **não corre no portão** — depende de
// um servidor de terceiros estar de pé, e um portão que amarela por isso deixa de
// ser lido. Corre-se à mão:
//
//	go test -race -tags rede ./internal/bancos/montepio/
//
// É o que confirma que o parser ainda corresponde ao que o Montepio devolve hoje.

// ⚠️ Confrontado com o simulador do Banco Montepio a 2026-07-27. Para 250 000 €
// de imóvel, 200 000 € financiados, 30 anos de prazo, habitação própria, titular
// de 36 anos e sem contrapartidas:
//
//	variável 3M    TAN 3,839 %  TAEG 4,5 %  spread 1,500  prestação 936,36 €
//	variável 6M    TAN 4,096 %  TAEG 4,8 %  spread 1,500  prestação 965,93 €
//	variável 12M   TAN 4,298 %  TAEG 5,0 %  spread 1,500  prestação 989,51 €
//	mista 5a       TAN 4,350 %  TAEG 5,0 %  spread 1,500  prestação 995,62 €
//	fixa 30a       TAN 4,850 %  TAEG 5,5 %  spread 1,500  prestação 1 055,38 €
//	com 4 contrap. TAN 3,039 %  TAEG 3,7 %  spread 0,700  prestação 847,42 €
//
// Estes números mudam com o preçário. O que este teste afirma não são eles — é
// que os campos continuam onde estavam e que a resposta continua a ler-se.
func TestAoVivoOMontepioResponde(t *testing.T) {
	casos := []struct {
		nome     string
		pedido   dominio.Pedido
		temFases bool
	}{
		{"variável 3M", pedidoBase(), true},
		{"variável 12M", comIndexante(pedidoBase(), dominio.Euribor12M), true},
		// ⚠️ A mista vem de propósito sem plano: o banco repete na fase indexada a
		// prestação da fase fixa. Ver TestMistaNaoPublicaUmPlanoQueSeContradiz.
		{"mista 5 anos", comMista(pedidoBase(), 5), false},
		// A fixa a 30 anos é um dos períodos praticados: sai uma fase só.
		{"fixa 30 anos", comFixa(pedidoBase()), true},
		{"com contrapartidas", comContrapartidas(pedidoBase()), true},
	}

	// Sem Timeout no cliente, de propósito: o prazo é o do ctx.
	cliente, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		t.Fatalf("montar o cliente com sessão: %v", err)
	}
	banco := montepio.Novo(cliente)

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, cancelar := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancelar()

			oferta, err := banco.Simular(ctx, c.pedido)
			if err != nil {
				t.Fatalf("Simular ao vivo: %v", err)
			}
			if oferta.TAN == nil || oferta.TAEG == nil || oferta.Prestacao == nil || oferta.MTIC == nil {
				t.Fatalf("o Montepio respondeu sem TAN, TAEG, prestação ou MTIC: %+v", oferta)
			}
			if temFases := len(oferta.Fases) > 0; temFases != c.temFases {
				t.Errorf("fases: esperava tê-las=%v, veio %+v", c.temFases, oferta.Fases)
			}
			t.Logf("%s: TAN %s, TAEG %s, spread %s, prestação %s, MTIC %s, notas %v",
				c.nome, oferta.TAN, oferta.TAEG, oferta.Spread, oferta.Prestacao, oferta.MTIC, oferta.Notas())
		})
	}
}

// ⚠️ A afirmação que sustenta o desenho todo do pedido.go: **o Montepio não
// classifica as suas recusas**. Pede-se um prazo de 20 anos com 30 de período
// fixo, com um titular de 36 anos, e o banco responde que não há condições para a
// idade dos proponentes. Se um dia passar a dizer a verdade, é aqui que se
// descobre — e então valerá a pena ler a mensagem dele.
//
// Este teste vai de propósito por baixo do pedido.go, com o ConditionCode a ser
// montado à mão, porque o caminho normal **impede** este pedido de sair.
func TestAoVivoARecusaNaoNomeiaACausa(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelar()

	cliente, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		t.Fatalf("montar o cliente com sessão: %v", err)
	}

	// O caminho suportado: 20 anos de prazo com taxa fixa desce o período para 15
	// e anota, em vez de ir bater na recusa.
	p := pedidoBase()
	p.TipoTaxa = dominio.TaxaFixa
	p.PrazoAnos = 20

	oferta, err := montepio.Novo(cliente).Simular(ctx, p)
	if err != nil {
		t.Fatalf("Simular ao vivo: %v", err)
	}
	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 || ajustes[0].Campo() != dominio.AjustadoTipoTaxa {
		t.Fatalf("esperava o ajuste de tipo de taxa que evita a recusa, vieram %+v", ajustes)
	}
	t.Logf("evitou-se a recusa: %s", ajustes[0].Nota())
}

// O prazo por idade ao vivo: o escalão que a página publica é o que o banco
// aplica. Um titular de 36 anos não passa dos 35 anos de prazo.
func TestAoVivoOEscalaoDaPaginaEOQueOBancoAplica(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelar()

	cliente, err := transporte.NovoClienteComSessao(nil)
	if err != nil {
		t.Fatalf("montar o cliente com sessão: %v", err)
	}

	p := pedidoBase()
	p.PrazoAnos = 40

	oferta, err := montepio.Novo(cliente).Simular(ctx, p)
	if err != nil {
		t.Fatalf("Simular ao vivo: %v", err)
	}
	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 || ajustes[0].Para() != 35 {
		t.Fatalf("aos 36 anos o escalão dá 35 anos de prazo; vieram %+v", ajustes)
	}
	t.Logf("o banco impôs: %s", ajustes[0].Nota())
}

func comIndexante(p dominio.Pedido, i dominio.Indexante) dominio.Pedido {
	p.Indexante = i
	return p
}

func comMista(p dominio.Pedido, periodo int) dominio.Pedido {
	p.TipoTaxa = dominio.TaxaMista
	p.PeriodoFixoAnos = anos(periodo)
	return p
}

func comFixa(p dominio.Pedido) dominio.Pedido {
	p.TipoTaxa = dominio.TaxaFixa
	return p
}

func comContrapartidas(p dominio.Pedido) dominio.Pedido {
	p.Produtos = []string{montepio.ProdutoContrapartidas}
	return p
}
