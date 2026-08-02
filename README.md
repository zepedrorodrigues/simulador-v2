# simulador-v2

Compara ofertas de crédito à habitação dos bancos portugueses e publica a série
temporal do preçário de mercado. Os simuladores públicos dos bancos correm-se 
por varrimento sobre cenários fixos; uma comparação
responde-se por consulta a essa série e **cálculo local**, sem falar com banco
nenhum.

**Go · PostgreSQL · chi · pgx/sqlc · OpenAPI.** Serve **apenas JSON**: a
interface é uma app React Native, em repositório à parte.

> **Estado: em construção.** O lado da escrita corre de ponta a ponta em três
> bancos — domínio, contrato dos bancos, grelha derivada dos requisitos de cada
> um, escala de LTV medida e gravação do lote por `simulador varrer`. Falta o
> lado da leitura: o cálculo local que responde a um cliente, e o servidor HTTP
> que o serve (KAN-13).

## Documentos

O desenho é versionado aqui, ao lado do código.

| documento | o que decide |
|---|---|
| [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md) | as quatro camadas, o modelo de dados, o varrimento e os portões. **Vinculativo** — quando o código diverge dele, um dos dois está errado |
| [`docs/CONTRATO-BANCO.md`](docs/CONTRATO-BANCO.md) | como se acrescenta um banco, e a disciplina de captura antes de código |
| [`docs/DOSSIE-BANCOS.md`](docs/DOSSIE-BANCOS.md) | o que se apurou sobre cada um dos dez bancos |
| [`docs/API.md`](docs/API.md) | as duas fronteiras HTTP e os seus estatutos diferentes |
| [`docs/ECRAS.md`](docs/ECRAS.md) | os ecrãs da app React Native |
| [`docs/APP.md`](docs/APP.md) | a stack da app e o que ela impõe ao servidor |
| [`PLAN.md`](PLAN.md) | as seis fases e a ordem entre elas |
| [`RESUME.md`](RESUME.md) | estado actual e próximos passos |

## Toolchain

| ferramenta | versão | onde está fixada |
|---|---|---|
| Go | 1.26.5 | `go.mod` (`go 1.26.5`) |
| Node | 24.18.0 LTS | aqui — o `openapi-typescript` ainda não entrou |
| `sqlc` | 1.31.1 | `go.mod`, bloco `tool` |
| `goose` | 3.27.3 | `go.mod`, bloco `tool` |
| `golangci-lint` | 2.12.2 | `go.mod`, bloco `tool` |
| `oapi-codegen` | 2.8.0 | `go.mod`, bloco `tool` |
| `make` | GNU make | não fixado — só invoca; em Windows, `winget install ezwinports.make` |
| PostgreSQL | 18.4 (`postgres:18.4-alpine`) | `docker-compose.yml` |
| Docker Compose | v2+ | não fixado — o `make dev` usa `--wait`, que existe desde a v2 |
| Node | 24.18.0 LTS | `.nvmrc` |

As quatro ferramentas de geração e lint correm-se **pelo módulo**, não pelo
`PATH`:

```
go tool sqlc generate
go tool goose ...
go tool golangci-lint run ./...
go tool oapi-codegen ...
```

## Ambiente de desenvolvimento

```
make dev      # sobe o PostgreSQL, e espera que responda de facto
make parar    # pára, guardando os dados
make limpar   # pára e leva o volume do PostgreSQL à frente
```

O `make dev` funciona numa máquina limpa **sem `.env` nenhum** — tudo tem default
no `docker-compose.yml`. O `.env.example` documenta o que se pode afinar, e
cresce com cada issue que traga um sítio novo a ler ambiente.

| serviço | porta | notas |
|---|---|---|
| PostgreSQL | `127.0.0.1:55433` | dados em volume nomeado `pgdata` |

⚠️ **Porta fora das habituais, e fora da do v1.** A 5432 costuma estar ocupada
por um PostgreSQL nativo na máquina de desenvolvimento, e o
`simulador-credito-habitacao` publica a 55432 — os dois repositórios têm de poder
estar de pé ao mesmo tempo, que é a situação de quem compara o v2 com o v1.
Publicada só no *loopback*: um bind em `0.0.0.0` punha a base de dados na
internet no dia em que isto corresse num VPS.

O `make dev` não usa `sleep`: espera pelo healthcheck do compose, que é
literalmente o `pg_isready`. ⚠️ O `pg_isready` leva
`-h 127.0.0.1` de propósito — sem ele responde o servidor temporário do `initdb`,
que só escuta no socket unix e é desligado a seguir. A razão está comentada no
`docker-compose.yml`, com a medição.

## Bancos

