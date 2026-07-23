-- catalogo_taxas — a série de mercado (ARQUITETURA.md §4).
--
-- Uma linha = um banco × um cenário de referência, num varrimento. É a única
-- coisa guardada a longo prazo, e não contém dados pessoais: os cenários são
-- fixos e o titular é neutro.
--
-- As decisões de tipo são vinculativas e cada uma vem de uma dor do v1:
--   · numeric, nunca float — erros de vírgula flutuante em comparações de
--     cêntimos aparecem como diferenças de 0,01 € que ninguém explica.
--   · timestamptz, nunca instante ingénuo — o v1 guardava sem fuso e as
--     comparações partiam-se ao trocar utcnow() por datetime.now(UTC).
--   · jsonb, nunca text com JSON dentro.

-- +goose Up
CREATE TABLE catalogo_taxas (
    id                 bigserial      PRIMARY KEY,

    -- Identidade da corrida e do cenário. Conhecidos sempre, mesmo em falha.
    varrimento_id      uuid           NOT NULL,          -- agrupa as linhas da mesma corrida
    capturado_em       timestamptz    NOT NULL,          -- COM fuso, deliberadamente
    cenario            text           NOT NULL,          -- chave da grelha de referência
    banco_id           text           NOT NULL,
    banco_nome         text           NOT NULL,
    rate_type          text           NOT NULL,          -- variavel | fixa | mista

    -- Parâmetros do pedido de referência. numeric, não float.
    valor_imovel       numeric(12,2)  NOT NULL,
    montante           numeric(12,2)  NOT NULL,
    prazo_anos         integer        NOT NULL,
    fixed_period_years integer,                           -- nulo em taxa variável pura
    euribor_indexante  text,                              -- nulo quando não se aplica

    -- Resposta do banco. Numa linha com sucesso os campos nucleares estão
    -- sempre presentes — um nulo aqui não é meio-dado, é resposta mal lida, e
    -- isso é uma falha (sucesso = false). Ver os CHECK abaixo.
    tan                numeric(6,3),                      -- pontos percentuais
    taeg               numeric(6,3),
    spread             numeric(6,3),                      -- nulo em taxa fixa pura (sem indexação)
    euribor_valor      numeric(6,3),                      -- idem
    prestacao_mensal   numeric(12,2),
    mtic               numeric(12,2),

    -- Listas/objecto: nunca nulos. Vazio = «nada aplicado» / «sem notas», que é
    -- informação; nulo seria «não sei». O /api/rate-catalog exige `products`
    -- sempre lista, nunca null (API.md §2).
    produtos           jsonb          NOT NULL DEFAULT '[]'::jsonb,  -- ids dos produtos aplicados
    aplicado           jsonb          NOT NULL DEFAULT '{}'::jsonb,  -- o que o banco usou, quando difere do pedido
    notas              jsonb          NOT NULL DEFAULT '[]'::jsonb,  -- avisos legíveis por pessoa

    sucesso            boolean        NOT NULL,
    erro               text,

    -- Uma linha com sucesso traz a resposta nuclear completa. Um nulo aqui não
    -- é meio-dado: é uma resposta mal lida, e isso é uma falha (sucesso=false).
    CONSTRAINT catalogo_taxas_resposta_completa_quando_sucesso
        CHECK (NOT sucesso OR (
            tan              IS NOT NULL AND
            taeg             IS NOT NULL AND
            prestacao_mensal IS NOT NULL AND
            mtic             IS NOT NULL
        )),

    -- Os campos indexados só existem onde há Euribor: variável e mista. Numa
    -- taxa fixa pura não há indexante nem spread sobre ele — ficam nulos.
    CONSTRAINT catalogo_taxas_indexados_quando_ha_euribor
        CHECK (NOT (sucesso AND rate_type IN ('variavel', 'mista')) OR (
            spread            IS NOT NULL AND
            euribor_indexante IS NOT NULL AND
            euribor_valor     IS NOT NULL
        )),

    -- Uma linha falhada nomeia porquê; uma bem-sucedida não traz erro.
    CONSTRAINT catalogo_taxas_erro_quando_falha
        CHECK (sucesso OR erro IS NOT NULL),

    CONSTRAINT catalogo_taxas_rate_type_valido
        CHECK (rate_type IN ('variavel', 'fixa', 'mista'))
);

-- A leitura do /api/rate-catalog: filtra por cenário e banco, ordena pela data.
CREATE INDEX catalogo_taxas_cenario_banco_capturado_idx
    ON catalogo_taxas (cenario, banco_id, capturado_em DESC);

-- A varredura mais recente, transversal a cenários (o endpoint de snapshots).
CREATE INDEX catalogo_taxas_capturado_idx
    ON catalogo_taxas (capturado_em DESC);

-- +goose Down
DROP TABLE catalogo_taxas;
