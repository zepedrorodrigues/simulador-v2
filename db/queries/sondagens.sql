-- Queries das sondagens. Geradas pelo sqlc para internal/infra/bd/.

-- GravarSondagem regista o que uma corrida da sonda apurou sobre um banco.
--
-- ⚠️ Grava-se SEMPRE, e não só na divergência. Uma sondagem que confirma é o que
-- distingue «confirmada» de «ninguém olhou», e são estados diferentes para quem
-- lê a oferta (ARQUITETURA.md §4, «A fiabilidade de um banco deriva-se»).
-- name: GravarSondagem :one
INSERT INTO sondagens (banco_id, sondado_em, degraus, divergentes, cegos)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- UltimaSondagemDeCadaBanco devolve, por banco, a corrida da sonda mais recente.
--
-- ⚠️ Não filtra por «divergiu». A fiabilidade deriva-se de comparar esta data com
-- a do último varrimento do banco, e uma sondagem que confirmou é tão necessária
-- a essa conta como uma que divergiu — sem ela, um banco confirmado ficava
-- indistinguível de um que ninguém sondou.
-- name: UltimaSondagemDeCadaBanco :many
SELECT DISTINCT ON (banco_id)
    banco_id, sondado_em, degraus, divergentes, cegos
FROM sondagens
ORDER BY banco_id, sondado_em DESC;
