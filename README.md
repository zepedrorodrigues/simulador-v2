# simulador-v2

Compara ofertas de crédito à habitação dos bancos portugueses, correndo os
simuladores públicos ao vivo, e publica a série temporal do preçário de mercado.

**Go · PostgreSQL · Redis · chi · pgx/sqlc · OpenAPI.** Serve **apenas JSON**: a
interface é uma app React Native, em repositório à parte.

> **Estado: em planeamento.** Ainda não há código de negócio — só as fundações.

Reescrita do [`simulador-credito-habitacao`](../simulador-credito-habitacao)
(Python/FastAPI, 10 bancos, a funcionar). O v1 continua a ser a referência
técnica sobre os bancos.

## Documentos

⚠️ **O desenho não é versionado neste repositório.** Vive num espaço Confluence
privado. É decisão, não esquecimento: o código é aberto de propósito, o
planeamento não.

| documento | o que decide |
|---|---|
| `ARQUITETURA` | as quatro camadas, o modelo de dados, a política de cache e os portões. **Vinculativo** — quando o código diverge dele, um dos dois está errado |
| `CONTRATO-BANCO` | como se acrescenta um banco, e a disciplina de captura antes de código |
| `DOSSIE-BANCOS` | o que se apurou sobre cada um dos dez bancos |
| `API` | as duas fronteiras HTTP e os seus estatutos diferentes |
| `ECRAS` | os ecrãs da app React Native |
| `APP` | a stack da app e o que ela impõe ao servidor |
| `USO-RESPONSAVEL` | carga nos bancos, dados pessoais, e o que exige parecer jurídico |
| `PLAN` | as seis fases e a ordem entre elas |
| `RESUME` | estado actual e próximos passos |

**Pedir acesso:** abre uma [issue](../../issues) a dizer quem és e para que
precisas, ou fala com o dono do repositório
([@zepedrorodrigues](https://github.com/zepedrorodrigues)). É dado caso a caso.

Não precisas de acesso nenhum para duas coisas. **Pôr isto a correr localmente:**
está tudo abaixo. **Perceber porque é que o portão te reprovou:** as mensagens do
`depguard` enunciam a regra violada, sem remeter para documento nenhum.

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

Nenhum implementado ainda. A ordem é: CGD, Novo Banco, Montepio e Banco CTT
primeiro — todos de HTTP puro, sem browser. Depois Santander e Crédito Agrícola.
Por último os quatro que exigem browser (ActivoBank, Millennium BCP, Bankinter,
BPI).

## Fronteiras

- `GET /api/v1/*` — a app. Versionada, evolui connosco.
- `GET /api/rate-catalog` — o `viabilidade-imobiliaria`, autenticado por
  `X-API-Key`. ⚠️ **Congelada e compatível ao byte com o v1**: mudar o formato
  parte o outro repositório.

## Uso responsável

O documento completo é interno; esta parte fica pública de propósito, porque uma
postura escondida não é postura.

- **Só simuladores públicos.** O servidor interroga os simuladores de crédito que
  os bancos publicam nos seus sites. **Nunca** endpoints de registo de contactos,
  de marcação ou de envio de propostas — não se geram *leads* nem se entrega o
  que quer que seja a um banco em nome de ninguém.
- **Frequência baixa, e cache a sério.** Cada comparação custa dezenas de
  segundos de trabalho aos servidores dos bancos, e parte do nosso IP. Por isso
  há cache com validade ajustada ao custo de cada banco, um *gate* que impede o
  mesmo banco de ser interrogado em paralelo, e um tecto de pedidos por IP. Não é
  só protecção nossa — é não ser um peso para quem não pediu nada.
- **Sem dados pessoais.** As simulações de utilizador **não são guardadas**: são
  corridas e devolvidas. A única coisa persistida a longo prazo é a série de
  mercado, varrida sobre cenários fixos com um titular neutro e fictício. Não há
  contas, sessões, emails nem histórico por pessoa, e não vai haver.
- **Os valores são indicativos.** São o que o simulador público de cada banco
  devolveu, na data registada. **Não são propostas, não vinculam o banco e não
  são aconselhamento financeiro.** Uma proposta a sério vem do banco, por escrito,
  depois de avaliar quem a pede.
- **Há perguntas em aberto, e estão assumidas como tal** — nomeadamente as que
  exigem parecer jurídico antes de isto ser publicado numa loja de aplicações.
  Não estão resolvidas por omissão.
