# RESUME — simulador-v2

Estado actual e próximos passos. ⚠️ **Sem changelog** — o relato de sessões não vive aqui. O `RESUME.md` do v1 chegou a 1317 linhas antes de ser esvaziado à força.

**Actualizado:** 2026-08-06

⚠️ **A §1 foi revertida: o pedido do cliente volta a ir ao banco.** Decidido a 2026-08-06, depois de um dia a confrontar o servido com dados reais. O porquê, com os números, está em `docs/DECISAO-AO-VIVO.md`.

⚠️ **Os documentos já descrevem o desenho novo; o código ainda é o antigo.** Quando discordarem, é o **código** que está por mudar. É a Fase 6 do `PLAN.md`, e não está começada.

## Onde estamos

**O ciclo antigo fecha de ponta a ponta e corre em local** — varre, grava, lê, responde por HTTP, e a app mostra-o. ⚠️ **E é esse ciclo que se vai desmontar.**

**Cinco bancos:** CGD, Novo Banco, Montepio, Banco CTT, Santander. Os parsers e as capturas **sobrevivem inteiros** e passam a ser o activo principal do repositório.

**Fidelidade:** 3002 ofertas conferidas em cada banco, **zero divergências** em 147 098 comparações. ⚠️ Deixa de ser curiosidade e passa a ser a **garantia central**: é ela que diz que o que servimos é o que o banco disse.

**A app** (`simulador-v2-app`): Expo SDK 57, os três passos do pedido (A3) e a lista com detalhe (A5). ⚠️ A **A4 volta** — resultados progressivos, com o fan-out do lado da app.

## Porque é que se reverteu

Num só dia, quatro assunções do modelo de preço caíram contra dados varridos a 2026-08-06:

| assunção | o que os bancos fazem | issue |
|---|---|---|
| a TAEG deriva-se de dois encargos | o gap TAEG−TAN **sobe** com o prazo em 4 dos 5 bancos | `KAN-54` |
| a derivada serve mesmo onde o banco publicou | banco 4,500 %, servia-se 4,712 % | `KAN-55` |
| os descontos de produtos somam-se | Novo Banco: 0,500 + 0,200 dá **0,600** | `KAN-56` |
| um crédito tem um spread | Santander: 0,5 p.p. por 36 meses, 0,8 depois | `KAN-57` |

⚠️ **Nenhuma foi apanhada por um teste.** O portão esteve verde o tempo todo. Todas apareceram por se confrontar o servido com o medido — e a última só porque a `KAN-55` recusou servir.

## Próximo passo

1. **Fechar a D1**: o que acontece ao `/api/rate-catalog`, que perde a fonte e é consumido pelo `viabilidade-imobiliaria` **em produção**. Bloqueia a §4 e a §6 do `ARQUITETURA.md`. Três saídas escritas, nenhuma escolhida.
2. **Medir os três números** de que a Fase 6 depende e que não temos: latência por banco, concorrência que cada banco tolera, validade útil da cache.
3. **`KAN-7`** — o `comparar` volta a falar com bancos, com tecto e timeout por banco.
4. **`KAN-8`** — cache em Postgres, chave = pedido exacto, pedido em claro fora do disco.
5. **`KAN-14`** — tecto por IP dimensionado para **N pedidos por comparação**, e `PROXIES_DE_CONFIANCA` **medido**. Passou de dívida a bloqueante.
6. **Só então** retirar o que morreu: `sondagens`, escala, encargos, grelha. ⚠️ Apagar antes deixa o repositório sem nada que responda.

## O que está por resolver

- ⚠️ **A D1 e a D4.** A primeira é dívida com outro repositório; a segunda é o parecer jurídico (`KAN-24`), que passou de «bloqueia as lojas» a **bloqueia o produto** — a fatia ao vivo é o produto inteiro, não uma funcionalidade dele.
- ⚠️ **O fan-out na app é o ponto mais discutível do desenho novo.** Foi escolha contra SSE (o `fetch` do React Native não o suporta nativamente), e põe na app a responsabilidade de não disparar dez pedidos de uma vez.
- ⚠️ **Somos um amplificador:** um pedido nosso vira ~10 aos bancos, com origem aparente nossa. Cruza a carga do varrimento às **~190 comparações/dia** — abaixo carregamos menos, acima cresce sem tecto.
- ⚠️ **A latência existe e está medida:** o Montepio não respondeu dentro de **10 s** em 4 cenários, em hora de expediente.
- **Canceladas pela reversão:** `KAN-54`, `KAN-55`, `KAN-56`, `KAN-57`, `KAN-41`, `KAN-34`, `KAN-38` — descrevem o modelo, não os bancos. ⚠️ Ficam escritas como canceladas, não apagadas.
- **`KAN-53`** (o relatório do varrimento subconta falhas) morre com o varrimento. ⚠️ A **lição** fica: uma corrida que parece boa e não é.
- **Vivas e agora centrais:** `KAN-7`, `KAN-8`, `KAN-14`. **Continua morta:** `KAN-15` (quantização) — a cache volta, ela não.
- **`KAN-19`** (Crédito Agrícola) continua a fazer sentido: é um banco a mais para perguntar.
- ⚠️ **Bancos de browser** (`KAN-20`, `KAN-21`) ficam **mais** caros com este desenho: um browser por pedido de cliente é outra ordem de grandeza.

