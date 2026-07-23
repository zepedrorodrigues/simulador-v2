// O comando simulador serve a API JSON e os subcomandos de manutenção.
//
// Monta-se aqui o que a infra construir; nenhuma regra de negócio vive neste
// pacote. Por agora há três subcomandos:
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
	"os"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
)

func main() {
	if err := executar(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "simulador:", err)
		os.Exit(1)
	}
}

func executar(ctx context.Context, args []string) error {
	comando := "servir"
	if len(args) > 0 {
		comando = args[0]
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
		fmt.Println("esquema em dia; servidor HTTP ainda não montado (KAN-13)")
		return nil
	case "migrar":
		if err := esquema.Migrar(ctx, db); err != nil {
			return err
		}
		fmt.Println("migrações aplicadas")
		return nil
	case "reverter":
		if err := esquema.Reverter(ctx, db); err != nil {
			return err
		}
		fmt.Println("migração revertida")
		return nil
	default:
		return fmt.Errorf("comando desconhecido %q (usa: servir | migrar | reverter)", comando)
	}
}
