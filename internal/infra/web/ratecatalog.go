package web

import (
	"context"
	"net/http"

	"github.com/zepedrorodrigues/simulador-v2/api"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
)

// O `/api/rate-catalog`, congelado e compatível ao byte com o v1.
//
// ⚠️ **Esta fronteira não é nossa para arrumar.** Os nomes são em inglês e
// snake_case ao contrário de todo o repositório, o `captured_at` vai sem fuso, e
// o `fixed_period_years` é nulo na variável. Nada disto se «corrige»: o
// `viabilidade-imobiliaria` lê isto em produção, e mudar o formato parte-o sem
// aviso (§6).
//
// Três armadilhas, todas medidas contra o v1 a correr a 2026-07-28 e todas
// cobertas pelo teste de contrato:
//
//  1. **números, não strings.** O `shopspring/decimal` serializa com aspas por
//     omissão, e o v1 (Python) envia números. A conversão para float64 é no tipo
//     de saída;
//  2. **`captured_at` sem fuso.** O v1 guardava instantes ingénuos e serializava
//     `2026-07-22T05:00:11`. É `string` e não `date-time` de propósito;
//  3. **`products` é sempre lista, nunca null.** Vazio quer dizer «correu sem
//     bonificações», que é informação — e um consumidor tem de o distinguir de
//     um erro.

// FonteDoCatalogo é a leitura da série de mercado.
type FonteDoCatalogo interface {
	PontosDoCatalogo(ctx context.Context, filtro dominio.FiltroDoCatalogo) ([]dominio.PontoDeMercado, error)
}

// formatoDoInstanteV1 é o que o v1 serializava: ISO sem fuso e sem `Z`.
//
// ⚠️ A base do v2 é `timestamptz` e sabe o fuso; é aqui, à porta, que se deita
// fora — porque é o que o consumidor espera receber. Escrever `Z` no fim parece
// mais correcto e parte o outro repositório.
const formatoDoInstanteV1 = "2006-01-02T15:04:05"

// obterRateCatalog serve a série.
func (s *Servidor) obterRateCatalog(w http.ResponseWriter, r *http.Request) {
	if s.catalogo == nil {
		erro(w, http.StatusServiceUnavailable, "sem_serie",
			"A série de mercado não está disponível neste serviço.")
		return
	}

	filtro := dominio.FiltroDoCatalogo{
		Banco:    r.URL.Query().Get("bank"),
		Cenario:  r.URL.Query().Get("scenario"),
		TipoTaxa: r.URL.Query().Get("rate_type"),
		Desde:    r.URL.Query().Get("since"),
		Limite:   limiteDe(r.URL.Query().Get("limit")),
	}

	pontos, err := s.catalogo.PontosDoCatalogo(r.Context(), filtro)
	if err != nil {
		erro(w, http.StatusInternalServerError, "serie_indisponivel",
			"Não se conseguiu ler a série de mercado.")
		return
	}

	resposta := api.RateCatalog{
		Count:     len(pontos),
		Points:    make([]api.Point, 0, len(pontos)),
		Scenarios: cenariosDeReferencia(),
	}
	for _, p := range pontos {
		resposta.Points = append(resposta.Points, pontoDe(p))
	}
	escrever(w, http.StatusOK, resposta)
}

// pontoDe traduz um ponto de mercado para a forma congelada.
func pontoDe(p dominio.PontoDeMercado) api.Point {
	ponto := api.Point{
		SnapshotId:       p.VarrimentoID,
		CapturedAt:       p.CapturadoEm.UTC().Format(formatoDoInstanteV1),
		ScenarioKey:      p.Cenario,
		BankId:           p.BancoID,
		BankName:         p.BancoNome,
		RateType:         p.TipoTaxa,
		ValorImovel:      float64De(p.ValorImovel.Decimal()),
		Montante:         float64De(p.Montante.Decimal()),
		PrazoAnos:        p.PrazoAnos,
		EuriborIndexante: p.Indexante,
		// ⚠️ Sempre lista, nunca null. Vazio é «correu sem bonificações».
		Products: p.Produtos,
	}
	if ponto.Products == nil {
		ponto.Products = []string{}
	}
	if p.PeriodoFixoAnos != nil {
		anos := *p.PeriodoFixoAnos
		ponto.FixedPeriodYears = &anos
	}
	if p.TAN != nil {
		ponto.Tan = float64De(p.TAN.Decimal())
	}
	if p.TAEG != nil {
		ponto.Taeg = float64De(p.TAEG.Decimal())
	}
	if p.Spread != nil {
		ponto.Spread = float64De(p.Spread.Decimal())
	}
	if p.EuriborValor != nil {
		ponto.EuriborValor = float64De(p.EuriborValor.Decimal())
	}
	if p.Prestacao != nil {
		ponto.PrestacaoMensal = float64De(p.Prestacao.Decimal())
	}
	if p.MTIC != nil {
		ponto.Mtic = float64De(p.MTIC.Decimal())
	}
	return ponto
}

// limiteDe lê o `limit` da query. Fora de sítio ou ausente vale o do v1.
func limiteDe(bruto string) int {
	const omissao, maximo = 2000, 10000
	if bruto == "" {
		return omissao
	}
	n := 0
	for _, r := range bruto {
		if r < '0' || r > '9' {
			return omissao
		}
		n = n*10 + int(r-'0')
		if n > maximo {
			return maximo
		}
	}
	if n < 1 {
		return omissao
	}
	return n
}

// cenariosDeReferencia é a grelha que o v1 publica, e que o consumidor usa para
// saber o que cada `scenario_key` quer dizer.
//
// ⚠️ Está congelada com os valores do v1 — 80 % e 90 % de LTV, mista a 5 anos e
// variável, a 30 anos — e não com a grelha nova, que é muito maior. A §6 diz que
// «os cenários de referência antigos passam a ser um subconjunto da grelha
// nova»: o que aqui se publica é o subconjunto, e não tudo o que se varre.
func cenariosDeReferencia() []api.Scenario {
	cinco := 5
	return []api.Scenario{
		{
			Key: "ltv80_mista_30a", Label: "LTV 80% · mista · 30 anos", Ltv: 80,
			ValorImovel: 250000, Montante: 200000, PrazoAnos: 30,
			RateType: "mista", FixedPeriodYears: &cinco,
		},
		{
			Key: "ltv90_mista_30a", Label: "LTV 90% · mista · 30 anos", Ltv: 90,
			ValorImovel: 250000, Montante: 225000, PrazoAnos: 30,
			RateType: "mista", FixedPeriodYears: &cinco,
		},
		{
			// ⚠️ Nulo, e não zero: na variável não há período fixo. É a
			// diferença que o teste de contrato apanha.
			Key: "ltv80_variavel_30a", Label: "LTV 80% · variável · 30 anos", Ltv: 80,
			ValorImovel: 250000, Montante: 200000, PrazoAnos: 30,
			RateType: "variavel", FixedPeriodYears: nil,
		},
	}
}