| banco | id | transporte | estado |
|---|---|---|---|
| CGD | `cgd` | HTTP simples | implementado, confrontado ao vivo a 2026-07-26 |
| Novo Banco | `novobanco` | HTTP simples | implementado, confrontado ao vivo a 2026-07-26 |
| Montepio | `montepio` | HTTP com sessão | implementado, confrontado ao vivo a 2026-07-27 |
| Banco CTT | `bancoctt` | HTTP simples | implementado, confrontado ao vivo a 2026-07-28 |
| Santander | `santander` | HTTP simples, config em runtime | implementado, confrontado ao vivo a 2026-07-28 |
| Crédito Agrícola | `creditoagricola` | HTTP simples | por fazer |
| ActivoBank, Millennium BCP | — | browser para credencial | fase 3 |
| Bankinter, BPI | — | browser como cliente | fase 3 |

A ordem é a da tabela: primeiro os de HTTP puro, sem browser, e por último os
quatro que exigem browser.

Cada banco corre offline nos testes, contra capturas reais versionadas em
`internal/bancos/<banco>/capturas/`. O teste que bate no simulador a sério está
por trás de `//go:build rede` e **não** corre no portão:

```
go test -race -tags rede ./internal/bancos/cgd/
```

## Fronteiras

- `/api/v1/*` — a app. Versionada, evolui connosco.
  `POST /api/v1/comparacoes` responde **síncrono**, num 200: não há trabalho em
  segundo plano, identificador para sondar nem estado `em_curso`, e nada do
  pedido é persistido — é por isso que não existe
  `GET /api/v1/comparacoes/{id}`.
- `GET /api/rate-catalog` — o `viabilidade-imobiliaria`, autenticado por
  `X-API-Key`. ⚠️ **Congelada e compatível ao byte com o v1**: mudar o formato
  parte o outro repositório.

## Pôr de pé

A imagem é uma só, e serve os quatro subcomandos — `servir`, `varrer`, `migrar`,
`reverter`. Sem Chromium: **24,3 MB**, a correr como `nonroot`, sobre
`distroless/static` (sem shell, sem gestor de pacotes).

```bash
docker build -t simulador-v2 .
docker run --rm -e DATABASE_URL=… simulador-v2 migrar
docker run --rm -p 8080:8080 -e DATABASE_URL=… simulador-v2 servir
```

⚠️ **O `migrar` não é opcional, e o binário obriga.** `servir` contra uma base
por migrar recusa-se, com «base de dados por migrar — corre `simulador migrar`
antes de servir». É a §4 do `ARQUITETURA.md` («não há auto-migração») a valer em
produção: o `migrar` é passo próprio do deploy, seja qual for a plataforma.

### Duas coisas por medir no primeiro deploy

1. **`PROXIES_DE_CONFIANCA`.** Vazio, o tecto por IP conta pelo endereço da
   ligação — que atrás de um proxy é o proxy, **igual para toda a gente**: o tecto do
   site inteiro passa a ser o de um utilizador, e o primeiro visitante tranca os
   restantes. É o bug de produção do v1. O erro contrário é pior: uma rede larga
   de mais deixa quem estiver nela escolher o seu IP num cabeçalho e contornar o
   tecto. Mede-se a rede exacta e escreve-se; até lá fica vazio, porque um tecto
   apertado de mais é visível e um tecto contornável não é.
2. **O primeiro varrimento.** Uma base acabada de migrar não tem série nenhuma, e
   `POST /api/v1/comparacoes` devolve **503** — que é o comportamento certo, e
   não uma avaria. Só depois do primeiro `varrer` é que há o que comparar.

## Uso responsável

É aqui que a postura fica escrita, e fica pública de propósito — uma postura
escondida não é postura. ⚠️ As **perguntas jurídicas** que faltam responder antes
de publicar nas lojas estão na `KAN-24`, com `prioridade-alta`: termos de serviço
dos bancos, redistribuição da série, regulação de crédito, requisitos das lojas,
RGPD e responsabilidade.

- **Sem dados pessoais.** As simulações de utilizador **não são guardadas**: são
  corridas e devolvidas. A única coisa persistida a longo prazo é a série de
  mercado, varrida sobre cenários fixos com um titular neutro e fictício. Não há
  contas, sessões, emails nem histórico por pessoa, e não vai haver.
- **Os valores são indicativos, e não são todos da mesma qualidade.** O spread e
  a taxa da fase fixa são **medidos** — é o que o simulador público de cada banco
  devolveu, na data registada, e essa data viaja em cada oferta. A prestação é
  aritmética exacta sobre eles. Mas a **TAEG e o MTIC são derivados**, não
  medidos: dependem dos encargos, e o seguro de vida depende de quem pede,
  enquanto a série é varrida com um titular neutro e fictício. Cada oferta
  declara em `pressupostos` as hipóteses de que o seu número depende, como o
  Anexo I e o Anexo II da MCD mandam. **Não são propostas, não vinculam o banco e
  não são aconselhamento financeiro.** Uma proposta a sério vem do banco, por
  escrito, depois de avaliar quem a pede.
- **Há perguntas em aberto, e estão assumidas como tal** — nomeadamente as que
  exigem parecer jurídico antes de isto ser publicado numa loja de aplicações.
  Não estão resolvidas por omissão.
