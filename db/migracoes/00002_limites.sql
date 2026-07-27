-- limites — tecto de pedidos por IP (ARQUITETURA.md §4).
--
-- O site é público e cada submissão custa dezenas de segundos de scraping a
-- partir do NOSSO IP contra os bancos. A tabela tem nome próprio — e não o de
-- uma funcionalidade apagada, como o `auth_throttle` do v1.
--
-- ⚠️ Isto é o tecto persistente por chave; o travão por banco vive noutro sítio
-- — no `pg_try_advisory_lock` do `internal/infra/travao` —, e cache não há
-- nenhuma. (Dizia aqui «vivem em Redis (§7)»; o Redis saiu do desenho nessa
-- mesma §7 e do ambiente na KAN-39. Só o comentário mudou: o esquema é o mesmo.)

-- +goose Up
CREATE TABLE limites (
    chave         text        PRIMARY KEY,       -- ex.: sim:ip:1.2.3.4
    janela_inicio timestamptz NOT NULL,
    contagem      integer     NOT NULL DEFAULT 0,
    bloqueado_ate timestamptz                     -- nulo enquanto não bloqueado
);

-- +goose Down
DROP TABLE limites;
