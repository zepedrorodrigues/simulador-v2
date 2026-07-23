-- limites — tecto de pedidos por IP (ARQUITETURA.md §4).
--
-- O site é público e cada submissão custa dezenas de segundos de scraping a
-- partir do NOSSO IP contra os bancos. A tabela tem nome próprio — e não o de
-- uma funcionalidade apagada, como o `auth_throttle` do v1.
--
-- ⚠️ Isto é o tecto persistente por chave; o gate por banco e a cache vivem em
-- Redis (§7), não aqui.

-- +goose Up
CREATE TABLE limites (
    chave         text        PRIMARY KEY,       -- ex.: sim:ip:1.2.3.4
    janela_inicio timestamptz NOT NULL,
    contagem      integer     NOT NULL DEFAULT 0,
    bloqueado_ate timestamptz                     -- nulo enquanto não bloqueado
);

-- +goose Down
DROP TABLE limites;
