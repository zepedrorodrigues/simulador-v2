# simulador-v2

Compara ofertas de crédito à habitação dos bancos portugueses, correndo os
simuladores públicos ao vivo, e publica a série temporal do preçário de mercado.

**Go · PostgreSQL · Redis · chi · pgx/sqlc · OpenAPI.** Serve **apenas JSON**: a
interface é uma app React Native, em repositório à parte.

> **Estado: em planeamento.** Ainda não há código. O desenho está fechado e o
> trabalho está nas issues. Ver [`PLAN.md`](PLAN.md).

Reescrita do [`simulador-credito-habitacao`](../simulador-credito-habitacao)
(Python/FastAPI, 10 bancos, a funcionar). O v1 continua a ser a referência
técnica sobre os bancos — o que se apurou sobre cada um está preservado em
[`docs/DOSSIE-BANCOS.md`](docs/DOSSIE-BANCOS.md).

## Documentos

| documento | o que decide |
|---|---|
| [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md) | camadas, modelo de dados, cache, portões. **Vinculativo.** |
| [`docs/CONTRATO-BANCO.md`](docs/CONTRATO-BANCO.md) | como se acrescenta um banco, e a disciplina de captura primeiro |
| [`docs/DOSSIE-BANCOS.md`](docs/DOSSIE-BANCOS.md) | o que o v1 apurou sobre cada um dos 10 bancos |
| [`docs/API.md`](docs/API.md) | as duas fronteiras HTTP |
| [`docs/ECRAS.md`](docs/ECRAS.md) | os ecrãs da app |
| [`docs/APP.md`](docs/APP.md) | a stack da app e o que ela impõe ao backend |
| [`docs/USO-RESPONSAVEL.md`](docs/USO-RESPONSAVEL.md) | carga nos bancos, dados pessoais, e o que exige parecer |
| [`PLAN.md`](PLAN.md) | as fases e a ordem |
| [`RESUME.md`](RESUME.md) | estado da sessão e próximos passos |

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
| Redis | 8.8 (`redis:8.8-alpine`) | `docker-compose.yml` |
| Docker Compose | v2+ | não fixado — o `make dev` usa `--wait`, que existe desde a v2 |

As quatro ferramentas de geração e lint correm-se **pelo módulo**, não pelo
`PATH`:

```
go tool sqlc generate
go tool goose ...
go tool golangci-lint run ./...
go tool oapi-codegen ...
```

É o que garante que a geração dá o mesmo resultado em qualquer máquina: a
versão vem do `go.mod` e o `go.sum` verifica-a. Um binário do `PATH` não dá essa
garantia — nesta máquina, por exemplo, o `golangci-lint` instalado por
`go install` era o **v1.64.8**, porque o caminho do módulo sem `/v2` só resolve
para o último v1. Correr o lint pelo `PATH` linta com outra ferramenta que não a
do repositório.

O `golangci-lint` v2 tem esquema de configuração próprio, e o `.golangci.yml`
deste repositório está nele (`version: "2"` no topo). Uma configuração v1 seria
ignorada em silêncio.

O portão corre-se por `make verificar` — `go tool golangci-lint run ./...` e
`go test -race ./...`, o repositório todo e não um subconjunto. O alvo `gerar`
entra com o issue que cria os ficheiros de que depende
([#5](../../issues/5), o `sqlc`).

## Ambiente de desenvolvimento

```
make dev      # sobe PostgreSQL e Redis, e espera que respondam de facto
make parar    # pára, guardando os dados
make limpar   # pára e leva o volume do PostgreSQL à frente
```

O `make dev` funciona numa máquina limpa **sem `.env` nenhum** — tudo tem default
no `docker-compose.yml`. O `.env.example` documenta o que se pode afinar, e
cresce com cada issue que traga um sítio novo a ler ambiente.

| serviço | porta | notas |
|---|---|---|
| PostgreSQL | `127.0.0.1:55433` | dados em volume nomeado `pgdata` |
| Redis | `127.0.0.1:56379` | efémero de propósito, sem volume |

⚠️ **Portas fora das habituais, e fora das do v1.** A 5432 costuma estar ocupada
por um PostgreSQL nativo na máquina de desenvolvimento, e o
`simulador-credito-habitacao` publica a 55432 — os dois repositórios têm de poder
estar de pé ao mesmo tempo, que é a situação de quem compara o v2 com o v1. Ambas
publicadas só no *loopback*: um bind em `0.0.0.0` punha a base de dados na
internet no dia em que isto corresse num VPS.

O `make dev` não usa `sleep`: espera pelos healthchecks do compose, que são
literalmente o `pg_isready` e o `redis-cli ping`. ⚠️ O `pg_isready` leva
`-h 127.0.0.1` de propósito — sem ele responde o servidor temporário do `initdb`,
que só escuta no socket unix e é desligado a seguir. A razão está comentada no
`docker-compose.yml`, com a medição.

## Bancos

Nenhum implementado ainda. A ordem está na [fase 1 do plano](PLAN.md#fase-1--a-fatia-vertical):
CGD, Novo Banco, Montepio e Banco CTT primeiro — todos de HTTP puro, sem browser.
Depois Santander e Crédito Agrícola. Por último os quatro que exigem browser
(ActivoBank, Millennium BCP, Bankinter, BPI).

## Fronteiras

- `GET /api/v1/*` — a app. Versionada, evolui connosco.
- `GET /api/rate-catalog` — o `viabilidade-imobiliaria`, autenticado por
  `X-API-Key`. ⚠️ **Congelada e compatível ao byte com o v1**: mudar o formato
  parte o outro repositório.

## Uso responsável

Corre simuladores **públicos** dos bancos, com frequência baixa e cache, e nunca
endpoints de registo de contactos. Os valores são **indicativos** — não são
propostas nem aconselhamento financeiro.
