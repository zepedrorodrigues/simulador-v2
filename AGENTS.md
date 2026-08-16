# AGENTS.md — simulador-v2

Reescrita em **Go** do `simulador-credito-habitacao`. Serve **só JSON**; a
interface é a app Expo em `../simulador-v2-app`.

As convenções da casa (pt-PT, branches, commits, labels, verificação por
reversão) estão em `../AGENTS.md` e, na versão longa, em
`~/.claude/convencoes-repos.md`. Não se repetem aqui.

## Antes de mudar seja o que for

**Ler o `docs/ARQUITETURA.md`. É vinculativo, não descritivo.** Se o que vais
fazer não cabe no âmbito de §1, ou viola a regra de dependência de §3, ou
acrescenta uma tabela que não está em §4 — **altera-se o documento primeiro, com
justificação**, e só depois o código. Foi por acrescento não decidido que o v1
ganhou autenticação e depois teve de a apagar em três migrações.

⚠️ **Regra de desempate:** quando um documento e o código discordarem, é o
**código que está por mudar** — excepto quando o documento afirma um **facto
sobre o mundo**, e aí verifica-se o facto.

## Comandos

```
make dev          # sobe o PostgreSQL em contentor
make gerar        # sqlc + oapi-codegen — nunca editar código gerado à mão
make verificar    # o portão completo, e passa-se INTEIRO
```

O portão, por dentro: `golangci-lint run ./...` (o repositório **todo**, não um
subconjunto) e `go test -race ./...` (o `-race` não é opcional: o modelo é
fan-out concorrente).

⚠️ **O `make verificar` não corre em PowerShell**: o alvo `gerado` usa
`test -z ... || { ... }`, sintaxe POSIX que o `cmd` não percebe («'test' is not
recognized»). **Corre-se em Git Bash.** O que o `gofumpt` reprovar arruma-se com
`go tool golangci-lint fmt ./...`.

Subcomandos do binário: `servir`, `migrar`, `reverter`. ⚠️ Os alvos de rede
`teste-rede`, `teste-fidelidade` e `latencia` custam pedidos a terceiros e não
se correm sem razão.

## Estrutura

```
cmd/simulador/      o binário
internal/dominio/   tipos e regras puras. Não importa mais nada do projecto.
internal/bancos/    um pacote por banco + as 4 estratégias de transporte
internal/aplicacao/ aovivo: pedir a UM banco, com vaga. Sem HTTP, sem SQL.
internal/infra/     chi, pgx/sqlc, tecto por banco, tecto por IP, cache, config
api/openapi.yaml    o contrato — fonte da verdade dos tipos Go e TypeScript
db/                 migrações goose + queries sqlc
```

Dependências **só para dentro**: `dominio ← bancos ← aplicacao ← infra ← cmd`,
imposto pelo `depguard` e não pela boa vontade. O `api/` é folha: **só** a
`infra` o consome.

## Invariantes

- **Todo o caminho do cliente fala com bancos**, e o custo é **por cliente, não
  por corrida**. Pedidos HTTP por simulação, medidos: **1,00** no Banco CTT,
  **2,03** no Montepio, **4,00** no Santander — uma comparação a cinco bancos são
  **~10 pedidos a terceiros**. Reaproveitar configuração e catálogo dentro de um
  pedido é defesa, não optimização.
- **Somos um amplificador.** Um pedido nosso vira ~10 aos bancos, com origem
  aparente nossa. O tecto por IP é **estrutural** e o `PROXIES_DE_CONFIANCA` é
  **bloqueante** — sem ele medido, ou o tecto é contornável, ou é o tecto do site
  inteiro (o bug de produção do v1).
- **Nada de estado em-processo** para cache ou tecto: vive em **Postgres** —
  `internal/infra/lotacao` (2 vagas por banco), `internal/infra/limites` (tecto
  por IP), `internal/infra/cache` (respostas, 5 min).
- **Não editar código gerado** (`sqlc`, `oapi-codegen`): desaparece no `make
  gerar` seguinte.
- **Não escrever um banco sem captura primeiro.** A ordem está em
  `docs/CONTRATO-BANCO.md` §3: capturar → parser contra a captura → payload → só
  então ligar a rede. E ver o teste **falhar** antes de o pôr a passar.
- **Sem pasta de `scripts`.** Foi onde o v1 acumulou 50+ ficheiros ad-hoc e 338
  erros de lint permanentes que cegaram o portão. Sondagem vive numa branch e não
  é submetida, ou vira um teste.
- **Nada de dados pessoais nem segredos nas capturas** — o repositório é público.
  Titular fictício; cookies, tokens e cabeçalhos de autenticação removidos.
- **Confirmar `git branch --show-current` antes de commitar.**

### Comentários

O mais curto que se perceba, e só se for estritamente necessário para se
perceber. Um comentário ganha o seu lugar quando diz o que o código não pode
dizer: um **número medido**, uma **base legal**, ou **porque é que a alternativa
óbvia está errada**. Se o porquê já vive num documento, o comentário **remete**
em vez de repetir — uma cópia diverge em silêncio, e já aconteceu aqui.

