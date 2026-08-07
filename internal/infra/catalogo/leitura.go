package catalogo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/comparar"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/grelha"
	"github.com/zepedrorodrigues/simulador-v2/internal/aplicacao/varrimento"
	"github.com/zepedrorodrigues/simulador-v2/internal/dominio"
	"github.com/zepedrorodrigues/simulador-v2/internal/infra/bd"
)

// A leitura do catálogo: de linhas de `catalogo_taxas` para observações com que
// o `aplicacao/comparar` responde.
//
// ⚠️ É o caminho inverso do `linha()`, e a simetria não é completa **de
// propósito**: o que se grava é uma observação, e o que se lê é o que a resposta
// precisa. Três coisas não voltam, e cada uma tem razão:
//
//   - o **plano de fases** não se grava (a §4 não lhe deu coluna). O que dele se
//     precisa é o prazo, e esse deriva-se — ver `varrimento.PrazoAplicado`;
//   - os **titulares** não existem na tabela, e é o ponto inteiro da §4: esta
//     série não tem dados pessoais. A resposta não precisa deles;
//   - as **notas** do varrimento não voltam. Elas descrevem o que aconteceu ao
//     pedido NEUTRO da grelha, e repeti-las a um cliente seria contar-lhe a
//     história de outra simulação.

// ErrSemVarrimento: a base ainda não tem nenhuma corrida gravada.
var ErrSemVarrimento = errors.New("não há varrimento nenhum gravado")

// SerieServivel compõe a série por BANCO — de cada um, o varrimento mais recente
// em que teve sucesso — e não de uma corrida só (§4, KAN-50).
//
// ⚠️ A regra antiga («uma resposta mistura-se de um varrimento só») caiu porque
// deixou de ser cumprível: desde que a sonda revarre sozinha o banco que diverge
// (KAN-48), a corrida mais recente é, com frequência, **um** banco — e servir só
// ela apagava os outros quatro da resposta.
func (p *Postgres) SerieServivel(ctx context.Context) (comparar.Serie, error) {
	q := bd.New(p.pool)

	linhas, err := q.ObservacoesDeCadaBanco(ctx)
	if err != nil {
		return comparar.Serie{}, fmt.Errorf("ler as observações de cada banco: %w", err)
	}
	if len(linhas) == 0 {
		return comparar.Serie{}, ErrSemVarrimento
	}

	obs := make([]varrimento.Observacao, 0, len(linhas))
	for i, l := range linhas {
		o, err := observacaoDe(l)
		if err != nil {
			return comparar.Serie{}, fmt.Errorf("observação %d (%s): %w", i+1, l.BancoID, err)
		}
		obs = append(obs, o)
	}

	sondagens, err := q.UltimaSondagemDeCadaBanco(ctx)
	if err != nil {
		return comparar.Serie{}, fmt.Errorf("ler a última sondagem de cada banco: %w", err)
	}

	serie := comporSerie(obs)
	serie.Fiabilidade = fiabilidadePorBanco(obs, sondagens)
	return serie, nil
}

// fiabilidadePorBanco deriva o que se sabe sobre a grelha de cada banco, de duas
// datas: a da última sondagem e a do varrimento que ela devia ter confirmado
// (ARQUITETURA.md §4, «A fiabilidade de um banco deriva-se»).
//
// ⚠️ Uma sondagem ANTERIOR ao varrimento não diz nada sobre a grelha que está a
// ser servida — ela julgou a anterior. Sem esta comparação, o revarrimento que a
// própria sonda dispara na divergência deixava o banco marcado em dúvida para
// sempre, com a dúvida já resolvida por baixo.
//
// ⚠️ E uma sonda CEGA não confirma nem levanta dúvida: ninguém contradisse a
// grelha, e ninguém a confirmou. É `por_confirmar`, pela mesma razão que o
// `sonda.Relatorio.Confirmada()` exige zero cegos — silêncio não é concordância.
func fiabilidadePorBanco(
	obs []varrimento.Observacao, sondagens []bd.UltimaSondagemDeCadaBancoRow,
) map[string]dominio.Fiabilidade {
	varridoEm := map[string]time.Time{}
	for _, o := range obs {
		if q := o.Oferta.CapturadoEm; q.After(varridoEm[o.Oferta.BancoID]) {
			varridoEm[o.Oferta.BancoID] = q
		}
	}

	fiabilidade := make(map[string]dominio.Fiabilidade, len(sondagens))
	for _, s := range sondagens {
		if !s.SondadoEm.Time.After(varridoEm[s.BancoID]) {
			continue
		}
		if s.Divergentes > 0 {
			fiabilidade[s.BancoID] = dominio.FiabilidadeEmDuvida
			continue
		}
		if s.Cegos > 0 {
			continue
		}
		fiabilidade[s.BancoID] = dominio.FiabilidadeConfirmada
	}
	return fiabilidade
}

