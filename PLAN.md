# Plano

Seis fases. Cada uma acaba em algo que **funciona e se pode ver a funcionar** —
não em «a camada X está pronta». O desenho está em [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md).

**Regra de ordem:** nenhuma fase começa antes de a anterior ter o seu portão
verde. A dívida do v1 acumulou-se por se passar à frente com «depois arruma-se».

---

## Fase 0 — Fundações

Sem isto não se escreve código de negócio. Termina quando `make verificar` corre
do princípio ao fim numa máquina limpa.

| # | Trabalho |
|---|---|
| 1 | Instalar a toolchain: Go, Node, `sqlc`, `goose`, `golangci-lint`, `oapi-codegen` |
| 2 | Esqueleto do módulo + as quatro camadas + `depguard` a impor a regra de dependência |
| 3 | `docker-compose`: PostgreSQL e Redis locais; `Makefile` com os alvos do portão |
| 4 | Migrações iniciais (`goose`): `catalogo_taxas` e `limites` |
| 5 | `sqlc`: as queries do catálogo, geradas e verificadas no portão |
| 6 | `api/openapi.yaml` inicial + geração dos tipos Go e TypeScript |
| 7 | CI no GitHub Actions: `build`, `go test -race`, `golangci-lint`, geração em dia |

**Portão:** `golangci-lint run ./...` limpo, `go test -race ./...` verde, e o
`depguard` a reprovar de facto um `import` proibido (verificado por reversão —
mete-se o `import` errado e confirma-se que falha, a nomear a camada).

---

## Fase 1 — A fatia vertical

Quatro bancos, todos de HTTP puro, escolhidos para exercitarem o máximo de
dimensões do contrato ao mínimo custo. **Sem browser, sem Chromium na imagem.**

| banco | dimensão que prova |
|---|---|
| **CGD** | linha de base; períodos fixos descobertos em runtime com recurso |
| **Novo Banco** | produtos ligados por omissão; **erro estruturado como sinal** (`V159` traz o prazo máximo); Euribor escolhível; a finalidade muda o preço |
| **Montepio** | **sessão** (`GET` → cookies + hash → `POST`); **não tem taxa fixa** → prova o mecanismo «ajusta e anota» |
| **Banco CTT** | Euribor **imposta e lida da resposta**; a armadilha do spread da fase errada |

| # | Trabalho |
|---|---|
| 8 | Domínio: `Pedido`, `Oferta`, `Fase`, `Produto`, `Requisitos` e as políticas puras de ajuste |
| 9 | Contrato `Banco`, registo, e os transportes `HTTPSimples` e `HTTPComSessao` |
| 10 | Orquestrador `comparar`: fan-out, resultados progressivos, cancelamento, isolamento de falha |
| 11 | Cache e *gate* por banco em Redis — validade por custo, sem atravessar a viragem do dia |
| 12-15 | Os quatro bancos, um a um, cada um com captura primeiro |
| 16 | `GET /api/v1/bancos`, `POST` e `GET /api/v1/simulacoes` |
| 17 | Tecto por IP + lista explícita de proxies de confiança |
| 18 | Quantização do pedido com guarda de degrau de LTV |

**Portão:** uma comparação real dos quatro bancos devolve quatro ofertas, cada
número confrontado com o site do banco e a data registada no teste; cada banco
com testes de captura a correr offline; e o Montepio a devolver a nota de
aproximação quando se pede taxa fixa.

---

## Fase 2 — Mercado

O que o `viabilidade-imobiliaria` consome. ⚠️ Fronteira congelada.

| # | Trabalho |
|---|---|
| 19 | Grelha de cenários de referência + varrimento periódico |
| 20 | `/api/rate-catalog` **compatível ao byte** + teste de contrato contra amostra real do v1 |
| 21 | Banco Santander |
| 22 | Banco Crédito Agrícola |

**Portão:** o cliente real do `viabilidade-imobiliaria`, sem uma linha alterada,
lê o catálogo do v2 e devolve os mesmos números que lia do v1.

---

## Fase 3 — Bancos de browser

⚠️ **Começa por uma decisão, não por código.** É a parte de risco por avaliar.

| # | Trabalho |
|---|---|
| 23 | Prova de conceito `playwright-go` vs `chromedp`, com o Bankinter (Cloudflare) como caso de teste. Decisão registada. |
| 24 | Sondagem: procurar a API por baixo do formulário OutSystems do BPI **antes** de escrever um scraper de browser |
| 25 | ActivoBank + Millennium BCP (browser só para cunhar credencial) |
| 26 | Bankinter |
| 27 | BPI |

**Saída conhecida se correr mal:** os bancos de browser correm como serviço à
parte, atrás da mesma interface. Isso é uma decisão a tomar na #23, com prova, e
não uma esperança.

---

## Fase 4 — Produção

| # | Trabalho |
|---|---|
| 28 | `Dockerfile` e deployment |
| 29 | Observabilidade: logs estruturados, `X-Request-ID` ponta a ponta |
| 30 | CORS para o Expo web |

---

## Fase 5 — Ecrã de mercado

A série do catálogo dentro da app. Fica para o fim de propósito: é a coisa mais
fácil de adiar e a mais fácil de fazer mal cedo demais.

---

## A app React Native

Repositório separado: **`simulador-v2-app`**. Arranca em paralelo com a **fase
1**, assim que `api/openapi.yaml` existir (issue #6) — os tipos TypeScript são
gerados dele, por isso a app constrói-se contra um servidor falso antes de o
servidor a sério responder.

Ecrãs em [`docs/ECRAS.md`](docs/ECRAS.md), stack e fases em
[`docs/APP.md`](docs/APP.md).

⚠️ **A app impõe uma restrição ao backend, e é bloqueante:** com uma app nas
lojas não se controla quem actualiza, por isso `/api/v1` **só pode mudar por
acrescento**. E o caminho de «esta versão é demasiado antiga» custa pouco agora e
é impossível de acrescentar quando já houver versões antigas no terreno — que é
exactamente quando faz falta.

## Uso responsável

[`docs/USO-RESPONSAVEL.md`](docs/USO-RESPONSAVEL.md). O v1 era um script pessoal;
o v2 é uma app publicada que corre os simuladores de dez bancos a partir do nosso
IP. As decisões técnicas estão tomadas e estão espalhadas pelas issues (cache,
quantização, tecto por IP, nunca tocar em endpoints de *lead*).

⚠️ **As perguntas que exigem parecer jurídico são bloqueantes da publicação nas
lojas** — não da fase 1. Estão na §3 desse documento e na issue própria.

---

## Como se sabe que isto não voltou a entulhar

Quatro medições, a rever no fim de cada fase. Os números do v1 são o ponto de
partida a não repetir.

| indicador | v1 | limite no v2 |
|---|---|---|
| Erros de lint fora do portão | 368 permanentes | **0**, sempre |
| Ficheiros ad-hoc sem dono | 50+ em `scripts/` | não existe essa pasta |
| Maior ficheiro de interface | 874 linhas num só HTML | app noutro repositório |
| Tabelas com nome de funcionalidade morta | 1 de 3 | **0** |

⚠️ E a regra que apanha o resto: **uma funcionalidade nova que não caiba no
âmbito de `ARQUITETURA.md` §1 não entra sem essa secção ser alterada primeiro.**
Foi por acrescento não decidido que o v1 ganhou autenticação, e depois teve de a
apagar em três migrações.