## Lições

Memória entre sessões. Máx. **25 entradas**; ao chegar lá, destilar — juntar as
que dizem o mesmo e apagar as que o código já impõe sozinho (uma lição que virou
teste, `depguard` ou hook deixa de precisar de linha).

- **2026-08-06 · a §1 foi revertida: o pedido do cliente volta a ir ao banco.**
  Não há varrimento, não há grelha, não há modelo de preço nosso. Guardar uma
  cópia do modelo de preço de cada banco obriga a acertar em como eles preçam, e
  num só dia de confronto com dados reais falhámos isso **quatro vezes**
  (`KAN-54` a `KAN-57`). O porquê longo está em `docs/DECISAO-AO-VIVO.md`.
- **2026-08-07 · a Fase 6 fechou e o desenho antigo saiu do repositório.**
  Durante duas semanas os documentos descreviam para onde se ia e o código de
  onde se vinha; essa distância acabou. Saíram com ela os subcomandos `varrer` e
  `sondar` e o `medicao` do Makefile.
- **2026-08-07 · a `/api/rate-catalog` saiu, e a D1 fechou assim:** o
  `viabilidade-imobiliaria` consome a do **v1** (`chmonitor`), que mantém os
  scrapers e não é tocado. A daqui era uma reimplementação congelada sem
  consumidor. ⚠️ Dizia-se aqui, e em mais quatro documentos, que ele a lia «em
  produção» — era falso nos dois sentidos. Um dia o v2 responderá às perguntas do
  `viabilidade`, mas **nunca por esta rota**: ela publica uma série temporal, e
  sem varrimento não há como a produzir.
- **2026-08-07 · o `infra/travao` saiu com o varrimento.** Era um fecho contra
  dois varrimentos do mesmo banco; ao vivo, dois clientes a perguntar pelo mesmo
  banco é o normal, e o que se limita é quantos de cada vez.
- **KAN-39 · o Redis saiu do ambiente**, e da §7 antes disso. O `CLAUDE.md` ainda
  dizia «vive em Redis» depois de já não viver. Não volta sem entrada nova na §7.
- **KAN-29 · o `api/` ser folha passou a estar imposto.** Até aí estava
  prometido, e uma promessa não trava nada.
- **2026-08-01 · tudo isto é versionado aqui** — `AGENTS.md`, `PLAN.md`,
  `RESUME.md` e `docs/`. Revoga o `652eda2`, que os mandara para um Confluence
  privado. O argumento («o código é aberto de propósito, o planeamento não») não
  caiu; caiu o custo de o cumprir: duas moradas sem sincronização divergiram — o
  `ARQUITETURA.md` tinha **485 linhas num lado e 505 no outro**, e nenhuma linha
  se perdeu por sorte, não por desenho. ⚠️ O espaço `GDP` do Confluence ficou lá
  e **não é a verdade**: não se lê nem se actualiza.

### Protocolo

Ler no arranque. **No fecho, acrescentar só o que se mediu e que uma sessão
futura repetiria** — uma data, o facto, e onde está a prova. Não é changelog: o
que já se vê no `git log` não vem para aqui.

## Onde está o resto — carregar por gatilho

O `docs/` são **187 KB em 8 ficheiros** (medido a 2026-08-15). Lê-los «para ter
contexto» custa ~50k tokens antes da primeira alteração.

| ficheiro | só quando |
|---|---|
| `docs/ARQUITETURA.md` | **antes de qualquer alteração estrutural** — é vinculativo |
| `docs/CONTRATO-BANCO.md` | mexer num banco ou acrescentar um |
| `docs/DOSSIE-BANCOS.md` | o que o v1 apurou sobre um banco em concreto |
| `docs/API.md` | mudar o contrato HTTP (e então também `api/openapi.yaml`) |
| `docs/APP.md`, `docs/ECRAS.md` | o que a app impõe ao contrato / os ecrãs |
| `docs/DEPLOY.md` | produção: VPS, Caddy, Postgres na mesma máquina |
| `docs/DECISAO-AO-VIVO.md` | discutir o âmbito de §1 outra vez |
| `RESUME.md` | retomar a meio, ou fechar sessão (estado e próximos passos, **sem changelog**) |
| `PLAN.md` | discutir sequência de fases |

**O backlog é o projecto `KAN` do JIRA** (`jpnmsr.atlassian.net`), não as issues
do GitHub — essas ficam como arquivo e não se abrem mais. Labels em uso:
`prioridade-alta`, `bancos`, `api`, `dados`, `dominio`, `infra`, `mercado`,
`risco`, `epico`. ⚠️ No JIRA as labels são texto livre: uma variante nova é
aceite sem avisar e parte os filtros em silêncio.

O v1 fica em `../simulador-credito-habitacao` e continua a ser a referência
técnica **sobre os bancos**. ⚠️ Não é referência de arquitectura — é precisamente
o que esta reescrita existe para não repetir.
