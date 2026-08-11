# CLAUDE.md — simulador-v2

Reescrita em **Go** do `simulador-credito-habitacao`. Serve **só JSON**; a interface é uma app React Native noutro repositório.

⚠️ **A §1 foi revertida a 2026-08-06: o pedido do cliente volta a ir ao banco.** Não há varrimento, não há grelha, não há modelo de preço nosso. O porquê está em `docs/DECISAO-AO-VIVO.md`, e a razão curta é que guardar uma cópia do modelo de preço de cada banco obriga a acertar em como eles preçam — e num só dia de confronto com dados reais falhámos isso quatro vezes (`KAN-54` a `KAN-57`).

✅ **A Fase 6 está feita (2026-08-07), e o desenho antigo saiu do repositório.** Durante duas semanas os documentos descreviam para onde se ia e o código de onde se vinha; essa distância acabou. ⚠️ A regra de desempate **mantém-se** para o que vier: quando um documento e o código discordarem, é o código que está por mudar — excepto quando o documento afirma um **facto sobre o mundo**, e aí verifica-se o facto. Foi assim que a D1 caiu.

As convenções da casa (pt-PT, branches, commits, labels, verificação por reversão) estão em `~/.claude/convencoes-repos.md` — não se repetem aqui.

## Antes de mudar seja o que for

**Ler o** `docs/ARQUITETURA.md`. É vinculativo, não descritivo. Se o que vais fazer não cabe no âmbito de §1, ou viola a regra de dependência de §3, ou acrescenta uma tabela que não está em §4 — **altera-se o documento primeiro, com justificação**, e só depois o código. Foi por acrescento não decidido que o v1 ganhou autenticação e depois teve de a apagar em três migrações.

## Comandos

```
make dev          # sobe o PostgreSQL em contentor
make gerar        # sqlc + oapi-codegen — nunca editar código gerado à mão
make verificar    # o portão completo (ver abaixo)
```

⚠️ **Todo o caminho do cliente fala com bancos.** Deixou de haver «alvos de rede» à parte, porque a rede deixou de ser a excepção — ver `docs/ARQUITETURA.md` §7.

⚠️ **E o custo passou a ser por cliente, não por corrida.** Pedidos HTTP por simulação, medidos: **1,00** no Banco CTT, **2,03** no Montepio, **4,00** no Santander. Uma comparação a cinco bancos custa **~10 pedidos a terceiros**. Reaproveitar configuração e catálogo dentro de um pedido deixou de ser optimização e passou a ser defesa.

⚠️ **Somos um amplificador.** Um pedido nosso vira ~10 aos bancos, com origem aparente nossa. O tecto por IP é **estrutural** e o `PROXIES_DE_CONFIANCA` é **bloqueante** — sem ele medido, ou o tecto é contornável, ou é o tecto do site inteiro (o bug de produção do v1).

✅ **Os subcomandos `varrer` e `sondar` desapareceram** (2026-08-07, Fase 6 passo 5), com o `medicao` do Makefile. Sobram três: `servir`, `migrar`, `reverter`. ⚠️ Os alvos de rede que ficam — `teste-rede`, `teste-fidelidade`, `latencia` — continuam a custar pedidos a terceiros e não se correm sem razão.

O portão são cinco coisas, e passa-se o portão **inteiro**:

```
golangci-lint run ./...       # o repositório TODO, não um subconjunto
go test -race ./...           # o -race não é opcional: o modelo é fan-out concorrente
```

⚠️ **O `make verificar` não corre em PowerShell**: o alvo `gerado` usa `test -z ... || { ... }`, sintaxe POSIX que o `cmd` não percebe («'test' is not recognized»). Corre-se em Git Bash. E o que o `gofumpt` reprovar arruma-se com `go tool golangci-lint fmt ./...`.

## Estrutura

```
cmd/simulador/     o binário
internal/dominio/  tipos e regras puras. Não importa mais nada do projecto.
internal/bancos/   um pacote por banco + as 4 estratégias de transporte
internal/aplicacao/aovivo: o caso de uso — pedir a UM banco, com vaga. Sem HTTP, sem SQL.
internal/infra/    chi, pgx/sqlc, tecto por banco, tecto por IP, cache, config
api/openapi.yaml   o contrato — fonte da verdade dos tipos Go e TypeScript
db/                migrações goose + queries sqlc
```

Dependências só para dentro: `dominio ← bancos ← aplicacao ← infra ← cmd`. Imposto pelo `depguard`, não pela boa vontade. ⚠️ E o `api/` é folha: **só** a `infra` o consome. Essa aresta passou a estar imposta na KAN-29 — até aí estava prometida e não travava.

## Comentários

**O mais curto que se perceba, e só se for estritamente necessário para se perceber.** Sem nota, se o código se explica.

