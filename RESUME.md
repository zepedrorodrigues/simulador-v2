# RESUME — simulador-v2

Estado actual e próximos passos. ⚠️ **Sem changelog** — o relato de sessões não
vive aqui. O `RESUME.md` do v1 chegou a 1317 linhas antes de ser esvaziado à
força; este fica curto por construção.

**Actualizado:** 2026-07-22

## Onde estamos

**Planeamento fechado, zero linhas de código.**

O desenho está feito e é vinculativo (`docs/ARQUITETURA.md`). O backlog está nas
issues 1-27. Nada foi implementado.

## O que está decidido

- **Go**, PostgreSQL, Redis, `chi`, `pgx` + `sqlc`, `goose`, OpenAPI escrito à mão
  como fonte da verdade.
- **Serve só JSON.** A interface é uma app Expo/React Native em
  `simulador-v2-app`.
- **Âmbito**: comparar ao vivo + publicar a série de mercado. Mais nada.
- **Fase 1 sem browser nenhum**: CGD, Novo Banco, Montepio, Banco CTT — todos de
  HTTP puro, escolhidos por exercitarem o máximo do contrato ao mínimo custo.
- **Scrapers reescritos de raiz**, com captura primeiro.
- **Duas tabelas.** As simulações de utilizador não são guardadas.

## Próximo passo

**Issue #1 — instalar a toolchain.** ⚠️ Medido nesta máquina: **o Go não está
instalado e o Node também não.** Estão o Docker e o `gh`. Nada arranca antes
disto.

Depois, pela ordem do `PLAN.md`: #2 (esqueleto + `depguard`) → #3 (docker-compose)
→ #4 (migrações) → #6 (openapi.yaml, que desbloqueia a app em paralelo).

## O que está por resolver

- ⚠️ **Bancos de browser (issue #23).** Os quatro bancos que precisam de browser
  são o risco por avaliar do projecto. `playwright-go` é binding da comunidade e
  o Bankinter atravessa Cloudflare com um disfarce específico. A fase 3 começa
  por uma prova de conceito e uma decisão registada, não por código. Saída
  conhecida: serviço à parte.
- ⚠️ **Perguntas jurídicas (issue #27).** Bloqueiam a publicação nas lojas, não a
  fase 1. `docs/USO-RESPONSAVEL.md` §3.
- **Identificar o nosso tráfego** aos bancos: a postura honesta parte o Bankinter,
  que filtra impressões digitais de automação. Decidir por banco, com a razão
  escrita.
- **A app ainda não tem repositório com código** — só plano (`docs/APP.md`).

## Ligação aos outros repositórios

- **`../simulador-credito-habitacao`** (v1) — continua a funcionar e é a
  **referência técnica sobre os bancos**, destilada em `docs/DOSSIE-BANCOS.md`.
  ⚠️ Não é referência de arquitectura: é o que esta reescrita existe para não
  repetir.
- **`../viabilidade-imobiliaria`** — consome `/api/rate-catalog` em produção, hoje
  do v1. Por isso essa fronteira está congelada. ⚠️ **A congelação é interina**:
  esse repositório vai levar o mesmo tratamento, e nessa altura o contrato
  redesenha-se em conjunto.
