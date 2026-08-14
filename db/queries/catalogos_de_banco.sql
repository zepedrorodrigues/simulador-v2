-- Queries dos catálogos que os bancos publicam (ARQUITETURA.md §4, tabela
-- `catalogos_de_banco`, KAN-36).

-- LerCatalogo devolve o catálogo guardado, se ainda for válido.
--
-- ⚠️ **A validade filtra-se na leitura, e não só na limpeza** — a mesma razão do
-- `LerRespostaEmCache`: a limpeza corre por manutenção e pode não ter corrido, e
-- servir um catálogo expirado é precisamente o erro que ele pode causar. Aqui
-- esse erro tem forma própria: um período que o banco deixou de praticar seria
-- proposto a alguém como se fosse de hoje.
-- name: LerCatalogo :one
SELECT valor, lido_em
FROM catalogos_de_banco
WHERE banco_id = @banco_id
  AND nome = @nome
  AND expira_em > @agora::timestamptz;

-- GravarCatalogo guarda o catálogo e estende a validade.
--
-- ⚠️ `ON CONFLICT DO UPDATE`, como na cache: duas leituras para o mesmo catálogo
-- são normais — expirou, ou dois clientes chegaram juntos e ambos foram à página.
-- A última é a mais fresca e é ela que fica. Com `DO NOTHING`, uma entrada a
-- expirar nunca se renovava e voltava-se a pedir a página a cada pedido, que é o
-- defeito que esta tabela existe para corrigir.
-- name: GravarCatalogo :exec
INSERT INTO catalogos_de_banco (banco_id, nome, valor, lido_em, expira_em)
VALUES (@banco_id, @nome, @valor, @lido_em::timestamptz, @expira_em::timestamptz)
ON CONFLICT (banco_id, nome) DO UPDATE SET
    valor     = EXCLUDED.valor,
    lido_em   = EXCLUDED.lido_em,
    expira_em = EXCLUDED.expira_em;

-- LimparCatalogosExpirados apaga o que já não se pode usar.
--
-- ⚠️ Por manutenção e não no caminho de um cliente, como as outras duas limpezas.
-- name: LimparCatalogosExpirados :execrows
DELETE FROM catalogos_de_banco
WHERE expira_em <= @agora::timestamptz;
