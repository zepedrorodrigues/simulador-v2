# simulador-v2

Compara ofertas de crédito à habitação dos bancos portugueses. Um pedido de um
cliente **vai ao simulador público de cada banco no momento em que é feito**, e
o que se serve é o que o banco respondeu — não há série guardada, nem grelha,
nem modelo de preço nosso.

**Go · PostgreSQL · chi · pgx/sqlc · OpenAPI.** Serve **apenas JSON**: a
interface é uma app React Native, em repositório à parte.

> **Estado: em construção.** O caminho do cliente está de pé — `POST
> /api/v1/ofertas/{banco}` pergunta a um banco, com tecto de concorrência por
> banco, prazo medido e cache em Postgres. Falta o alojamento.

⚠️ **Isto foi ao contrário e voltou.** Até 2026-08-06 o desenho era o inverso:
varrer os bancos sobre cenários fixos, guardar a série, e responder por cálculo
local sem falar com banco nenhum. Caiu porque guardar uma cópia do modelo de
preço de cada banco obriga a acertar em como eles preçam — e num só dia de
confronto com dados reais falhou-se isso quatro vezes. O porquê, com os números,
está em [`docs/DECISAO-AO-VIVO.md`](docs/DECISAO-AO-VIVO.md).

⚠️ **E o varrimento já não existe** (2026-08-07): saíram com ele a
`catalogo_taxas`, a `sondagens`, a grelha, a sonda e o `/api/rate-catalog` —
14 mil linhas. Quem procurar a série temporal do preçário encontra-a no v1,
[`simulador-credito-habitacao`](https://github.com/zepedrorodrigues/simulador-credito-habitacao),
que continua a servi-la e a ser consumido pelo `viabilidade-imobiliaria`.

## Documentos

O desenho é versionado aqui, ao lado do código.

| documento | o que decide |
|---|---|
| [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md) | as quatro camadas, o modelo de dados, a fatia ao vivo e os portões. **Vinculativo** — quando o código diverge dele, um dos dois está errado |
| [`docs/CONTRATO-BANCO.md`](docs/CONTRATO-BANCO.md) | como se acrescenta um banco, e a disciplina de captura antes de código |
| [`docs/DOSSIE-BANCOS.md`](docs/DOSSIE-BANCOS.md) | o que se apurou sobre cada um dos dez bancos |
| [`docs/API.md`](docs/API.md) | a fronteira HTTP, os códigos de erro e o que cada um distingue |
| [`docs/ECRAS.md`](docs/ECRAS.md) | os ecrãs da app React Native |
| [`docs/APP.md`](docs/APP.md) | a stack da app e o que ela impõe ao servidor |
| [`PLAN.md`](PLAN.md) | as seis fases e a ordem entre elas |
| [`RESUME.md`](RESUME.md) | estado actual e próximos passos |

## Toolchain

| ferramenta | versão | onde está fixada |
|---|---|---|
| Go | 1.26.5 | `go.mod` (`go 1.26.5`) |
| Node | 24.18.0 LTS | `.nvmrc` — só para o `openapi-typescript` do `make gerar` |
| `openapi-typescript` | 7.x | `package-lock.json` |
| `sqlc` | 1.31.1 | `go.mod`, bloco `tool` |
| `goose` | 3.27.3 | `go.mod`, bloco `tool` |
| `golangci-lint` | 2.12.2 | `go.mod`, bloco `tool` |
| `oapi-codegen` | 2.8.0 | `go.mod`, bloco `tool` |
| `make` | GNU make | não fixado — só invoca; em Windows, `winget install ezwinports.make` |
| PostgreSQL | 18.4 (`postgres:18.4-alpine`) | `docker-compose.yml` |
| Docker Compose | v2+ | não fixado — o `make dev` usa `--wait`, que existe desde a v2 |

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
| BPI | `bpi` | browser como cliente (Playwright) | implementado a 2026-08-16, ⚠️ **não servido**: o `servir` não constrói o transporte de browser e a imagem não tem Chromium — não aparece no `/api/v1/bancos` |
| Crédito Agrícola | `creditoagricola` | HTTP simples | por fazer |
| Bankinter | — | browser como cliente | fase 3 |
| ActivoBank, Millennium BCP | — | browser para credencial | fase 3 |

A ordem é a da tabela: primeiro os de HTTP puro, sem browser, e por último os
que exigem browser. ⚠️ Ligar um banco de browser ao `servir` é a decisão da §5
do `ARQUITETURA.md` — Chromium na imagem, ou um serviço à parte — e ainda não
está tomada.

Cada banco corre offline nos testes, contra capturas reais versionadas em
`internal/bancos/<banco>/capturas/`. O teste que bate no simulador a sério está
por trás de `//go:build rede` e **não** corre no portão:

```
go test -race -tags rede ./internal/bancos/cgd/
```

## Fronteiras

- `/api/v1/*` — a app, e **é a única**. Versionada, evolui connosco.
  `POST /api/v1/ofertas/{banco}` pergunta a **um** banco e responde **síncrono**,
  num 200: não há trabalho em segundo plano, identificador para sondar nem estado
  `em_curso`, e nada do pedido é persistido. Quem compara cinco bancos faz cinco
  pedidos — o fan-out vive na app.
- ⚠️ **`POST /api/v1/comparacoes` e `GET /api/rate-catalog` saíram** (2026-08-07),
  com o varrimento que os alimentava. O primeiro comparava a partir da série; o
  segundo servia-a ao `viabilidade-imobiliaria` — que **nunca chegou a consumir
  esta** (lê a do v1) e não é afectado.

## Pôr de pé

A imagem é uma só, e serve os três subcomandos — `servir`, `migrar`,
`reverter`. Sem Chromium: **24,3 MB**, a correr como `nonroot`, sobre
`distroless/static` (sem shell, sem gestor de pacotes).

```bash
docker build -t simulador-v2 .
docker run --rm -e DATABASE_URL=… simulador-v2 migrar
docker run --rm -p 8080:8080 -e DATABASE_URL=… simulador-v2 servir
```

**A forma de produção** — Caddy à frente, Postgres na mesma máquina, TLS — está
em `compose.producao.yml`, e o passo a passo em
[`docs/DEPLOY.md`](docs/DEPLOY.md). ⚠️ **Escrita e corrida em local, nada
exposto:** não há máquina ainda, e subir a máquina não é publicar o serviço — a
ordem é de pé, medido e fechado antes de divulgado.

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
2. **A validade da cache.** Os 5 minutos do `CACHE_VALIDADE` são um valor de
   partida e **não uma medição** — mede-se com uma vigia de horas, a perguntar o
   mesmo ao mesmo banco e a ver quando muda, e o que decide é a **primeira**
   mudança e não a média. ⚠️ Uma base acabada de migrar responde bem desde o
   primeiro pedido: não há série para encher, vai-se ao banco.

## Uso responsável

É aqui que a postura fica escrita, e fica pública de propósito — uma postura
escondida não é postura.

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
- **Nada disto é aconselhamento nem substitui o que o banco escreve.** O que este
  serviço faz é perguntar aos simuladores públicos e mostrar o que eles
  responderam, com a hora a que responderam.