Um comentário ganha o seu lugar quando diz o que o código não pode dizer: um **número medido**, uma **base legal**, ou **porque é que a alternativa óbvia está errada**. Fora disso não escreve.

⚠️ E se o porquê já vive num documento, o comentário **remete** em vez de repetir — uma cópia diverge do original em silêncio, e já aconteceu aqui: a interface `Banco` copiada para o `CONTRATO-BANCO.md` perdeu os parágrafos do `ctx` sem ninguém dar por isso.

## Armadilhas

- **Não editar código gerado.** O do `sqlc` e o do `oapi-codegen` são reconstruídos; edições à mão desaparecem no `make gerar` seguinte.
- **Não escrever um banco sem captura primeiro.** A ordem está em `docs/CONTRATO-BANCO.md` §3: capturar → parser contra a captura → payload → só então ligar a rede. E ver o teste **falhar** antes de o pôr a passar.
- ✅ **A** `/api/rate-catalog` **saiu** (2026-08-07), e a D1 fechou-se assim: o `viabilidade-imobiliaria` consome a do **v1** (`simulador-credito-habitacao`, o `chmonitor`), que mantém os seus scrapers e **não é tocado**. A daqui era uma reimplementação congelada que nunca teve consumidor — este serviço não está alojado. ⚠️ **Dizia-se aqui, e em mais quatro documentos, que ele a lia «em produção»** — era falso nos dois sentidos, e foi verificado a 2026-08-07 no README e no `CLAUDE.md` do outro repositório. ⚠️ Um dia o v2 responderá às perguntas do `viabilidade`, mas **nunca por esta rota**: ela publica uma série temporal, e sem varrimento não há como a produzir.
- **Nada de estado em-processo** para cache ou tecto. Vive em **Postgres**: `internal/infra/lotacao` (2 vagas por banco), `internal/infra/limites` (tecto por IP) e `internal/infra/cache` (respostas, 5 min). ⚠️ O `infra/travao` era um **fecho** contra dois varrimentos do mesmo banco e **saiu com ele**: ao vivo, dois clientes a perguntar pelo mesmo banco é o normal, e o que se limita é quantos de cada vez. O v1 tinha-o em memória e avisava no arranque que com mais de um worker o mesmo banco levava N scrapes em paralelo. ⚠️ Dizia aqui «Vive em Redis», e o Redis saiu do desenho na §7 — e do ambiente na KAN-39. Não volta sem uma entrada nova na §7.
- **Sem pasta de scripts.** Foi onde o v1 acumulou 50+ ficheiros ad-hoc e 338 erros de lint permanentes que cegaram o portão. Trabalho de sondagem vive numa branch e não é submetido, ou vira um teste.
- **Nada de dados pessoais nem segredos nas capturas** — o repositório é público. Titular fictício; cookies, tokens e cabeçalhos de autenticação removidos.
- **Confirmar o** `git branch --show-current` **antes de commitar.** O `main` só recebe merges de `development`.

## Onde está o resto

`docs/DOSSIE-BANCOS.md` (o que o v1 apurou sobre cada banco), `docs/API.md`, `docs/DEPLOY.md` (a forma de produção: VPS, Caddy, Postgres na mesma máquina), `docs/ECRAS.md`, `docs/CONTRATO-BANCO.md`, `docs/APP.md` — este último traz as fases da app e a restrição que ela impõe ao contrato.

**O backlog é o projecto** `KAN` **do JIRA** (`jpnmsr.atlassian.net`), não as issues do GitHub — essas ficam como arquivo e não se abrem mais.

⚠️ **Tudo isto é versionado aqui, desde 2026-08-01** — o `CLAUDE.md`, o `PLAN.md`, o `RESUME.md` e os `docs/`. Revoga o `652eda2`, que os tinha mandado para um Confluence privado com o argumento «o código é aberto de propósito, o planeamento não». O argumento não caiu; caiu o custo de o cumprir: duas moradas sem sincronização divergiram a sério — medido no dia, o `ARQUITETURA.md` tinha 485 linhas num lado e 505 no outro, e nenhuma linha de conteúdo se perdeu por sorte e não por desenho.

⚠️ **O Confluence deixou de ser usado.** As páginas do espaço `GDP` ficaram lá e **não são a verdade** — não se lêem nem se actualizam.

⚠️ **O `RESUME.md` é o estado da sessão e lê-se aqui.** Actualizá-lo faz parte de fechar trabalho, mantendo-o curto: estado actual e próximos passos, **sem changelog**.

O v1 fica em `../simulador-credito-habitacao` e continua a ser a referência técnica sobre os bancos. ⚠️ Não é referência de arquitectura — é precisamente o que esta reescrita existe para não repetir.
