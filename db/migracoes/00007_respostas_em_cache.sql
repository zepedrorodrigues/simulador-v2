-- respostas_em_cache — a cache do pedido ao vivo (ARQUITETURA.md §4 e §7.6).
--
-- Somos um amplificador: um pedido de cliente vira ~10 pedidos aos bancos, com
-- origem aparente nossa. Esta tabela é a única defesa que corta essa carga sem
-- cortar funcionalidade — não é optimização de latência.
--
-- ⚠️ **O pedido em claro NÃO vive aqui, e é a razão de a chave ser um resumo.**
-- Um pedido traz data de nascimento e rendimento de alguém; a §4 recusa gravar
-- isso, e é a mesma razão por que o endpoint é POST e não GET. O que se grava é
-- o SHA-256 de (versão, banco, pedido normalizado) — determinístico, para dois
-- clientes iguais partilharem o acerto, e sem volta, para a linha não dizer quem
-- eles são.
--
-- ⚠️ O VALOR é a resposta já traduzida para o contrato, e não o objecto de
-- domínio: assim um acerto é igual a uma resposta fresca por construção. O
-- porquê, com a alternativa que se recusou, está na §4.
--
-- ⚠️ E só entram respostas de SUCESSO. Guardar uma falha transformava um soluço
-- de segundos numa indisponibilidade de toda a validade, para todos os clientes
-- com o mesmo pedido.

-- +goose Up
CREATE TABLE respostas_em_cache (
    chave        text        PRIMARY KEY,  -- SHA-256 hex de (versão, banco, pedido)
    banco_id     text        NOT NULL,     -- para diagnóstico e limpeza; a chave já o inclui
    resposta     jsonb       NOT NULL,     -- a api.Oferta servida, sem dados pessoais
    capturado_em timestamptz NOT NULL,     -- quando se FALOU com o banco, não quando se gravou
    expira_em    timestamptz NOT NULL
);

-- A limpeza varre por validade, não por chave.
CREATE INDEX respostas_em_cache_expira_em ON respostas_em_cache (expira_em);

-- +goose Down
DROP TABLE respostas_em_cache;
