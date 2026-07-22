# Arquitectura — simulador-v2

Este documento é a decisão de desenho. É vinculativo: quando o código diverge
dele, um dos dois está errado e resolve-se **antes** de continuar.

**Stack:** Go · PostgreSQL · Redis · `chi` · `pgx` + `sqlc` · OpenAPI.
A interface com pessoas é uma app **React Native**, em repositório separado.

## 1. Âmbito

O `simulador-v2` faz **duas** coisas e mais nenhuma:

1. **Comparar ofertas de crédito à habitação ao vivo.** Recebe um pedido,
   interroga os simuladores públicos de N bancos, devolve as ofertas à medida
   que chegam.
2. **Publicar a série temporal de mercado** (`GET /api/rate-catalog`), varrida
   periodicamente sobre uma grelha de cenários de referência com titular neutro.

O que **não** faz, e não passa a fazer sem uma decisão nova registada aqui:
contas de utilizador, histórico por pessoa, relatórios por email, backup próprio,
`doctor`, `market-diff`, `cadence`, envio de leads a bancos.

Este repositório serve **JSON e mais nada** — sem templates, sem HTML, sem
ficheiros estáticos. A restrição é deliberada: no v1 não havia fronteira entre
UI e servidor, e a lógica de apresentação acabou espalhada pelo servidor.

## 2. De onde vêm estas regras

Quatro dores concretas do v1. Cada uma produz uma regra **verificável** — não um
princípio, uma regra que um portão automático reprova.

