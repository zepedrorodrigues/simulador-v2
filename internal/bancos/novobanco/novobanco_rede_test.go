//go:build rede

package novobanco_test

import (
	"context"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/novobanco"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Este ficheiro bate no simulador a sério e **não corre no portão** — depende de
// um servidor de terceiros estar de pé, e um portão que amarela por isso deixa
// de ser lido. Corre-se à mão:
//
//	go test -race -tags rede ./internal/bancos/novobanco/
//
// É o que confirma que o parser ainda corresponde ao que o Novo Banco devolve
// hoje.

// ⚠️ Confrontado com o simulador do Novo Banco a 2026-07-26: nos três tipos de
// taxa, os números que saem daqui são os que a API devolve. Na altura, para
// 250 000 € de imóvel, 200 000 € financiados, 30 anos de prazo, habitação
// própria, titular nascido a 1990-01-15 e com as duas bonificações ligadas:
//
//	variável 12M  TAN 3,698 %  TAEG 4,4 %  spread 0,900  prestação 920,34 €
//	variável 3M   TAN 3,239 %  TAEG 3,9 %  spread 0,900  prestação 869,21 €
//	mista 5a      TAN 3,992 %  TAEG 4,7 %  spread 0,900  prestação 953,91 €
//	fixa 10a      TAN 5,124 %  TAEG 5,9 %  spread 0,900  prestação 2 133,45 € (10 anos)
//
// Estes números mudam com o preçário. O que este teste afirma não são eles — é
// que os campos continuam onde estavam e que a resposta continua a ler-se.
func TestAoVivoONovoBancoResponde(t *testing.T) {
	casos := []struct {
		nome     string
		pedido   dominio.Pedido
		temFases bool
	}{
		{"variavel 12M", pedidoBase(), true},
		{"variavel 3M", comIndexante(pedidoBase(), dominio.Euribor3M), true},
		// ⚠️ A mista vem de propósito sem fases: o banco não diz o que se passa
		// depois do período fixo. Ver TestMistaNaoInventaOQueOBancoNaoDiz.
		{"mista 5 anos", comMista(pedidoBase(), 5), false},
		{"fixa 10 anos", comFixa(pedidoBase(), 10), true},
	}

	// Sem Timeout no cliente, de propósito: o prazo é o do ctx, e dois prazos a
	// competir tornam difícil explicar quem desistiu primeiro.
	banco := novobanco.Novo(transporte.NovoCliente(nil), nil)

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelar()

			oferta, err := banco.Simular(ctx, c.pedido)
			if err != nil {
				t.Fatalf("Simular ao vivo: %v", err)
			}
			if oferta.TAN == nil || oferta.TAEG == nil || oferta.Prestacao == nil || oferta.MTIC == nil {
				t.Fatalf("o Novo Banco respondeu sem TAN, TAEG, prestação ou MTIC: %+v", oferta)
			}
			if temFases := len(oferta.Fases) > 0; temFases != c.temFases {
				t.Errorf("fases: esperava tê-las=%v, veio %+v", c.temFases, oferta.Fases)
			}
			t.Logf("%s: TAN %s, TAEG %s, spread %s, prestação %s, MTIC %s, notas %v",
				c.nome, oferta.TAN, oferta.TAEG, oferta.Spread, oferta.Prestacao, oferta.MTIC, oferta.Notas())
		})
	}
}

// O V159 ao vivo: pede-se um prazo que a idade não permite e confirma-se que o
// banco continua a dizer qual é o máximo — e que o reaplicamos.
//
// ⚠️ É a afirmação que sustenta a decisão de **não** guardarmos uma tabela de
// idades. Se o banco deixar de trazer o máximo dentro do erro, é aqui que se
// descobre.
func TestAoVivoOPrazoMaximoVemDentroDoErro(t *testing.T) {
	ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelar()

	p := pedidoBase()
	p.PrazoAnos = 40 // o titular de 1990 não chega lá: o contrato acabaria depois dos 75

	oferta, err := novobanco.Novo(transporte.NovoCliente(nil), nil).Simular(ctx, p)
	if err != nil {
		t.Fatalf("Simular ao vivo: %v", err)
	}

	ajustes := oferta.Ajustes()
	if len(ajustes) != 1 || ajustes[0].Campo() != dominio.AjustadoPrazoAnos {
		t.Fatalf("esperava um ajuste de prazo vindo do erro do banco, vieram %+v", ajustes)
	}
	t.Logf("o banco impôs: %s", ajustes[0].Nota())
}
