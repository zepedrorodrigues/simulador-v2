-- Queries da cache do pedido ao vivo (ARQUITETURA.md §4, tabela
-- `respostas_em_cache`).
--
-- ⚠️ A chave que entra aqui já é um resumo: quem a deriva é o
-- `internal/infra/cache`, e o pedido em claro não chega a esta camada.

-- LerRespostaEmCache devolve a resposta guardada, se ainda for válida.
--
-- ⚠️ **A validade filtra-se na leitura, e não só na limpeza.** A limpeza corre
-- por manutenção e pode não ter corrido; se a validade só vivesse lá, uma
-- entrada expirada era servida durante a janela entre expirar e ser apagada — que
-- é precisamente o erro que a cache pode causar. O `expira_em > @agora` é o que
-- garante que uma entrada velha não se serve, tenha a limpeza corrido ou não.
-- name: LerRespostaEmCache :one
SELECT resposta, capturado_em
FROM respostas_em_cache
WHERE chave = @chave
  AND expira_em > @agora::timestamptz;

-- GravarRespostaEmCache guarda a resposta e estende a validade.
--
-- ⚠️ `ON CONFLICT DO UPDATE` e não `DO NOTHING`: duas respostas para a mesma
-- chave são normais — a anterior expirou, ou dois clientes chegaram juntos e
-- ambos foram ao banco. A última resposta é a mais fresca, e é ela que fica.
-- Com `DO NOTHING`, uma entrada a expirar nunca se renovava e a cache deixava de
-- acertar sem nada a dizer porquê.
-- name: GravarRespostaEmCache :exec
INSERT INTO respostas_em_cache (chave, banco_id, resposta, capturado_em, expira_em)
VALUES (@chave, @banco_id, @resposta, @capturado_em::timestamptz, @expira_em::timestamptz)
ON CONFLICT (chave) DO UPDATE SET
    resposta     = EXCLUDED.resposta,
    capturado_em = EXCLUDED.capturado_em,
    expira_em    = EXCLUDED.expira_em;

-- LimparRespostasExpiradas apaga o que já não se pode servir.
--
-- ⚠️ Corre por manutenção e não no caminho de um cliente, pela mesma razão do
-- `LimparLimitesAntigos`: apagar a cada pedido punha uma escrita a mais em cada
-- visita para poupar linhas que a leitura já ignora.
-- name: LimparRespostasExpiradas :execrows
DELETE FROM respostas_em_cache
WHERE expira_em <= @agora::timestamptz;
