//go:build rede

package bpi_test

import (
	"context"
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/bpi"
	"github.com/zepedrorodrigues/simulador-v2/internal/bancos/transporte"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

func TestSimularReal(t *testing.T) {
	if testing.Short() {
		t.Skip("simulação real requer rede e browser")
	}

	// Criar transporte de browser
	browser, err := transporte.NovoBrowserPlaywright()
	if err != nil {
		t.Fatalf("browser: %v", err)
	}
	defer browser.Parar()

	// Criar banco
	banco := bpi.Novo(browser)

	// Criar pedido de teste
	pedido := dominio.Pedido{
		Montante:  dominio.DinheiroDeInteiro(200_000),
		PrazoAnos: 30,
		TipoTaxa:  dominio.TaxaVariavel,
		Titulares: []dominio.Titular{
			{
				DataNascimento: dominio.DataDeInstante(time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)),
			},
		},
	}

	// Executar simulação
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	oferta, err := banco.Simular(ctx, pedido)
	if err != nil {
		t.Fatalf("simular: %v", err)
	}

	// Verificar resultados
	t.Logf("oferta: %+v", oferta)

	if !oferta.Sucesso() {
		t.Errorf("oferta não teve sucesso: %v", oferta.Erro)
	}

	if len(oferta.Fases) == 0 {
		t.Error("oferta não tem fases")
	}
}