// comporSerie aplica a guarda da §7.3: só entram bancos cujo varrimento caia na
// mesma data (fuso de Lisboa) do mais recente que a base tem.
//
// ⚠️ É sobre coerência ENTRE bancos, não sobre frescura. Se ninguém varreu hoje,
// todos estão do mesmo lado da viragem e servem-se todos — o `capturado_em` de
// cada oferta diz de quando são. Recusar servir até haver varrimento do dia é
// outra decisão, e não está tomada (§4).
//
// Em Go e não em SQL porque precisa do fuso, e porque assim testa-se sem base.
func comporSerie(obs []varrimento.Observacao) comparar.Serie {
	maisRecente := time.Time{}
	porBanco := map[string]time.Time{}
	for _, o := range obs {
		q := o.Oferta.CapturadoEm
		if q.After(maisRecente) {
			maisRecente = q
		}
		if q.After(porBanco[o.Oferta.BancoID]) {
			porBanco[o.Oferta.BancoID] = q
		}
	}

	dia := diaDeLisboa(maisRecente)
	desactualizados := make([]string, 0)
	for id, q := range porBanco {
		if diaDeLisboa(q) != dia {
			desactualizados = append(desactualizados, id)
		}
	}

	servivel := make([]varrimento.Observacao, 0, len(obs))
	for _, o := range obs {
		if diaDeLisboa(porBanco[o.Oferta.BancoID]) == dia {
			servivel = append(servivel, o)
		}
	}
	return comparar.Serie{Observacoes: servivel, Desactualizados: ordenados(desactualizados)}
}

// diaDeLisboa é a data civil do instante no fuso onde os bancos praticam preço.
// ⚠️ Não UTC: em Julho, 23:50 de Lisboa é 22:50 UTC do mesmo dia, mas 00:30 de
// Lisboa é 23:30 UTC do dia ANTERIOR — e a viragem que a §7.3 nomeia é a de cá.
func diaDeLisboa(t time.Time) string {
	return t.In(lisboa()).Format("2006-01-02")
}

// lisboa é o fuso onde o preço é praticado. Sem base de dados de fusos (Windows,
// contentor `scratch`) o `LoadLocation` falha, e aí UTC é a única resposta
// honesta — errar a data por uma hora é melhor do que não responder.
func lisboa() *time.Location {
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		return time.UTC
	}
	return loc
}

// ordenados dá ordem estável a uma lista de ids, para a resposta não mudar de
// forma entre pedidos iguais.
func ordenados(ids []string) []string {
	sort.Strings(ids)
	return ids
}

// observacaoDe reconstrói uma observação a partir de uma linha.
func observacaoDe(l bd.ObservacoesDeCadaBancoRow) (varrimento.Observacao, error) {
	cenario, err := grelha.LerCenario(l.Cenario)
	if err != nil {
		// ⚠️ Falha alto e não devolve um ponto plausível. Uma chave que não se
		// reconhece é uma mudança de formato, e servir o que dela se conseguiu
		// ler era o modo de falha da §7.4 — um número certo no cenário errado.
		return varrimento.Observacao{}, fmt.Errorf("cenário ilegível %q: %w", l.Cenario, err)
	}

	pedido := dominio.Pedido{
		ValorImovel: dinheiroDe(l.ValorImovel),
		Montante:    dinheiroDe(l.Montante),
		PrazoAnos:   int(l.PrazoAnos),
		TipoTaxa:    cenario.TipoTaxa,
		Finalidade:  cenario.Finalidade,
		Localizacao: dominio.LocalizacaoContinente,
	}
	if l.FixedPeriodYears.Valid {
		anos := int(l.FixedPeriodYears.Int32)
		pedido.PeriodoFixoAnos = &anos
	}

	produtos, err := listaDeJSON(l.Produtos)
	if err != nil {
		return varrimento.Observacao{}, fmt.Errorf("produtos: %w", err)
	}
	pedido.Produtos = produtos

	oferta := dominio.Oferta{
		BancoID:           l.BancoID,
		BancoNome:         l.BancoNome,
		TAN:               taxaDe(l.Tan),
		TAEG:              taxaDe(l.Taeg),
		Spread:            taxaDe(l.Spread),
		EuriborValor:      taxaDe(l.EuriborValor),
		Prestacao:         montanteDe(l.PrestacaoMensal),
		MTIC:              montanteDe(l.Mtic),
		ProdutosAplicados: produtos,
		CapturadoEm:       l.CapturadoEm.Time,
	}
	if l.EuriborIndexante.Valid {
		oferta.Indexante = dominio.Indexante(l.EuriborIndexante.String)
	}

	// ⚠️ A base da taxa fixa volta como TAN da observação, e não como um campo
	// novo. É o que o `varrimento.BaseDaTaxaFixa` reencontra ao subtrair-lhe o
	// spread do degrau — e é assim que uma observação lida da base e uma
	// observada em memória se comportam da mesma maneira no `comparar`.
	//
	// Sem base gravada, a linha de taxa fixa fica com a TAN medida e o comparar
	// recusa-a: é o que a §4 manda («grava-se, e não se serve»).
	if l.BaseFixa.Valid && pedido.TipoTaxa == dominio.TaxaFixa {
		base := taxaDe(l.BaseFixa)
		spread := taxaDe(l.Spread)
		if base != nil && spread == nil {
			// A linha de fixa não tem spread — é o normal —, por isso a TAN que
			// se reconstrói é a base mais o spread do degrau em que foi medida,
			// e isso reconstitui-se no comparar. Guarda-se a base como TAN e o
			// degrau responde pelo resto.
			oferta.TAN = base
		}
	}

	obs := varrimento.Observacao{
		Ponto:  varrimento.Ponto{Cenario: l.Cenario, Pedido: pedido},
		Oferta: oferta,
	}

	degrau, err := degrauDe(l)
	if err != nil {
		return varrimento.Observacao{}, err
	}
	obs.Degrau = degrau
	return obs, nil
}

