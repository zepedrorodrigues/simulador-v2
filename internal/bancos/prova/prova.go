// Package prova tem a afirmação que o teste de cada banco repete: que Simular
// respeita o prazo do ctx.
//
// Vive em pacote próprio, e não num ficheiro _test.go, porque quem precisa dela
// são os testes de cada banco — que estão noutro pacote. É a mesma razão por que
// a stdlib tem o net/http/httptest.
package prova

import (
	"context"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// Folga é quanto se deixa passar para lá do prazo antes de dar o banco como
// surdo ao ctx. Não é zero de propósito: o escalonador do Go não devolve ao
// microssegundo, e um teste que reprove por causa disso é um teste que aborrece
// em vez de apanhar. Meio segundo é folgado face aos prazos de dezenas de
// segundos que o orquestrador impõe, e curto face ao atraso do transporte falso.
const Folga = 500 * time.Millisecond

// RespeitaPrazo afirma que b.Simular volta dentro do prazo do ctx.
//
// ⚠️ Chama-se com o banco ligado a um transporte lento — um transporte.Falso com
// Atraso bem maior do que o prazo. Sem isso, o teste passa por não haver nada
// que demore, e um teste que nunca se viu falhar não prova nada.
//
// O erro devolvido pelo banco não interessa aqui: um banco que desiste porque o
// prazo acabou devolve erro, e é o comportamento certo. O que se mede é o tempo.
func RespeitaPrazo(t *testing.T, b bancos.Banco, p dominio.Pedido, prazo time.Duration) {
	t.Helper()

	ctx, cancelar := context.WithTimeout(t.Context(), prazo)
	defer cancelar()

	feito := make(chan struct{})
	inicio := time.Now()
	go func() {
		defer close(feito)
		_, _ = b.Simular(ctx, p)
	}()

	select {
	case <-feito:
		if demorou := time.Since(inicio); demorou > prazo+Folga {
			t.Fatalf("%s: Simular voltou ao fim de %s, com um prazo de %s", b.ID(), demorou, prazo)
		}
	case <-time.After(prazo + Folga):
		t.Fatalf(
			"%s: Simular não voltou dentro do prazo de %s — ignora o ctx, e um banco assim segura a comparação inteira",
			b.ID(), prazo,
		)
	}
}
