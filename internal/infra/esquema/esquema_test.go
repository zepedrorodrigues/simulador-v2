package esquema_test

import (
	"strings"
	"testing"

	"github.com/zepedrorodrigues/simulador-v2/internal/infra/esquema"
)

// A guarda do arranque só presta se a mensagem disser o que fazer: ErrPorMigrar
// tem de nomear o comando que resolve. Teste puro, sem base de dados.
func TestErrPorMigrarNomeiaOComando(t *testing.T) {
	msg := esquema.ErrPorMigrar.Error()
	if !strings.Contains(msg, "simulador migrar") {
		t.Fatalf("ErrPorMigrar devia nomear `simulador migrar`, tem: %q", msg)
	}
}