// degrauDe reconstrói o intervalo de LTV quando a linha é um degrau da escala.
func degrauDe(l bd.ObservacoesDeCadaBancoRow) (*dominio.DegrauLTV, error) {
	if !l.LtvMin.Valid || !l.LtvMax.Valid {
		return nil, nil
	}
	spread := taxaDe(l.Spread)
	if spread == nil {
		return nil, fmt.Errorf("degrau com intervalo e sem spread — a linha diz sobre que LTV vale um preço que não traz")
	}

	d := &dominio.DegrauLTV{
		De:     racioDe(l.LtvMin),
		Ate:    racioDe(l.LtvMax),
		Spread: *spread,
	}
	if l.SpreadMinimo.Valid {
		minimo := taxaDe(l.SpreadMinimo)
		d.SpreadMinimo = minimo
	}
	return d, nil
}

// --- as conversões da fronteira -------------------------------------------------

func decimalDe(n pgtype.Numeric) decimal.Decimal {
	if !n.Valid || n.Int == nil {
		return decimal.Zero
	}
	return decimal.NewFromBigInt(n.Int, n.Exp)
}

func taxaDe(n pgtype.Numeric) *dominio.Taxa {
	if !n.Valid {
		return nil
	}
	t := dominio.TaxaDeDecimal(decimalDe(n))
	return &t
}

func montanteDe(n pgtype.Numeric) *dominio.Dinheiro {
	if !n.Valid {
		return nil
	}
	d := dominio.DinheiroDeDecimal(decimalDe(n))
	return &d
}

func dinheiroDe(n pgtype.Numeric) dominio.Dinheiro {
	return dominio.DinheiroDeDecimal(decimalDe(n))
}

func racioDe(n pgtype.Numeric) dominio.Racio {
	return dominio.RacioDeDecimal(decimalDe(n))
}

// listaDeJSON lê uma coluna jsonb de strings. ⚠️ Nulo e `[]` dão os dois uma
// lista vazia: a coluna é NOT NULL DEFAULT '[]', e vazio quer dizer «nenhum
// produto», que é informação e não ausência dela.
func listaDeJSON(b []byte) ([]string, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var lista []string
	if err := json.Unmarshal(b, &lista); err != nil {
		return nil, fmt.Errorf("ler a lista %q: %w", string(b), err)
	}
	if len(lista) == 0 {
		return nil, nil
	}
	return lista, nil
}

