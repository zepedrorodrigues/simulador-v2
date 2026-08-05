-- sondagens — o veredicto da sonda (ARQUITETURA.md §4, KAN-49).
--
-- Uma linha = um banco × uma corrida da sonda. É a TERCEIRA tabela, e a §4 tem
-- a entrada que a autoriza: um veredicto de sonda não existe em lado nenhum e
-- não é cópia de nada. O grão é banco × corrida, e não banco × ponto da grelha
-- — numa coluna de `catalogo_taxas` repetia-se em todas as linhas do banco.
--
-- ⚠️ E gravar as LEITURAS da sonda em `catalogo_taxas` seria pior do que feio.
-- Uma leitura de sonda parece uma observação — mede um spread num LTV, como o
-- varrimento —, e entraria na composição da série servida: uma sonda de 4
-- pontos passava a ser o «varrimento mais recente» daquele banco, em vez de um
-- de 96. O banco ficava com uma grelha de quatro pontos e nada dava erro.
--
-- ⚠️ Uma linha por corrida, e o histórico fica. Um banco que diverge três
-- sondas seguidas é notícia diferente de um que divergiu uma vez, e um UPDATE
-- sobre uma linha única apagava essa distinção sem ninguém pedir.

-- +goose Up
CREATE TABLE sondagens (
    id          bigserial   PRIMARY KEY,
    banco_id    text        NOT NULL,
    sondado_em  timestamptz NOT NULL,          -- COM fuso, como tudo nesta base

    -- O que a corrida apurou. Contagens e não uma bandeira: «divergiu» é
    -- `divergentes > 0`, e guardar só a bandeira perdia quantos degraus mexeram
    -- — que é a diferença entre um preçário retocado e um refeito.
    degraus     integer     NOT NULL,
    divergentes integer     NOT NULL,
    cegos       integer     NOT NULL,

    -- Uma sondagem visita pelo menos um degrau: sem degraus não há o que
    -- confirmar, e o `sonda.PontosDe` recusa-se antes de chegar aqui.
    CONSTRAINT sondagens_visitou_alguma_coisa
        CHECK (degraus > 0),

    -- Divergentes e cegos são subconjuntos dos degraus visitados, e nenhum é
    -- negativo. Um par impossível aqui seria um relatório mal lido.
    CONSTRAINT sondagens_contagens_cabem_nos_degraus
        CHECK (divergentes >= 0 AND cegos >= 0 AND divergentes + cegos <= degraus)
);

-- A leitura é sempre «a última sondagem de cada banco».
CREATE INDEX sondagens_banco_sondado_idx
    ON sondagens (banco_id, sondado_em DESC);

-- +goose Down
DROP TABLE sondagens;
