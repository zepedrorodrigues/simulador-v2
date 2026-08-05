-- Queries do catálogo de taxas. Geradas pelo sqlc para internal/infra/bd/.

-- InserirTaxa grava uma linha de um banco num varrimento. Os campos de resposta
-- são opcionais: numa falha entram nulos e `erro` preenchido.
--
-- ⚠️ ltv_min, ltv_max e spread_minimo são nulos numa linha que não seja um
-- degrau da escala de LTV — uma observação de um período fixo, de um tenor, de
-- uma finalidade ou de um produto é medida no LTV de referência e não afirma
-- intervalo nenhum (ARQUITETURA.md §4). O LTV dessa observação continua
-- derivável de montante/valor_imovel, que já vão na linha.
-- ⚠️ residuo_prestacao é nulo onde não havia o que comparar — linha de falha,
-- oferta sem plano de fases, ou plano de zero meses (§4, «O resíduo mora numa
-- coluna»). Nulo não é zero: zero seria uma medição que fechou ao cêntimo.
--
-- ⚠️ base_fixa é nula em tudo o que não seja taxa fixa, e também numa linha de
-- taxa fixa cujo varrimento não mediu escala de LTV nenhuma — aí não há por onde
-- corrigir a TAN para o LTV de quem pergunta, e a linha grava-se como dado bruto
-- mas não se serve (§4, «Onde a base vive»).
-- name: InserirTaxa :one
INSERT INTO catalogo_taxas (
    varrimento_id, capturado_em, cenario, banco_id, banco_nome, rate_type,
    valor_imovel, montante, prazo_anos, fixed_period_years, euribor_indexante,
    tan, taeg, spread, euribor_valor, prestacao_mensal, mtic,
    ltv_min, ltv_max, spread_minimo, residuo_prestacao, base_fixa,
    produtos, aplicado, notas, sucesso, erro
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16, $17,
    $18, $19, $20, $21, $22,
    $23, $24, $25, $26, $27
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

-- ⚠️ Aqui viviam o `UltimoVarrimentoID` e o `ObservacoesDoVarrimento`, que
-- serviam a resposta pelas linhas de UMA corrida. Saíram na KAN-50, e saíram em
-- vez de ficarem sem uso: a leitura por «último varrimento» é exactamente o
-- defeito corrigido, e uma consulta com esse nome à mão é o convite a
-- reintroduzi-lo. Recuperam-se em git.

-- ObservacoesDeCadaBanco devolve, para CADA banco, as linhas do varrimento mais
-- recente em que ele teve sucesso — e não as de uma corrida só (ARQUITETURA.md
-- §4, «Que observações compõem a série servida», KAN-50).
--
-- ⚠️ O `DISTINCT ON` corre sobre linhas de sucesso, e é isso que faz um banco
-- cuja última corrida falhou inteira cair na anterior em vez de desaparecer.
-- Quão velha é essa anterior não se decide aqui: quem chama aplica a guarda da
-- viragem do dia (§7.3), que precisa do fuso de Lisboa e não de SQL.
--
-- Não traz `varrimento_id`: com um por banco, ele deixou de identificar a
-- resposta e guardá-lo convidava a voltar a raciocinar por corrida.
-- name: ObservacoesDeCadaBanco :many
WITH ultimo AS (
    SELECT DISTINCT ON (banco_id) banco_id, varrimento_id
    FROM catalogo_taxas
    WHERE sucesso
    ORDER BY banco_id, capturado_em DESC
)
SELECT
    t.capturado_em, t.cenario, t.banco_id, t.banco_nome, t.rate_type,
    t.valor_imovel, t.montante, t.prazo_anos, t.fixed_period_years, t.euribor_indexante,
    t.tan, t.taeg, t.spread, t.euribor_valor, t.prestacao_mensal, t.mtic,
    t.ltv_min, t.ltv_max, t.spread_minimo, t.base_fixa,
    t.produtos, t.aplicado
FROM catalogo_taxas t
JOIN ultimo u ON t.banco_id = u.banco_id AND t.varrimento_id = u.varrimento_id
WHERE t.sucesso
ORDER BY t.id;
