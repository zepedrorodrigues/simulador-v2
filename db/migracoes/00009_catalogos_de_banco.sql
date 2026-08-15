-- catalogos_de_banco — o que um banco publica e nós temos de saber para lhe
-- montar o pedido (ARQUITETURA.md §4, KAN-36).
--
-- ⚠️ **É a regra da §4 à letra: guarda-se o PARÂMETRO, não o resultado.** Um
-- catálogo é a lista de opções que o banco publica; não é a resposta a pedido
-- nenhum, e não é de ninguém.
--
-- ⚠️ **O que a obrigou a existir, medido a 2026-08-14:** a CGD publica os
-- períodos de taxa fixa dentro do HTML da página inicial, e o pacote dela lia-os
-- dentro do `Simular`. Por pedido de cliente com fase fixa: 3 pedidos à CGD e
-- 82 333 bytes, dos quais 75 291 (91,4 %) eram a página — para catorze pares
-- `ano → código`.
--
-- ⚠️ **Não tem dados pessoais, e é o que a separa da `respostas_em_cache`.** O
-- que aqui vive é o que o banco mostra a quem visite o site. Por isso a chave é
-- LEGÍVEL — `(banco_id, nome)` — e não um resumo criptográfico: um resumo aqui
-- não protegia nada e tirava a hipótese de olhar para a tabela e perceber o que
-- lá está.

-- +goose Up
CREATE TABLE catalogos_de_banco (
    banco_id   text        NOT NULL,     -- 'cgd'
    nome       text        NOT NULL,     -- 'periodos' — que catálogo é, dentro do banco
    valor      jsonb       NOT NULL,     -- a forma é do banco; só ele a lê e a escreve
    lido_em    timestamptz NOT NULL,     -- quando se FALOU com o banco
    expira_em  timestamptz NOT NULL,
    PRIMARY KEY (banco_id, nome)
);

-- A limpeza varre por validade, como na cache.
CREATE INDEX catalogos_de_banco_expira_em ON catalogos_de_banco (expira_em);

-- +goose Down
DROP TABLE catalogos_de_banco;
