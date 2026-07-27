package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
)

func TestComandoDesconhecidoNomeiaOsQueExistem(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://ninguem@127.0.0.1:1/nada")

	// Um comando errado não pode sair com uma mensagem que obriga a ir ao
	// código descobrir quais são. ⚠️ E tem de reprovar ANTES de tentar ligar-se
	// à base: um erro de ligação a esconder um erro de digitação faz perder
	// tempo a olhar para o Postgres.
	err := executar(context.Background(), []string{"varre"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("um comando desconhecido passou")
	}
	for _, c := range []string{"servir", "varrer", "migrar", "reverter"} {
		if !strings.Contains(err.Error(), c) {
			t.Errorf("a mensagem não nomeia o subcomando %q: %v", c, err)
		}
	}
}

func TestSemDATABASE_URLRecusaAntesDeSejaOQueFor(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	err := executar(context.Background(), []string{"varrer"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("erro = %v, esperava que nomeasse a variável em falta", err)
	}
}

func TestAListaDeBancosIgnoraEspacosEVazios(t *testing.T) {
	// O `--bancos` escreve-se à mão numa linha de comandos, e uma vírgula a
	// mais ou um espaço depois dela não podem virar um id vazio — que o registo
	// recusaria com um erro sobre "banco desconhecido \"\"", a apontar para o
	// sítio errado.
	for _, caso := range []struct {
		entrada string
		quer    []string
	}{
		{"", nil},
		{"cgd", []string{"cgd"}},
		{"cgd,novobanco", []string{"cgd", "novobanco"}},
		{" cgd , novobanco ", []string{"cgd", "novobanco"}},
		{"cgd,,novobanco,", []string{"cgd", "novobanco"}},
		{",", nil},
	} {
		if got := lista(caso.entrada); !slices.Equal(got, caso.quer) {
			t.Errorf("lista(%q) = %v, esperava %v", caso.entrada, got, caso.quer)
		}
	}
}
