package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestComandoDesconhecidoNomeiaOsQueExistem(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://ninguem@127.0.0.1:1/nada")

	// Um comando errado não pode sair com uma mensagem que obriga a ir ao
	// código descobrir quais são. ⚠️ E tem de reprovar ANTES de tentar ligar-se
	// à base: um erro de ligação a esconder um erro de digitação faz perder
	// tempo a olhar para o Postgres.
	err := executar(context.Background(), []string{"servi"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("um comando desconhecido passou")
	}
	for _, c := range []string{"servir", "migrar", "reverter"} {
		if !strings.Contains(err.Error(), c) {
			t.Errorf("a mensagem não nomeia o subcomando %q: %v", c, err)
		}
	}
}

func TestSemDATABASE_URLRecusaAntesDeSejaOQueFor(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	err := executar(context.Background(), []string{"migrar"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("erro = %v, esperava que nomeasse a variável em falta", err)
	}
}

// ⚠️ **Havia aqui um `TestAListaDeBancosIgnoraEspacosEVazios`**, que guardava o
// `--bancos` do `varrer` contra vírgulas a mais. Saiu com o subcomando (Fase 6,
// passo 5): não há mais nenhuma flag que receba uma lista escrita à mão.
