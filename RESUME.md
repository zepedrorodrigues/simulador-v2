# RESUME — simulador-v2

Estado actual e próximos passos. ⚠️ **Sem changelog** — o relato de sessões não
vive aqui. O `RESUME.md` do v1 chegou a 1317 linhas antes de ser esvaziado à
força; este fica curto por construção.

**Actualizado:** 2026-07-22

## Onde estamos

**Esqueleto das quatro camadas de pé, com a regra de dependência imposta. Zero
linhas de código de negócio.**

O desenho está feito e é vinculativo (`docs/ARQUITETURA.md`). O backlog está nas
issues 1-27. O issue #1 está fechado: Go 1.26.5 e Node 24.18.0 instalados, e as
quatro ferramentas de geração fixadas no bloco `tool` do `go.mod` — correm-se por
`go tool …`, nunca pelo `PATH` (ver README, secção Toolchain).

O issue #2 está fechado: `cmd/simulador/` e `internal/{dominio,bancos,aplicacao,
infra}/`, o `.golangci.yml` (esquema v2) com o `depguard` a impor as cinco linhas
de `ARQUITETURA.md` §3, e o `Makefile` com `verificar`. Verificado por reversão —
um `import` proibido em cada camada reprova o portão a nomear a camada de onde
parte.

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

**Issue #3 — `docker-compose` com PostgreSQL e Redis.** O `Makefile` já existe;
o #3 acrescenta-lhe o alvo `dev`. Depois, pela ordem do `PLAN.md`: #4 (migrações)
→ #5 (`sqlc`, que traz o alvo `gerar`) → #6 (openapi.yaml, que desbloqueia a app
em paralelo).

⚠️ Correr o lint por `go tool golangci-lint run ./...` — é o que o `make
verificar` faz. O `golangci-lint` do `PATH` desta máquina é o v1.64.8 e não lê o
`.golangci.yml` v2.

⚠️ O `make` não existia nesta máquina — foi instalado por
`winget install ezwinports.make` (GNU make 4.4.1). Entrou no `PATH` do
utilizador, mas só em shells abertas **depois** da instalação.

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
