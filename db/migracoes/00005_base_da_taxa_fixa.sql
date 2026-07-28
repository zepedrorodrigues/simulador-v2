-- A base da taxa fixa, em catalogo_taxas (ARQUITETURA.md §4).
--
-- Medido no produto cartesiano da CGD a 2026-07-28, sobre 121 valores de LTV: a
-- TAN da taxa fixa MUDA com o LTV, nas mesmas fronteiras da variável e com a
-- mesma altura de degrau. Subtraindo o spread do degrau, sobra um número
-- constante ao cêntimo por período — 3,500 a 10 anos, 3,650 a 20, 3,900 a 30:
--
--     TAN_fixa(período, ltv) = base(período) + spread(ltv)
--
-- ⚠️ Uma grelha que guardasse a TAN servia o preço do LTV a que a mediu a toda a
-- gente: 0,70 p.p. de erro para quem cai do outro lado dos 68 %.
--
-- ⚠️ E ESTA COLUNA QUASE NÃO EXISTIU. A §4 pesou guardá-la contra derivá-la na
-- leitura, e a base É derivável — é a TAN da linha menos o spread do degrau do
-- mesmo varrimento_id. O que a paga não é o cálculo, que é uma subtracção: é o
-- CONTEXTO. A subtracção precisa do lote inteiro (a escala do banco vive noutras
-- linhas), e quem lê para responder a um cliente lê UMA linha. Sem a coluna, a
-- leitura teria de recarregar os degraus do varrimento a que a linha pertence
-- para poder usá-la — e uma consulta que precisa de outra consulta para não
-- mentir é a definição de um dado que não está onde devia.
--
-- Preenche-se no momento de gravar, onde o lote está todo à mão.
--
-- numeric(6,3) como a `tan` de que sai: é uma taxa em pontos percentuais.
--
-- ⚠️ NULA em tudo o que não seja taxa fixa, e nula também numa linha de taxa
-- fixa cujo varrimento não mediu escala nenhuma. Esse segundo caso é o que
-- interessa perceber: a linha grava-se na mesma — a TAN é um número real que o
-- banco devolveu, e apagá-la seria perder a medição —, mas quem responde a um
-- cliente não tem por onde a corrigir para o LTV dele, e por isso NÃO A SERVE.
-- Nula aqui é «não há por onde corrigir», nunca «a base é zero».

-- +goose Up
ALTER TABLE catalogo_taxas
    ADD COLUMN base_fixa numeric(6,3);

-- Só a taxa fixa tem base. Na variável e na mista o spread é publicado à parte e
-- a identidade fecha com a Euribor — uma base ali seria subtrair duas vezes o
-- mesmo número, e o resultado não descreveria preço nenhum.
ALTER TABLE catalogo_taxas
    ADD CONSTRAINT catalogo_taxas_base_so_na_taxa_fixa
        CHECK (base_fixa IS NULL OR rate_type = 'fixa');

-- +goose Down
ALTER TABLE catalogo_taxas
    DROP CONSTRAINT catalogo_taxas_base_so_na_taxa_fixa,
    DROP COLUMN base_fixa;