## Lições

**Um modelo do preço de outra pessoa é uma dívida que vence sozinha.** Cada peça da reconstrução — escala de LTV, encargos, descontos, fases — era uma hipótese sobre como um banco preça. Quatro caíram num dia, nenhuma por teste. ⚠️ **O que as apanhou foi servir e comparar.**

**Uma guarda que recusa servir denuncia mais do que um teste que passa.** A `KAN-55` recusou uma TAEG e, ao recusar, expôs a `KAN-56` e a `KAN-57` — duas coisas que estavam na base desde sempre e que ninguém confrontava.

**Um comentário que descreve uma verificação não é a verificação.** O `comProdutos` dizia que a linha da combinação existia «para poder contradizer a aditividade»; ninguém a confrontava. Os resíduos do ajuste eram calculados e descartados com `_`. As duas peças existiam, escritas e explicadas, e não corriam.

**Um teste cujos dois lados se constroem do mesmo sítio não vê a diferença entre eles.** O primeiro critério da `KAN-55` comparava fases da oferta com fases da observação: passava nos testes (a fixture constrói-as) e **nunca disparava em produção** (o `leitura.go` não as reconstrói). Só se viu ao correr contra o varrimento real.

**Correr a forma de produção é um teste, e encontra o que nenhuma suite encontra.** Continua verdadeiro, e foi assim que este dia aconteceu.

## Notas de máquina

- Lint por `go tool golangci-lint run ./...` — o do `PATH` é v1.64.8 e não lê o `.golangci.yml` v2. O `gofumpt` arruma-se com `go tool golangci-lint fmt ./...` **antes** do portão.
- ⚠️ **O `make verificar` não corre em PowerShell** (sintaxe POSIX no alvo `gerado`). Corre-se em Git Bash. O alvo `gerado` compara com o **committado**: gerar, committar, verificar.
- ⚠️ **No Git Bash, um caminho absoluto num comando `docker` é reescrito** — prefixar `MSYS_NO_PATHCONV=1`.
- ⚠️ **A porta 5432 desta máquina é de um PostgreSQL nativo**, não dos contentores (que estão na 55433). Ver com `docker port <contentor>`.
- ⚠️ Ler respostas em Python nesta máquina precisa de `PYTHONIOENCODING=utf-8`. **E o Python do Windows não lê caminhos `/c/...` do Git Bash** — usar `C:\...`.
- ⚠️ O `postgres:18` recusa o mount do v1: nas imagens 18+ é `/var/lib/postgresql`. E `pg_isready` sem `-h 127.0.0.1` dá pronto cedo demais.
- ⚠️ **Na app:** o `openapi-typescript` 7 rebenta com o TypeScript 7 — fica no `~5.9`. **O `expo start` reescreve o `tsconfig.json` sozinho** e tira-lhe o `.expo/types` do `include` — confirmar o `git diff` antes de commitar.
- ⚠️ **No `jest-expo` o `fetch` global não é o do Node** e devolve `undefined`. Um teste que queira falar com o servidor lê o JSON de ficheiro.

## Onde vive o quê

- **Tudo neste repositório.** ⚠️ Já não há Confluence — as páginas do espaço `GDP` ficaram e **não são a verdade**.
- **JIRA**, projecto `KAN` — o backlog, e é ele que manda. ⚠️ **Precisa de triagem depois da reversão**: sete issues ficam sem sentido e três ressuscitam.
- `../simulador-credito-habitacao` (v1) — ⚠️ **passou a ser referência de desenho, e não só técnica.** Ele **era** ao vivo. O que correu mal nele — estado em-processo, autenticação sem decisão, 50+ ficheiros ad-hoc, 338 erros de lint — **não vinha de ser ao vivo**, e essas partes do v2 ficam de pé.
- `../viabilidade-imobiliaria` — consome `/api/rate-catalog` em produção. ⚠️ **É agora um credor**, e não um consumidor tranquilo.
- `../simulador-v2-app` — o contrato chega lá por `npm run sincronizar-api`; os dois ficam commitados e o CI reprova divergência.
