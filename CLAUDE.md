# CLAUDE.md — simulador-v2

Reescrita em **Go** do `simulador-credito-habitacao`. Serve **só JSON**; a interface é uma app React Native noutro repositório.

As convenções da casa (pt-PT, branches, commits, labels, verificação por reversão) estão em `~/.claude/convencoes-repos.md` — não se repetem aqui.

## Antes de mudar seja o que for

**Ler o** `docs/ARQUITETURA.md`. É vinculativo, não descritivo. Se o que vais fazer não cabe no âmbito de §1, ou viola a regra de dependência de §3, ou acrescenta uma tabela que não está em §4 — **altera-se o documento primeiro, com justificação**, e só depois o código. Foi por acrescento não decidido que o v1 ganhou autenticação e depois teve de a apagar em três migrações.

## Comandos

```
make dev          # sobe o PostgreSQL em contentor
make gerar        # sqlc + oapi-codegen — nunca editar código gerado à mão
make verificar    # o portão completo (ver abaixo)
```

⚠️ **Os alvos que falam com os bancos são três, e escolhem-se pelo que custam a
terceiros** — não por «toca ou não toca na rede». Nenhum entra no portão.

| alvo | pedidos a terceiros | duração | quando |
|---|---|---|---|
| `make teste-rede` | dezenas, por cinco bancos | minutos | confirmar que um parser ainda corresponde ao que o banco devolve |
| `make teste-fidelidade` | **~2000** (250 amostras por banco) | até 3h | confirmar que nada se perde entre o corpo que chega e a `dominio.Oferta` |
| `make medicao` | **>1000, todos à CGD** | 1h+ | o cartesiano e o e2e da grelha — perguntas de desenho, não de parser |

⚠️ **`make medicao` e `make teste-fidelidade` correm-se em hora morta, e a hora
escolhe-se antes de os disparar.** Aplica-se-lhes o «Reduzir a carga nos bancos
ao mínimo que funciona». Até 2026-08-02 os três
viviam sob a mesma tag `rede`, e pedir uma confirmação de parser disparava mil
pedidos à CGD (KAN-47).

⚠️ **E o binário também fala com os bancos, pelo mesmo critério.** Dois
subcomandos, dois custos:

| subcomando | pedidos a terceiros | quando |
|---|---|---|
| `simulador varrer` | **~96 por banco** (~480 pelos cinco) | reconstruir a grelha; corre em hora morta, com guarda `--se-antigo` de 6h |
| `simulador sondar` | **~4 por banco** (~20 pelos cinco) | confirmar que a grelha ainda descreve o banco (KAN-48) |

A sonda é **vinte e quatro vezes mais barata** do que o varrimento, e é isso que
a torna corrível de hora a hora. ⚠️ **Mas na divergência ela revarre aquele
banco** — logo uma corrida que encontre um preçário mudado custa os ~96 desse
banco. Para sondar sem pagar o revarrimento: `simulador sondar --sem-revarrer`. ⚠️ **Não é «ver sem mexer»** desde a KAN-49: o veredicto é gravado à mesma, e uma divergência passa a aparecer nas ofertas como `em_duvida`. O que o sinalizador poupa são os ~96 pedidos ao banco, não a declaração — medir e não contar a ninguém é o defeito que a KAN-49 corrige.

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
internal/aplicacao/casos de uso. Sem HTTP, sem SQL.
internal/infra/    chi, pgx/sqlc, travão por banco, config
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
- ⚠️ **A** `/api/rate-catalog` **está congelada.** Campos em inglês e `snake_case`, ao contrário de todo o resto do repositório. O `viabilidade-imobiliaria` lê-a em produção; mudar o formato parte-o sem aviso. Há um teste de contrato — se ele falha, o erro é teu, não dele.
- **Nada de estado em-processo** para dedup ou *gate* por banco. Vive em **Postgres**, no `internal/infra/travao` (`pg_try_advisory_lock`). O v1 tinha-o em memória e avisava no arranque que com mais de um worker o mesmo banco levava N scrapes em paralelo. ⚠️ Dizia aqui «Vive em Redis», e o Redis saiu do desenho na §7 — e do ambiente na KAN-39. Não volta sem uma entrada nova na §7.
- **Sem pasta de scripts.** Foi onde o v1 acumulou 50+ ficheiros ad-hoc e 338 erros de lint permanentes que cegaram o portão. Trabalho de sondagem vive numa branch e não é submetido, ou vira um teste.
- **Nada de dados pessoais nem segredos nas capturas** — o repositório é público. Titular fictício; cookies, tokens e cabeçalhos de autenticação removidos.
- **Confirmar o** `git branch --show-current` **antes de commitar.** O `main` só recebe merges de `development`.

## Onde está o resto

`docs/DOSSIE-BANCOS.md` (o que o v1 apurou sobre cada banco), `docs/API.md`, `docs/ECRAS.md`, `docs/CONTRATO-BANCO.md`, `docs/APP.md` — este último separa o que já está decidido tecnicamente do que exige parecer jurídico antes de publicar nas lojas.

**O backlog é o projecto** `KAN` **do JIRA** (`jpnmsr.atlassian.net`), não as issues do GitHub — essas ficam como arquivo e não se abrem mais.

⚠️ **Tudo isto é versionado aqui, desde 2026-08-01** — o `CLAUDE.md`, o `PLAN.md`, o `RESUME.md` e os `docs/`. Revoga o `652eda2`, que os tinha mandado para um Confluence privado com o argumento «o código é aberto de propósito, o planeamento não». O argumento não caiu; caiu o custo de o cumprir: duas moradas sem sincronização divergiram a sério — medido no dia, o `ARQUITETURA.md` tinha 485 linhas num lado e 505 no outro, e nenhuma linha de conteúdo se perdeu por sorte e não por desenho.

⚠️ **O Confluence deixou de ser usado.** As páginas do espaço `GDP` ficaram lá e **não são a verdade** — não se lêem nem se actualizam.

⚠️ **O `RESUME.md` é o estado da sessão e lê-se aqui.** Actualizá-lo faz parte de fechar trabalho, mantendo-o curto: estado actual e próximos passos, **sem changelog**.

O v1 fica em `../simulador-credito-habitacao` e continua a ser a referência técnica sobre os bancos. ⚠️ Não é referência de arquitectura — é precisamente o que esta reescrita existe para não repetir.