| Dor do v1 | Evidência | Regra do v2 |
|---|---|---|
| Modelo de dados entulhado | 3 tabelas, uma delas (`auth_throttle`) com o nome de uma funcionalidade apagada; 7 migrações, 3 a criar e destruir autenticação; `raw_json`/`applied_json`/`notes_json` como `Text` | **§4**: duas tabelas. `jsonb`, nunca `text` com JSON dentro. Sem colunas herdadas de funcionalidades mortas. |
| Sem arquitectura; tudo vazava | Lógica de negócio em `web/`, `cli.py`, `reporting/` e `scrapers/` ao mesmo tempo; `scripts/` com 50+ ficheiros ad-hoc | **§3**: quatro camadas com uma regra de dependência **imposta pelo linter**. Sem pasta de scripts. |
| Scrapers frágeis e inconsistentes | Cada scraper reimplementa o seu transporte; o BPI recorta `innerText` com regex e falhava ~1 em 3 corridas; os 4 de browser não tinham forma de ser testados offline | **§5**: parsing puro, separado do transporte, sempre contra capturas reais versionadas. Quatro estratégias de transporte, não dez. |
| Frontend complicado | Um `dashboard.html` de 874 linhas com CSS e JS embutidos, formulário e resultados na mesma janela (issue #2 do v1: «visually too much information + just crap») | **§6**: o servidor não conhece apresentação. Fluxo em ecrãs separados (ver `ECRAS.md`). |

## 3. Camadas

```
cmd/
  simulador/        o binário: servidor HTTP e subcomandos
internal/
  dominio/          tipos e regras. Zero I/O, zero dependências do projecto.
  bancos/           um pacote por banco + as estratégias de transporte.
  aplicacao/        casos de uso: orquestrar, cachear, varrer. Sem HTTP, sem SQL.
  infra/            chi, pgx/sqlc, redis, configuração, relógio.
api/openapi.yaml    o contrato publicado (fonte da verdade)
db/                 migrações, queries .sql, sqlc.yaml
```

**A regra de dependência**, do interior para o exterior:

```
dominio    → (nada, além da stdlib)
bancos     → dominio
aplicacao  → dominio, bancos
infra      → dominio, bancos, aplicacao
cmd        → infra
```

`dominio` não sabe que existe HTTP. `bancos` não sabe que existe base de dados.
`aplicacao` não sabe se foi chamada por HTTP ou pela linha de comandos.

⚠️ **Isto é imposto, não prometido.** O `internal/` já impede importação de fora
do módulo, mas não regula as camadas entre si — isso fica a cargo do `depguard`
no `.golangci.yml`, que reprova a compilação do portão perante um `import` que
viole a tabela acima. Um diagrama que ninguém executa foi exactamente o que o v1
teve.

### O que vive em cada camada

**`internal/dominio/`** — `Pedido` (o que o utilizador quer), `Oferta` (o que um
banco responde), `Fase` (um troço do plano com taxa própria), `Produto`,
`Requisitos` (o que um banco precisa e aceita), e as políticas puras: encaixar um
período fixo numa lista válida, limitar o prazo por idade, decidir o que é
comparável. Funções puras sobre estes tipos.

**`internal/bancos/`** — `contrato.go` define a interface que cada banco cumpre;
`transporte/` tem as quatro estratégias (§5); `<banco>/` tem três ficheiros e uma
pasta de capturas; `registo.go` mapeia `bancoID → construtor`.

**`internal/aplicacao/`** — `comparar` (correr N bancos em paralelo, emitir
resultados progressivos), `varrimento` (o snapshot dos cenários de referência),
`cache` (a política de §7), `limites` (tecto por IP). Recebe os bancos por
injecção; nunca importa o registo directamente — é o que torna um caso de uso
testável com bancos falsos.

**`internal/infra/`** — `http` (routers `chi`, handlers, tradução de erros de
domínio para códigos HTTP), `bd` (código gerado pelo `sqlc` + migrações),
`cache` (cliente Redis), `config`, `relogio`.

⚠️ **Os tipos que saem em JSON são distintos dos tipos de `dominio`.** Parece
duplicação e não é: são a fronteira publicada e versionada, que não pode mudar só
porque o domínio mudou. No v1 o modelo de domínio *era* o contrato HTTP, e por
isso qualquer refactor arriscava partir o `viabilidade-imobiliaria`.

## 4. Modelo de dados

**Duas tabelas. Não há uma terceira sem uma entrada nova nesta secção.**

### `catalogo_taxas` — a série de mercado

Uma linha = um banco × um cenário de referência, num varrimento. É a única coisa
guardada a longo prazo, e não contém dados pessoais: os cenários são fixos e o
titular é neutro.

| coluna | tipo | nota |
|---|---|---|
| `id` | `bigserial` PK | |
| `varrimento_id` | `uuid` | agrupa as linhas da mesma corrida |
| `capturado_em` | `timestamptz` | ⚠️ **com** fuso |
| `cenario` | `text` | chave da grelha de referência |
| `banco_id`, `banco_nome` | `text` | |
| `rate_type` | `text` | `variavel` \| `fixa` \| `mista` |
| `valor_imovel`, `montante` | `numeric(12,2)` | ⚠️ **não** vírgula flutuante |
| `prazo_anos`, `fixed_period_years` | `int` | |
| `euribor_indexante` | `text` | |
| `tan`, `taeg`, `spread`, `euribor_valor` | `numeric(6,3)` | pontos percentuais |
| `prestacao_mensal`, `mtic` | `numeric(12,2)` | |
| `produtos` | `jsonb` | ids dos produtos aplicados |
| `aplicado` | `jsonb` | o que o banco usou de facto, quando difere do pedido |
| `notas` | `jsonb` | avisos legíveis por pessoa |
| `sucesso` | `bool` | |
| `erro` | `text` | |

Índices: `(cenario, banco_id, capturado_em desc)` e `(capturado_em desc)`.

⚠️ **`numeric`, não `float`.** O v1 guardava dinheiro e taxas em `Float`. Não
partiu nada visível, e é esse o problema: erros de vírgula flutuante em
comparações de cêntimos aparecem como diferenças de 0,01 € que ninguém consegue
explicar nem reproduzir. Em Go: `pgtype.Numeric` na fronteira da BD e
`shopspring/decimal` no domínio.

⚠️ **Armadilha de compatibilidade a verificar com teste.** O
`shopspring/decimal` serializa para JSON como **string entre aspas** por omissão,
e o v1 (Python) enviava **números**. Como o `/api/rate-catalog` tem de ficar
compatível ao byte (§6), a conversão para `float64` faz-se no tipo de saída, e há
um teste de contrato que compara a resposta com uma amostra real do v1.

⚠️ **`timestamptz`, não instante ingénuo.** O v1 guardava sem fuso e tinha uma
`utcnow()` com um comentário a explicar que não se podia trocar por
`datetime.now(UTC)` sem rebentar as comparações. Isso é dívida a render juros.

⚠️ **`produtos` é obrigatório para a série ser comparável.** Sem saber que
condições cada taxa pressupõe, a CGD e o BPI aparecem caros por lhes faltar o
desconto, não por cobrarem mais. O v1 descobriu isto tarde.

### `limites` — tecto de pedidos por IP

| coluna | tipo |
|---|---|
| `chave` | `text` PK (ex.: `sim:ip:1.2.3.4`) |
| `janela_inicio` | `timestamptz` |
| `contagem` | `int` |
| `bloqueado_ate` | `timestamptz` null |

O site é público e cada submissão custa dezenas de segundos de scraping **a
partir do nosso IP** contra os bancos. A tabela tem nome próprio, e não o de uma
funcionalidade apagada.

### O que **não** é uma tabela

- **Simulações de utilizador.** Correm e são devolvidas. Não são guardadas.
- **A cache.** Vive em Redis (§7).
- **Sessões, contas, tokens, emails.** Não existem e não vão existir.

**Migrações:** `goose`, ficheiros SQL versionados em `db/migracoes/`, embebidos
no binário. ⚠️ **Não há auto-migração.** Foi o `create_all` automático do v1 que
deixou a base num estado híbrido que o Alembic desconhecia, e a migração seguinte
morreu sobre uma tabela que já existia.

## 5. Contrato dos bancos

Detalhe completo em [`CONTRATO-BANCO.md`](CONTRATO-BANCO.md). O essencial de
arquitectura:

**Cada banco são três ficheiros e uma pasta:**

```
internal/bancos/cgd/
  cgd.go         a implementação da interface; declara `Requisitos()`
  pedido.go      dominio.Pedido  →  payload do banco      (PURO)
  resposta.go    payload do banco →  dominio.Oferta       (PURO)
  capturas/      pares pedido/resposta reais, versionados
```

⚠️ **`pedido.go` e `resposta.go` não fazem I/O.** Recebem e devolvem dados, sem
rede e sem relógio. É isto que torna cada banco testável offline contra uma
captura real, e é a diferença estrutural face ao v1 — onde os quatro bancos de
browser não tinham forma nenhuma de ser testados sem rede.

**Quatro estratégias de transporte, partilhadas** (`internal/bancos/transporte/`),
porque foram quatro as que o v1 provou serem necessárias:

| estratégia | quem a usa | o que faz |
|---|---|---|
| `HTTPSimples` | CGD, Novo Banco, Banco CTT, Crédito Agrícola, Santander | um ou mais pedidos HTTP, sem estado |
| `HTTPComSessao` | Montepio | um `GET` de arranque que fixa cookies e extrai um token do HTML, depois o `POST` |
| `BrowserParaCredencial` | ActivoBank, Millennium BCP | browser só para cunhar um token, simulação em HTTP |
| `BrowserComoCliente` | Bankinter (Cloudflare), BPI (formulário) | o pedido parte de dentro da página |

No v1 cada scraper reimplementava a sua variante de «abrir browser / fixar
cookies / cunhar token», e as variantes nunca convergiram. Aqui a estratégia é
injectada — o que também a torna substituível por uma falsa nos testes.

⚠️ **As duas estratégias de browser são um risco por avaliar, não uma decisão
tomada.** Em Go isso é `playwright-go` (binding da comunidade) ou `chromedp`. O
caso difícil é o Bankinter, que atravessa Cloudflare com um disfarce
anti-automação específico. A decisão fica para a fase 3, com uma prova de
conceito antes do compromisso, e com uma saída conhecida: correr os bancos de
browser como serviço à parte, atrás da mesma interface.

**Política de flexibilidade (herdada do v1, e boa):** quando o banco não aceita
exactamente o que foi pedido — período fixo fora da lista, prazo acima do máximo
por idade, Euribor imposta — simula-se no valor válido mais próximo, regista-se o
que foi aplicado em `Oferta.Aplicado` e acrescenta-se uma nota. **Não falha.**
Falhar é para quando o banco não consegue responder de todo.

⚠️ **Um banco avariado nunca derruba a comparação.** No v1 isso obrigava a um
`except Exception` dentro de cada scraper. Em Go a política é a mesma mas fica
**num sítio só**: o orquestrador de `aplicacao/comparar` corre cada banco numa
goroutine com `recover` e converte pânico ou erro numa oferta de falha. Dentro de
um banco, os erros devolvem-se; não se engolem.

## 6. Fronteiras HTTP

Duas, com estatutos diferentes. Contrato completo em [`API.md`](API.md), esquema
executável em `api/openapi.yaml`.

**`/api/v1/*` — a app React Native.** Nossa, versionada, evolui connosco. Os
tipos Go do servidor e os tipos TypeScript da app são **gerados** a partir do
`openapi.yaml` (`oapi-codegen` e `openapi-typescript`). O contrato é código dos
dois lados, não documentação.

**`/api/rate-catalog` — o `viabilidade-imobiliaria`.** ⚠️ **Compatível ao byte
com o v1.** O cliente que já existe (`src/viabilidade/taxas.py`) lê `points[]`
com `tan, spread, euribor_valor, euribor_indexante, fixed_period_years, bank_id,
bank_name, scenario_key, rate_type, prazo_anos, captured_at`, mais `scenarios[]`,
com os parâmetros `scenario, bank, rate_type, since, limit` e o cabeçalho
`X-API-Key`. Os nomes ficam **em inglês e em snake_case** neste endpoint, ao
contrário do resto do repositório, porque mudá-los parte o outro repositório sem
aviso. Há um teste de contrato dedicado.

Sem chave configurada o catálogo fica aberto — é o comportamento de
desenvolvimento, e é um aviso alto no arranque.

⚠️ **A congelação é interina, não permanente.** O `viabilidade-imobiliaria` vai
levar o mesmo tratamento que este repositório está a levar. Quando isso
acontecer, o contrato redesenha-se **em conjunto** — com os nomes em português e
com a decomposição spread/Euribor que a issue #22 do v1 propunha. Até lá, o
formato antigo é lei: quem consome está em produção e não pede licença para
partir.

## 7. Cache e capacidade

O v1 mediu isto a sério; o v2 começa já do outro lado da medição.

**Os números medidos** (v1, 2026-07-22, contentor contra Postgres 17): uma
comparação de 10 bancos fecha em **52,2 s**, mas 9 dos 10 estão prontos aos
**25,9 s** e 7 aos **7,4 s** — o BPI sozinho define o total. Bancos de HTTP puro
custam **1-2 s**; bancos de browser custam **30-80 s**. Sem cache, o tecto é ~20
comparações por hora.

Daí saem cinco decisões, todas já tomadas:

1. **Resultados progressivos, sempre.** A app recebe cada banco à medida que
   chega. Serializar os bancos para poupar memória empurra a cauda e piora o que
   já é bom — o v1 mediu-o e registou-o.

2. **Validade por custo do scraper, não uniforme.** Bancos de HTTP são baratos:
   cache curta. Bancos de browser são caros: cache longa. O custo é conhecido,
   não precisa de ser medido.

3. **⚠️ A validade nunca atravessa a viragem do dia.** A Euribor fixa
   diariamente e a TAN depende dela. Uma validade de 6 h que apanhe a meia-noite
   serve uma taxa de ontem como se fosse de hoje.

4. **Quantizar o pedido, não só a chave.** Montante ao milhar, valor do imóvel
   aos 5 000 €. ⚠️ **Com guarda de LTV**: se o arredondamento cruzar um degrau de
   5 % de LTV, não arredonda — o spread é uma função em degraus e dar a banda
   errada é pior do que perder o acerto de cache. O que foi de facto simulado
   volta na resposta, pelo mesmo mecanismo do `aplicado`. ⚠️ A idade **não** se
   quantiza: entra no seguro de vida e no prazo máximo.

5. **⚠️ Nada de estado em-processo.** O v1 tinha o gate por banco, o dedup e a
   cache dentro do processo, e um aviso no arranque a dizer que com mais do que
   um worker o mesmo banco levava N scrapes em paralelo — precisamente o que o
   gate existia para evitar. Várias PaaS definem a concorrência sozinhas. No v2 o
   gate por banco e a cache vivem em **Redis** desde o primeiro dia.

**⚠️ IP atrás de proxy — bug de produção do v1, corrigido de origem.** No v1, o
servidor não aceitava o `X-Forwarded-For` de um proxy noutro contentor e o IP do
cliente passava a ser o do proxy, igual para toda a gente: o tecto «10 pedidos
por 10 min por IP» virava o tecto do **site inteiro**, e o primeiro visitante
trancava os restantes. A correcção **não** é confiar em toda a gente — isso deixa
qualquer cliente escolher o seu IP num cabeçalho. É uma lista explícita de
proxies de confiança, com default vazio, e um teste que confirma que um
`X-Forwarded-For` enviado directamente à app é ignorado.

## 8. Verificação

Regra da casa: um critério de pronto vale pelo que se mediu. Reverte-se o defeito
e confirma-se que o teste **falha**, e que falha a nomear a coisa certa.

Cinco portões, todos automáticos:

1. **`depguard`** — a regra de dependência de §3.
2. **Teste de contrato do `/api/rate-catalog`** — o formato de §6 que o
   `viabilidade-imobiliaria` consome, contra uma amostra real do v1.
3. **Testes de captura por banco** — cada `resposta.go` contra as capturas
   versionadas, offline e sem rede.
4. **`golangci-lint run ./...` limpo, sobre o repositório inteiro.** ⚠️ Não sobre
   um subconjunto. O v1 tinha 368 erros permanentes fora de `src/`, o que cegou o
   portão por completo: ninguém lê uma saída de 368 linhas para descobrir a
   369.ª. Aqui não há pasta de scripts onde eles se acumulem, e o portão é o
   repositório todo ou não é portão.
5. **`go test -race ./...`** — o detector de corridas é obrigatório no portão. O
   modelo é fan-out concorrente sobre N bancos; uma corrida de dados aqui produz
   números errados, não um crash.
