-- Queries do catálogo de taxas. Geradas pelo sqlc para internal/infra/bd/.

-- InserirTaxa grava uma linha de um banco num varrimento. Os campos de resposta
-- são opcionais: numa falha entram nulos e `erro` preenchido.
--
-- ⚠️ ltv_min, ltv_max e spread_minimo são nulos numa linha que não seja um
-- degrau da escala de LTV — uma observação de um período fixo, de um tenor, de
-- uma finalidade ou de um produto é medida no LTV de referência e não afirma
-- intervalo nenhum (ARQUITETURA.md §4). O LTV dessa observação continua
-- derivável de montante/valor_imovel, que já vão na linha.
-- name: InserirTaxa :one
INSERT INTO catalogo_taxas (
    varrimento_id, capturado_em, cenario, banco_id, banco_nome, rate_type,
    valor_imovel, montante, prazo_anos, fixed_period_years, euribor_indexante,
    tan, taeg, spread, euribor_valor, prestacao_mensal, mtic,
    ltv_min, ltv_max, spread_minimo,
    produtos, aplicado, notas, sucesso, erro
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16, $17,
    $18, $19, $20,
    $21, $22, $23, $24, $25
)
RETURNING id;

-- ListarPontos serve o points[] do /api/rate-catalog. Os filtros são todos
-- opcionais (nulo = não filtra); `limite` nulo devolve tudo (LIMIT NULL no PG).
-- Só linhas bem-sucedidas: o consumidor lê tan/spread/euribor, ausentes em falha.
-- name: ListarPontos :many
SELECT
    varrimento_id, capturado_em, cenario, banco_id, banco_nome, rate_type,
    valor_imovel, montante, prazo_anos, fixed_period_years, euribor_indexante,
    tan, taeg, spread, euribor_valor, prestacao_mensal, mtic, produtos
FROM catalogo_taxas
WHERE sucesso
  AND (sqlc.narg('cenario')::text      IS NULL OR cenario   = sqlc.narg('cenario'))
  AND (sqlc.narg('banco_id')::text     IS NULL OR banco_id  = sqlc.narg('banco_id'))
  AND (sqlc.narg('rate_type')::text    IS NULL OR rate_type = sqlc.narg('rate_type'))
  AND (sqlc.narg('desde')::timestamptz IS NULL OR capturado_em >= sqlc.narg('desde'))
ORDER BY capturado_em DESC
LIMIT sqlc.narg('limite')::int;

-- NovoVarrimentoID cunha o id que agrupa as linhas de uma corrida.
--
-- ⚠️ Sai da base e não do processo, e é de propósito: é a base que já é a
-- autoridade sobre o que existe, e assim não entra no go.mod uma dependência
-- de UUID para gerar dezasseis bytes.
-- name: NovoVarrimentoID :one
SELECT gen_random_uuid()::uuid AS varrimento_id;

-- UltimoVarrimentoEm é a guarda de idempotência (`--se-antigo`): diz quando foi
-- a observação mais recente, para não se dispararem varrimentos em cima uns dos
-- outros. Nulo quando nunca se varreu nada.
--
-- ⚠️ Olha para TODAS as linhas e não só para as de sucesso. Um varrimento em que
-- todos os bancos falharam continua a ser um varrimento que já se fez, e
-- repeti-lo já a seguir é bater no banco outra vez pela mesma razão que a guarda
-- existe para evitar. O v1 estragou a primeira medição assim: cinco corridas em
-- 14 minutos não são cinco dias de dados.
-- name: UltimoVarrimentoEm :one
SELECT max(capturado_em)::timestamptz AS capturado_em FROM catalogo_taxas;

-- ListarSnapshots serve o /api/rate-catalog/snapshots: um por varrimento, com a
-- data e a contagem de linhas.
-- name: ListarSnapshots :many
SELECT
    varrimento_id,
    max(capturado_em)::timestamptz AS capturado_em,
    count(*)::bigint               AS linhas
FROM catalogo_taxas
GROUP BY varrimento_id
ORDER BY max(capturado_em) DESC;
