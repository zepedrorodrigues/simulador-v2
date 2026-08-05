package catalogo

import (
	"testing"
	"time"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// A guarda da §7.3, sem base de dados: a composição só junta bancos do mesmo
// lado da viragem do dia.
//
// ⚠️ Os instantes escrevem-se no fuso de LISBOA e não em UTC, porque é a viragem
// de cá que a §7.3 nomeia. Em Agosto Lisboa está em UTC+1: escrever isto em UTC
// punha as 00:30 no dia anterior e o teste passava a afirmar outra coisa.
func TestAViragemDoDiaSeparaOsBancos(t *testing.T) {
	lx := lisboa()
	ontemTarde := time.Date(2026, 8, 4, 23, 50, 0, 0, lx)
	hojeCedo := time.Date(2026, 8, 5, 0, 10, 0, 0, lx)

	serie := comporSerie([]varrimento.Observacao{
		observacaoEm("cgd", hojeCedo),
		observacaoEm("novobanco", ontemTarde),
		observacaoEm("montepio", hojeCedo),
	})

	if len(serie.Desactualizados) != 1 || serie.Desactualizados[0] != "novobanco" {
		t.Errorf("o banco do outro lado da viragem devia ser o novobanco e sozinho; veio %v",
			serie.Desactualizados)
	}
	for _, o := range serie.Observacoes {
		if o.Oferta.BancoID == "novobanco" {
			t.Error("o novobanco foi varrido antes da viragem do dia e entrou na série servida")
		}
	}
	if len(serie.Observacoes) != 2 {
		t.Errorf("esperavam-se as 2 observações deste lado da viragem, vieram %d", len(serie.Observacoes))
	}
}

// ⚠️ A guarda é sobre coerência ENTRE bancos, e não sobre frescura: uma série
// toda de ontem serve-se toda. Sem isto, a primeira consequência da §7.3 seria
// um 503 todas as manhãs até ao primeiro varrimento do dia — decisão que não
// está tomada (§4).
func TestUmaSerieTodaDeOntemServeSeInteira(t *testing.T) {
	lx := lisboa()
	ontem := time.Date(2026, 8, 4, 3, 0, 0, 0, lx)

	serie := comporSerie([]varrimento.Observacao{
		observacaoEm("cgd", ontem),
		observacaoEm("novobanco", ontem.Add(2*time.Hour)),
	})

	if len(serie.Desactualizados) != 0 {
		t.Errorf("nenhum banco atravessou a viragem e mesmo assim ficaram de fora %v",
			serie.Desactualizados)
	}
	if len(serie.Observacoes) != 2 {
		t.Errorf("esperavam-se 2 observações servíveis, vieram %d", len(serie.Observacoes))
	}
}

func observacaoEm(bancoID string, quando time.Time) varrimento.Observacao {
	return varrimento.Observacao{
		Oferta: dominio.Oferta{BancoID: bancoID, CapturadoEm: quando},
	}
}
