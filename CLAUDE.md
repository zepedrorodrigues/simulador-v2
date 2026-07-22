# CLAUDE.md — simulador-v2

Reescrita em **Go** do `simulador-credito-habitacao`. Serve **só JSON**; a
interface é uma app React Native noutro repositório.

As convenções da casa (pt-PT, branches, commits, labels, verificação por
reversão) estão em `~/.claude/convencoes-repos.md` — não se repetem aqui.

## Antes de mudar seja o que for

**Ler [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md).** É vinculativo, não
descritivo. Se o que vais fazer não cabe no âmbito de §1, ou viola a regra de
dependência de §3, ou acrescenta uma tabela que não está em §4 — **altera-se o
documento primeiro, com justificação**, e só depois o código. Foi por acrescento
não decidido que o v1 ganhou autenticação e depois teve de a apagar em três
migrações.

## Comandos

```
make dev          # sobe PostgreSQL e Redis em contentor
make gerar        # sqlc + oapi-codegen — nunca editar código gerado à mão
make verificar    # o portão completo (ver abaixo)
```

O portão são cinco coisas, e passa-se o portão **inteiro**:

```
golangci-lint run ./...       # o repositório TODO, não um subconjunto
go test -race ./...           # o -race não é opcional: o modelo é fan-out concorrente
```

## Estrutura

```
cmd/simulador/     o binário
internal/dominio/  tipos e regras puras. Não importa mais nada do projecto.
internal/bancos/   um pacote por banco + as 4 estratégias de transporte
internal/aplicacao/casos de uso. Sem HTTP, sem SQL.
internal/infra/    chi, pgx/sqlc, redis, config
api/openapi.yaml   o contrato — fonte da verdade dos tipos Go e TypeScript
db/                migrações goose + queries sqlc
```

Dependências só para dentro: `dominio ← bancos ← aplicacao ← infra ← cmd`.
Imposto pelo `depguard`, não pela boa vontade.

## Armadilhas

- **Não editar código gerado.** O do `sqlc` e o do `oapi-codegen` são
  reconstruídos; edições à mão desaparecem no `make gerar` seguinte.
- **Não escrever um banco sem captura primeiro.** A ordem está em
  `docs/CONTRATO-BANCO.md` §3: capturar → parser contra a captura → payload →
  só então ligar a rede. E ver o teste **falhar** antes de o pôr a passar.
- **⚠️ `/api/rate-catalog` está congelada.** Campos em inglês e `snake_case`, ao
  contrário de todo o resto do repositório. O `viabilidade-imobiliaria` lê-a em
  produção; mudar o formato parte-o sem aviso. Há um teste de contrato — se ele
  falha, o erro é teu, não dele.
- **Nada de estado em-processo** para cache, dedup ou *gate* por banco. Vive em
  Redis. O v1 tinha-o em memória e avisava no arranque que com mais de um worker
  o mesmo banco levava N scrapes em paralelo.
- **Sem pasta de scripts.** Foi onde o v1 acumulou 50+ ficheiros ad-hoc e 338
  erros de lint permanentes que cegaram o portão. Trabalho de sondagem vive numa
  branch e não é submetido, ou vira um teste.
- **Nada de dados pessoais nem segredos nas capturas** — o repositório é público.
  Titular fictício; cookies, tokens e cabeçalhos de autenticação removidos.
- **Confirmar `git branch --show-current` antes de commitar.** O `main` só recebe
  merges de `development`.

## Onde está o resto

`PLAN.md` (fases), `docs/DOSSIE-BANCOS.md` (o que o v1 apurou sobre cada banco),
`docs/API.md`, `docs/ECRAS.md`, e o backlog nas issues.

O v1 fica em `../simulador-credito-habitacao` e continua a ser a referência
técnica sobre os bancos. ⚠️ Não é referência de arquitectura — é precisamente o
que esta reescrita existe para não repetir.
