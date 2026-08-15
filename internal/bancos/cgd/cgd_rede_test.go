//go:build rede

package cgd_test

import (
	"context"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/cgd"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Este ficheiro bate no simulador a sério e **não corre no portão** — depende
// de um servidor de terceiros estar de pé, e um portão que amarela por isso
// deixa de ser lido. Corre-se à mão:
//
//	go test -race -tags rede ./internal/bancos/cgd/
//
// É o que confirma que o parser ainda corresponde ao que a CGD devolve hoje.

// ⚠️ Confrontado com o simuladorch.cgd.pt a 2026-07-26: nos três tipos de taxa,
// os números que saem daqui são os que o site mostra. Na altura, para 250 000 €
// de imóvel, 200 000 € financiados e 30 anos de prazo, em habitação própria:
//
//	variável   TAN 3,946 %  TAEG 4,5 %  spread 1,350  prestação 948,61 €
//	mista 5a   TAN 4,350 %  TAEG 4,9 %  spread 1,350  prestação 995,62 €, e 954,71 € depois
//	fixa 10a   TAN 4,850 %  TAEG 5,5 %  spread 1,350  prestação 2 106,68 € (10 anos, não 30)
//
// Estes números mudam com o preçário. O que este teste afirma não são eles — é
// que os campos continuam onde estavam e que a resposta continua a ler-se.
func TestAoVivoACGDResponde(t *testing.T) {
	casos := []struct {
		nome   string
		pedido dominio.Pedido
	}{
		{"variavel", pedidoBase()},
		{"mista 5 anos", comMista(pedidoBase(), 5)},
		{"fixa 10 anos", comFixa(pedidoBase(), 10)},
	}

	// Sem Timeout no cliente, de propósito: o prazo é o do ctx, e dois prazos a
	// competir tornam difícil explicar quem desistiu primeiro.
	banco := cgd.Novo(transporte.NovoCliente(nil), nil)

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelar()

			oferta, err := banco.Simular(ctx, c.pedido)
			if err != nil {
				t.Fatalf("Simular ao vivo: %v", err)
			}
			if oferta.TAN == nil || oferta.TAEG == nil || oferta.Prestacao == nil {
				t.Fatalf("a CGD respondeu sem TAN, TAEG ou prestação: %+v", oferta)
			}
			if len(oferta.Fases) == 0 {
				t.Error("a oferta veio sem plano de fases")
			}
			t.Logf("%s: TAN %s, TAEG %s, spread %s, prestação %s, MTIC %s",
				c.nome, oferta.TAN, oferta.TAEG, oferta.Spread, oferta.Prestacao, oferta.MTIC)
		})
	}
}