// PontosDoCatalogo lê a série de mercado que o `/api/rate-catalog` publica.
//
// ⚠️ É a leitura da fronteira CONGELADA, e por isso usa a `ListarPontos` — a
// query antiga, com o subconjunto de colunas que o v1 publicava — e não a
// `ObservacoesDeCadaBanco`. As duas parecem-se e servem coisas diferentes:
// aquela alimenta a resposta ao cliente e precisa da escala e da base; esta
// devolve o que o `viabilidade-imobiliaria` lê há meses.
//
// ⚠️ E a composição por banco da KAN-50 **não lhe toca**: esta fronteira publica
// pontos com o seu `varrimento_id`, e quem a lê filtra como sempre filtrou.
func (p *Postgres) PontosDoCatalogo(
	ctx context.Context, filtro dominio.FiltroDoCatalogo,
) ([]dominio.PontoDeMercado, error) {
	params := bd.ListarPontosParams{Limite: pgtype.Int4{Int32: int32Ou(filtro.Limite), Valid: true}}
	if filtro.Banco != "" {
		params.BancoID = pgtype.Text{String: filtro.Banco, Valid: true}
	}
	if filtro.Cenario != "" {
		params.Cenario = pgtype.Text{String: filtro.Cenario, Valid: true}
	}
	if filtro.TipoTaxa != "" {
		params.RateType = pgtype.Text{String: filtro.TipoTaxa, Valid: true}
	}
	if filtro.Desde != "" {
		// ⚠️ Um `since` que não se percebe é erro de quem pergunta, e sai como
		// erro. Ignorá-lo devolvia a série inteira com ar de resposta filtrada.
		desde, err := time.Parse(time.RFC3339, filtro.Desde)
		if err != nil {
			desde, err = time.Parse("2006-01-02", filtro.Desde)
			if err != nil {
				return nil, fmt.Errorf("`since` inválido %q: usa ISO, por exemplo 2026-07-01", filtro.Desde)
			}
		}
		params.Desde = pgtype.Timestamptz{Time: desde, Valid: true}
	}

	linhas, err := bd.New(p.pool).ListarPontos(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("ler a série de mercado: %w", err)
	}

	pontos := make([]dominio.PontoDeMercado, 0, len(linhas))
	for _, l := range linhas {
		produtos, err := listaDeJSON(l.Produtos)
		if err != nil {
			return nil, fmt.Errorf("produtos do ponto %s/%s: %w", l.BancoID, l.Cenario, err)
		}
		ponto := dominio.PontoDeMercado{
			VarrimentoID: uuidTexto(l.VarrimentoID),
			CapturadoEm:  l.CapturadoEm.Time,
			Cenario:      l.Cenario,
			BancoID:      l.BancoID,
			BancoNome:    l.BancoNome,
			TipoTaxa:     l.RateType,
			ValorImovel:  dinheiroDe(l.ValorImovel),
			Montante:     dinheiroDe(l.Montante),
			PrazoAnos:    int(l.PrazoAnos),
			TAN:          taxaDe(l.Tan),
			TAEG:         taxaDe(l.Taeg),
			Spread:       taxaDe(l.Spread),
			EuriborValor: taxaDe(l.EuriborValor),
			Prestacao:    montanteDe(l.PrestacaoMensal),
			MTIC:         montanteDe(l.Mtic),
			Produtos:     produtos,
		}
		if l.FixedPeriodYears.Valid {
			anos := int(l.FixedPeriodYears.Int32)
			ponto.PeriodoFixoAnos = &anos
		}
		if l.EuriborIndexante.Valid {
			ponto.Indexante = l.EuriborIndexante.String
		}
		pontos = append(pontos, ponto)
	}
	return pontos, nil
}

// int32Ou converte o limite, com o tecto do v1.
func int32Ou(n int) int32 {
	const omissao, maximo = 2000, 10000
	if n < 1 {
		n = omissao
	}
	if n > maximo {
		n = maximo
	}
	return int32(n) //nolint:gosec // preso entre 1 e 10000 acima
}

// SnapshotsDoCatalogo lista os varrimentos: um por corrida, do mais recente
// para o mais antigo.
//
// ⚠️ Sem filtros e sem limite, ao contrário do `PontosDoCatalogo`. São uma linha
// por varrimento — algumas dezenas por ano de operação —, e paginar uma lista
// desse tamanho era desenhar para um consumidor que ainda não existe. Quando a
// tabela crescer ao ponto de isto doer, o contrato ganha os parâmetros e
// discute-se então o formato.
func (p *Postgres) SnapshotsDoCatalogo(ctx context.Context) ([]dominio.Snapshot, error) {
	linhas, err := bd.New(p.pool).ListarSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar os varrimentos: %w", err)
	}

	snapshots := make([]dominio.Snapshot, 0, len(linhas))
	for _, l := range linhas {
		snapshots = append(snapshots, dominio.Snapshot{
			VarrimentoID: uuidTexto(l.VarrimentoID),
			CapturadoEm:  l.CapturadoEm.Time,
			Linhas:       int(l.Linhas),
		})
	}
	return snapshots, nil
}
