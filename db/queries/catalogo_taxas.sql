-- Queries do catálogo de taxas. Geradas pelo sqlc para internal/infra/bd/.

-- InserirTaxa grava uma linha de um banco num varrimento. Os campos de resposta
-- são opcionais: numa falha entram nulos e `erro` preenchido.
-- name: InserirTaxa :one
INSERT INTO catalogo_taxas (
    varrimento_id, capturado_em, cenario, banco_id, banco_nome, rate_type,
    valor_imovel, montante, prazo_anos, fixed_period_years, euribor_indexante,
    tan, taeg, spread, euribor_valor, prestacao_mensal, mtic,
    produtos, aplicado, notas, sucesso, erro
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16, $17,
    $18, $19, $20, $21, $22
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
