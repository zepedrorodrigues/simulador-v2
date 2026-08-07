// O comando simulador serve a API JSON e os subcomandos de manutenção.
//
// Monta-se aqui o que a infra construir; nenhuma regra de negócio vive neste
// pacote — o depguard nega-lhe os imports de `dominio`, `bancos` e `aplicacao`
// (ARQUITETURA.md §3). Há três subcomandos:
//
//	simulador servir     (o default) — verifica o esquema e recusa-se a
//	                     arrancar com a base por migrar; o servidor HTTP
//	                     ainda não está montado (KAN-13).
//	simulador migrar     aplica as migrações pendentes.
//	simulador reverter   desfaz a última migração.
//
// Todos lêem a base em DATABASE_URL.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/web"
)

func main() {
	if err := executar(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "simulador:", err)
		os.Exit(1)
	}
}

func executar(ctx context.Context, args []string, saida io.Writer) error {
	comando := "servir"
	if len(args) > 0 {
		comando = args[0]
	}

	// ⚠️ O nome do comando verifica-se ANTES de se abrir seja o que for. Hoje o
	// sql.Open é preguiçoso e não liga, portanto a ordem não muda o resultado —
	// mas passaria a mudar no dia em que o Ligar fizesse um Ping, e aí um erro
	// de ligação escondia um erro de digitação. Quem escreveu `varre` perde a
	// tarde a olhar para o Postgres.
	if !slices.Contains(comandos, comando) {
		return fmt.Errorf("comando desconhecido %q (usa: %s)", comando, strings.Join(comandos, " | "))
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("falta DATABASE_URL no ambiente")
	}

	db, err := esquema.Ligar(url)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch comando {
	case "servir":
		// O arranque não migra: verifica e recusa-se a servir se a base
		// estiver por migrar. Aplicar é `simulador migrar`, de propósito.
		if err := esquema.ExigirEmDia(ctx, db); err != nil {
			return err
		}
		return web.Servir(ctx, url, os.Getenv("ENDERECO_HTTP"), saida)
	case "migrar":
		if err := esquema.Migrar(ctx, db); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(saida, "migrações aplicadas")
		return nil
	case "reverter":
		if err := esquema.Reverter(ctx, db); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(saida, "migração revertida")
		return nil
	}
	// Inalcançável: o comando foi verificado contra a lista à cabeça.
	return fmt.Errorf("comando %q sem tratamento", comando)
}

// comandos é a lista única dos subcomandos. Está aqui e não espalhada pelo
// switch para a mensagem de erro não se desactualizar quando entrar o próximo —
// que é como um `usa:` acaba a mentir.
var comandos = []string{"servir", "migrar", "reverter"}

// ⚠️ Os `_, _ =` nos Fprint são deliberados e não preguiça. A saída é o stdout
// de um processo curto: se escrever nele falhar, não há para onde reportar —
// escrever o erro seria escrever no mesmo sítio que acabou de falhar. O
// errcheck isenta o fmt.Println para stdout e não isenta o Fprintln para um
// io.Writer, que é o que este ficheiro usa para os testes poderem ler a saída.
