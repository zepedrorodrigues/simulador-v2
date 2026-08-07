-- O varrimento morre, e leva as suas tabelas (ARQUITETURA.md §4, D1 da
-- DECISAO-AO-VIVO.md; Fase 6, passo 5).
--
-- ⚠️ **Isto é o fim de um desenho, e não uma limpeza.** A `catalogo_taxas` era a
-- série de preçário varrido de que saía toda a resposta ao cliente; a
-- `sondagens` era o veredicto da sonda sobre essa série. As duas ficaram órfãs
-- quando o pedido do cliente voltou a ir ao banco (2026-08-06), e caem agora que
-- o caminho ao vivo está de pé — não antes, porque apagar primeiro deixava o
-- repositório sem nada que respondesse.
--
-- ⚠️ **O `/api/rate-catalog` sai com elas, e a D1 fica resolvida assim:** o
-- `viabilidade-imobiliaria` consome o `/api/rate-catalog` do **v1**
-- (`simulador-credito-habitacao`, o `chmonitor`), que mantém os seus próprios
-- scrapers e não é tocado por isto. A rota daqui era uma reimplementação
-- congelada que **nunca teve consumidor** — este serviço não está alojado. Ver a
-- nota na §4.
--
-- ⚠️ **O Down recria as tabelas vazias, e não os dados.** Uma migração de
-- remoção não guarda o que apaga: quem precisar da série tem de a repor de um
-- `pg_dump`. É a razão de este ficheiro existir como passo próprio e datado, em
-- vez de as tabelas desaparecerem dentro de outra mudança.

-- +goose Up
DROP TABLE IF EXISTS sondagens;
DROP TABLE IF EXISTS catalogo_taxas;

-- +goose Down
-- ⚠️ Recria a FORMA e não o conteúdo — ver o aviso acima. As colunas são as que
-- a 00001 a 00006 tinham deixado, para um `down` seguido de `up` não partir.
CREATE TABLE catalogo_taxas (
    id                bigserial   PRIMARY KEY,
    varrimento_id     text        NOT NULL,
    capturado_em      timestamptz NOT NULL,
    banco_id          text        NOT NULL,
    banco_nome        text        NOT NULL,
    cenario           text        NOT NULL,
    prazo_anos        integer     NOT NULL,
    rate_type         text        NOT NULL,
    tan               numeric(6,3),
    taeg              numeric(6,3),
    spread            numeric(6,3),
    euribor_valor     numeric(6,3),
    euribor_indexante text,
    prestacao         numeric(12,2),
    mtic              numeric(14,2),
    produtos          jsonb       NOT NULL DEFAULT '[]',
    ltv_min           numeric(9,8),
    ltv_max           numeric(9,8),
    spread_minimo     numeric(6,3),
    residuo_prestacao numeric(12,2),
    base_fixa         numeric(6,3)
);

CREATE TABLE sondagens (
    id           bigserial   PRIMARY KEY,
    sondado_em   timestamptz NOT NULL,
    banco_id     text        NOT NULL,
    cenario      text        NOT NULL,
    prazo_anos   integer     NOT NULL,
    rate_type    text        NOT NULL,
    concordou    boolean     NOT NULL,
    tan_sondada  numeric(6,3),
    tan_guardada numeric(6,3)
);
