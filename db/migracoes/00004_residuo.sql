-- O resíduo de cada observação, em catalogo_taxas (ARQUITETURA.md §4 e §7.4).
--
-- A §7.4 decidiu a 2026-07-25 que «cada varrimento mede o seu próprio resíduo» e
-- que a diferença **se guarda**, e não dizia onde. A §4 fixou-o a 2026-07-28, em
-- «O resíduo mora numa coluna»: aqui, e não numa tabela nova — o resíduo é uma
-- medição por observação, e uma observação já é uma linha desta tabela.
--
-- O que a coluna contém: a prestação que o banco devolveu para a primeira fase
-- MENOS a que a amortização francesa dá sobre o plano que ele próprio descreveu.
--
-- Três decisões, e nenhuma é de estilo:
--
--   · numeric(12,2), como a prestacao_mensal de que sai. Um resíduo é uma
--     diferença entre dois montantes e vive na mesma escala que eles.
--
--   · COM SINAL, e por isso não há CHECK a exigir que seja positivo. Positivo é
--     o banco a cobrar acima da nossa conta, negativo o contrário, e a direcção
--     é metade da informação: um valor absoluto diria que alguma coisa mudou sem
--     dizer para que lado.
--
--   · SEM CHECK de grandeza. Um resíduo grande é precisamente o sinal que esta
--     coluna existe para dar — recusá-lo na escrita seria apagar o aviso para
--     manter a tabela bonita. E a §8 já decidiu que o resíduo não é portão:
--     depende de servidores de terceiros estarem de pé.
--
-- ⚠️ NULO é «não havia o que comparar», nunca «comparou-se e deu zero». São três
-- casos, todos legítimos: a linha é de falha; a oferta veio sem plano de fases
-- (a §5 permite-o — um banco não publica a fase que não descreveu); ou o plano
-- descreve zero meses. Um zero silencioso em qualquer deles leria-se como «bateu
-- ao cêntimo», que é o oposto do que aconteceu.

-- +goose Up
ALTER TABLE catalogo_taxas
    ADD COLUMN residuo_prestacao numeric(12,2);

-- +goose Down
ALTER TABLE catalogo_taxas
    DROP COLUMN residuo_prestacao;
