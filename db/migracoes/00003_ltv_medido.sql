-- O intervalo de LTV medido, em catalogo_taxas (ARQUITETURA.md §4).
--
-- A §4 decidiu a 2026-07-26 (KAN-35) que o spread se guarda por INTERVALO de
-- LTV com fronteiras medidas, e não por banda de passo fixo — e a 00001, de
-- antes dessa decisão, não tem onde as pôr. Isto fecha essa divergência entre o
-- documento vinculativo e o esquema.
--
-- As três colunas descrevem uma coisa só: o degrau da escala de que esta linha
-- é a medição. São NULAS quando a linha não é um degrau — uma observação de um
-- período fixo, de um tenor, de uma finalidade ou de um produto é medida no LTV
-- de referência e não afirma intervalo nenhum. ⚠️ O LTV dessa observação
-- continua derivável: montante e valor_imovel já estão na linha.
--
-- Duas decisões de tipo, e cada uma vem de um número:
--
--   · numeric(9,8) e não numeric(6,3). O LTV é uma FRACÇÃO (o contrato publica
--     ltv: 0.8), e três casas decimais sobre uma fracção arredondam a 0,1 p.p.
--     de LTV — erro máximo de 0,05 p.p., que é o orçamento INTEIRO da
--     tolerância do refinamento. Gastar ~18 pedidos por banco a estreitar uma
--     fronteira e desfazê-lo na gravação não é guardar o que se mediu. Medido a
--     2026-07-27 na forma da CGD: as fronteiras têm até sete casas — 0,3325,
--     0,6659375, 0,67875 — e a segunda, em numeric(6,3), ficava 0,666.
--
--   · spread_minimo em vez do ltv_resolvido bool que a §4 declarava. É o que o
--     dominio.DegrauLTV faz, e pela mesma razão: com uma bandeira, «não
--     resolvido» e «sem o outro lado» são estados independentes e o par
--     impossível é representável. E sem os dois números a nota obrigatória não
--     se reconstrói da linha — ela nomeia o spread servido E o que se
--     descartou. O resolvido deriva-se de spread_minimo IS NULL.

-- +goose Up
ALTER TABLE catalogo_taxas
    ADD COLUMN ltv_min       numeric(9,8),
    ADD COLUMN ltv_max       numeric(9,8),
    ADD COLUMN spread_minimo numeric(6,3);

-- Um intervalo é os dois extremos ou nenhum. Meia coluna preenchida seria um
-- degrau sem fim, e ninguém saberia até onde o spread vale.
ALTER TABLE catalogo_taxas
    ADD CONSTRAINT catalogo_taxas_intervalo_de_ltv_inteiro
        CHECK ((ltv_min IS NULL) = (ltv_max IS NULL));

-- Existindo, é um intervalo e não um ponto nem um disparate. O tecto de 2 dá
-- folga a um LTV acima de 100 % (a CGD financia até 100 % com garantia pública)
-- sem deixar passar um valor que ficou em pontos percentuais por engano — que é
-- o erro que os tipos do dominio existem para tornar impossível em Go, e que
-- aqui só um CHECK apanha.
ALTER TABLE catalogo_taxas
    ADD CONSTRAINT catalogo_taxas_intervalo_de_ltv_crescente
        CHECK (ltv_min IS NULL OR (ltv_min >= 0 AND ltv_min < ltv_max AND ltv_max <= 2));

-- ⚠️ O spread_minimo é o lado BARATO de um degrau por resolver, e o spread
-- servido é o caro. Ao contrário serve-se em silêncio o preço mais favorável
-- num intervalo onde não se sabe qual é — que é o oposto do que o Anexo I,
-- Parte II, alínea (d) da Directiva 2014/17/UE manda. É a mesma guarda que o
-- dominio.NovaEscalaDeLTV faz em Go; aqui está para o caso de a linha chegar
-- por outro caminho.
ALTER TABLE catalogo_taxas
    ADD CONSTRAINT catalogo_taxas_spread_minimo_e_o_lado_barato
        CHECK (spread_minimo IS NULL OR (
            ltv_min       IS NOT NULL AND
            spread        IS NOT NULL AND
            spread_minimo <  spread
        ));

-- A consulta que responde a um cliente deixou de ser uma igualdade em LTV e
-- passou a ser «o intervalo que contém o meu» (§4).
--
-- ⚠️ O índice de 00001 fica ao lado deste e não é substituído: não é prefixo
-- dele — o ltv_min entra ao meio — e é ele que serve o /api/rate-catalog, que
-- não filtra por LTV. Numa tabela escrita uma vez por varrimento, o custo de
-- escrita de um índice a mais não é argumento.
CREATE INDEX catalogo_taxas_cenario_banco_ltv_idx
    ON catalogo_taxas (cenario, banco_id, ltv_min, capturado_em DESC);

-- +goose Down
DROP INDEX catalogo_taxas_cenario_banco_ltv_idx;

ALTER TABLE catalogo_taxas
    DROP CONSTRAINT catalogo_taxas_spread_minimo_e_o_lado_barato,
    DROP CONSTRAINT catalogo_taxas_intervalo_de_ltv_crescente,
    DROP CONSTRAINT catalogo_taxas_intervalo_de_ltv_inteiro,
    DROP COLUMN spread_minimo,
    DROP COLUMN ltv_max,
    DROP COLUMN ltv_min;
