# RESUME — simulador-v2

Estado actual e próximos passos. ⚠️ **Sem changelog** — o relato de sessões não vive aqui. O `RESUME.md` do v1 chegou a 1317 linhas antes de ser esvaziado à força.

**Actualizado:** 2026-08-02

⚠️ **A sonda está fundida** (PR [#70](https://github.com/zepedrorodrigues/simulador-v2/pull/70), merge `cb32c3c`) — o pacote `internal/aplicacao/sonda`, sem o subcomando ligado. **E a documentação também** (PR [#71](https://github.com/zepedrorodrigues/simulador-v2/pull/71), merge `35eb7a5`): o `development` tem os documentos todos.

## Onde estamos

**O ciclo fecha de ponta a ponta, e já se viu fechar na forma de produção.** Varre-se em hora morta, grava-se, lê-se, responde-se por HTTP, e a app mostra-o no ecrã.

**Cinco bancos:** CGD, Novo Banco, Montepio, Banco CTT, Santander. O varrimento custa 96 pontos por corrida mais ~86 amostras de escala por banco.

**Fidelidade:** 3002 ofertas conferidas em cada banco, **zero divergências** em 147 098 comparações, erro de leitura ≤ 0,100 % a 95 % de confiança. ⚠️ Não se lê como «acertámos 99,9 %»: o observado é zero, e os 0,100 % são o que uma amostra de 3002 permite excluir.

**Os cinco parsers continuam a corresponder ao que os bancos devolvem** (medido 2026-08-01). Nenhum banco mudou por baixo de nós.

**A app pede, mostra, e foi confirmada.** `simulador-v2-app`: Expo SDK 57, os três passos do pedido (A3) e a lista de ofertas com detalhe (A5).

## O ensaio da forma de produção (2026-08-01)

Correu-se em local o que um servidor vai correr: a imagem do `Dockerfile`, PostgreSQL **fechado**, migrações como passo próprio, Caddy com TLS à frente.

| | |
|---|---|
| imagem | 1m32s, **24,4 MB** |
| `migrar` como passo próprio | sai 0, e só então o `servir` arranca |
| chave de API fraca | **recusa arrancar**, e diz porquê |
| PostgreSQL alcançável de fora | **não** |
| `/api/rate-catalog` sem/com chave | 401 / 200 |
| base migrada e sem série | 503 `sem_varrimento` |
| varrimento ao vivo (2 bancos) | 57 observações de 49 pontos, **resíduo 0,00 € mediana e máximo** |
| `KAN-45` contra dados reais | **pediram-se 5, vieram 5** — 3 como `sem_serie`, nomeados |
| tecto por IP atrás do proxy | 429 ao pedido 60, `Retry-After: 60` |
| `pg_dump` → restauro em base nova | 57 → 57 observações |

⚠️ **O restauro foi EXECUTADO, não descrito** — a diferença entre uma cópia de segurança e a intenção de ter uma.

⚠️ **E o ensaio pagou-se:** encontrou a `KAN-46` e a `KAN-47`, nenhuma das quais qualquer suite apanharia.

## O que se mediu, e mudou o desenho

- **A TAN da fixa muda com o LTV:** `TAN_fixa(período, ltv) = base(período) + spread(ltv)`. O que se guarda é a **base** (`KAN-41`).
- **O spread da fase indexada sai só do LTV:** 1 210 de 1 210 observações exactas.
- **A TAEG deriva-se dos encargos ajustados**, identificáveis só a partir de **dois prazos**.
- **Sondar não custa o mesmo a todos:** 1,00 pedido HTTP por simulação no Banco CTT, 2,03 no Montepio, **4,00** no Santander. Argumento medido para reaproveitar config e catálogo dentro de uma corrida; **não está feito**.
- ⚠️ **Ligar o CORS partia o tecto por IP ao meio** — cada comparação da app custava dois do tecto e por `curl` custava um. Os preflights deixaram de contar.
- ⚠️ **O binário recusa servir uma base por migrar, e recusa arrancar com chave fraca.** As duas confirmadas no ensaio: são a §4 e a §6 a valerem por construção.
- ⚠️ **O rendimento mensal é usado por UM dos cinco bancos**, e mudou o passo 2 da app. ⚠️ **A data de nascimento é o caso contrário e fica como está:** a CGD não a pergunta e ela entra à mesma no prazo máximo. `usa: false` quer dizer «aquele simulador não pergunta isto», não «é irrelevante».

## O que está decidido

- **Go**, PostgreSQL, `chi`, `pgx`+`sqlc`, `goose`, OpenAPI à mão. ⚠️ **Sem Redis.**
- **Serve só JSON**, e por isso a app web é serviço à parte.
- **Varrer em hora morta, responder por cálculo local.** Um pedido, uma resposta.
- **Guarda-se o parâmetro, não o resultado.** Duas tabelas. Simulações de utilizador não são guardadas.
- ⚠️ **TAEG e MTIC são DERIVADOS** e vão com `pressupostos` obrigatórios.
- ⚠️ **Um preço sem data não é servido.** O rodapé da lista dá o `capturado_em` **mais antigo** — é afirmação sobre o conjunto.
- ⚠️ **Uma oferta ajustada não leva estrela de «melhor», e uma oferta sozinha também não.** A `KAN-45` não mexe nisto: o filtro por `sucesso` vem antes da contagem.
- ⚠️ **A resposta traz uma oferta por banco PEDIDO** (`KAN-45`). Lista vazia = todos os **conhecidos** (união do registo com a grelha). Um id que não é banco nenhum é 400 com `campo: "bancos"`.
- ⚠️ **Cinco códigos de erro por oferta:** `prazo_impossivel`, `produto_indisponivel` (varreu-se e não mede **este** cenário), `banco_indisponivel` (foi-se lá e não respondeu), `resposta_ilegivel`, `sem_serie` (**não se foi lá**). Um código novo não é mudança de versão: o campo é `type: string` sem enum.
- ⚠️ **A grelha confirma-se por sondagem barata** (§7, decisão 6). ~4 pedidos por banco contra 96. Uma sonda por degrau, logo **abaixo do `Ate`**: apanha fronteira que desce, falha a que sobe — e o erro que fica é servir o spread mais alto, que é a direcção que a MCD manda presumir. Tolerância **lida do degrau**, não escolhida. Na divergência: servir o antigo com fiabilidade reduzida **e** revarrer aquele banco, seguro só por causa do travão em Postgres. Fundida; falta ligar o subcomando.
- ⚠️ **ALOJAMENTO ADIADO** (2026-08-01), revogando o Fly de 28-07. Não é o fornecedor: é que o primeiro ensaio a sério encontrou dois defeitos numa tarde, e pôr no ar antes de saber o que mais está assim seria escolher a data em vez do estado.
- ⚠️ **A documentação voltou ao repositório** (2026-08-01), revogando o `652eda2`. O Confluence deixou de ser usado. A razão não foi o argumento — o código continua aberto de propósito — foi o custo: duas moradas sem sincronização produziram divergência a sério (485 contra 505 linhas no `ARQUITETURA.md`).
- ⚠️ **Os documentos foram compactados a 39 %** (3055 → 1845 linhas), com o critério: estado actual, decisões do passado que importem **e que o código não explique**, e futuro. **Todos os cortes grandes foram cópias de artefactos que já existem** — maquetas de ecrãs construídos, exemplos JSON de um esquema executável, a interface `Banco` copiada para dentro de um documento, medições repetidas em dois ficheiros. Nenhum foi prosa a mais. ⚠️ O `DOSSIE-BANCOS` fica intacto de propósito: 468 linhas de factos medidos banco a banco, nenhum no código e nenhum duplicado.
- ⚠️ **Os comentários do código seguem a mesma regra** (ver `CLAUDE.md`): o mais curto que se perceba, e só se for estritamente necessário. Se o porquê já vive num documento, **remete em vez de repetir** — a cópia da interface `Banco` no `CONTRATO-BANCO.md` tinha perdido os parágrafos do `ctx` sem ninguém dar por isso.

## Próximo passo

1. **Acabar os comentários do código.** Ficou o padrão e a regra (ver «Comentários» no `CLAUDE.md`), não o trabalho: são **4083 linhas de comentário para 8313 de código, 32%**, com **~140 blocos de 8+ linhas seguidas** por rever. Os maiores estão em `dominio/` (taeg, encargos, oferta, dinheiro), `grelha/` e `bancos/`. ⚠️ A pergunta a fazer a cada um é «isto sobrevive noutro sítio?», e agora a maioria sobrevive — os documentos ficaram compactos e precisos de propósito, primeiro.
2. `KAN-46` — os cabeçalhos de defesa que a `API.md` §3 prometia e ninguém emite. ⚠️ O documento já não promete; a falta continua.
3. **Ligar o subcomando da sonda:** ler a escala guardada, correr contra os bancos, e na divergência servir com fiabilidade reduzida e revarrer aquele banco.
4. **Confirmar a app contra o servidor já com a `KAN-45`** — a confirmação da A5 é anterior à correcção.
5. A app, A6 e A7. ⚠️ Ver o ramo `wip/estados-a6-descartado` antes de começar a A6.
6. `KAN-19` — Crédito Agrícola. ⚠️ O `reference_rate_value` é o **spread**, não a Euribor, apesar de o `rateIndexType` dizer `EUR12TM`.
7. **Só então, alojamento.**

## O que está por resolver

- ⚠️ `KAN-46` — acima. A `KAN-47` está feita.
- ⚠️ **O `PROXIES_DE_CONFIANCA` por medir.** Atrás de um proxy nosso deixa de ser medição e passa a valor conhecido; só é problema atrás da rede opaca de uma plataforma.
- ⚠️ **O domínio de LTV a varrer não sai do banco.** Hoje 30-100 % para todos; a CGD financia até 90 % na própria. É por isto que a app não afirma limites de LTV.
- ⚠️ **Se a relação da taxa fixa vale fora da CGD.** Mediu-se lá e **não se herda**.
- ⚠️ **Tensão na §4:** o `CHECK` exige TAEG numa linha de sucesso e a §4 diz que a que não se consegue dar se omite. Hoje não morde.
- **Registadas:** `KAN-25` (profissão), `KAN-26` (ordenar ofertas ajustadas), `KAN-30` (pânico nosso sai como `banco_indisponivel` — ficou mais fácil com o precedente do `sem_serie`), `KAN-34`, `KAN-36`, `KAN-37`, `KAN-38`, `KAN-46`.
- ⚠️ **Detalhe de voz:** o corpo do 429 diz «Tenta daqui a 1m0s» — **tu**, onde os `textos` da app usam **você**.
- ⚠️ **Bancos de browser** (`KAN-20`, `KAN-21`) e **perguntas jurídicas** (`KAN-24`, bloqueia as lojas e não a web).

## Lições

**Os documentos mentiam, e concordavam uns com os outros.** O `API.md` prometia `/api/v1/simulacoes`, que nunca existiu, e a §6 do `ARQUITETURA` repetia-lhe o nome. ⚠️ **O portão não apanha isto por construção** — compara o *gerado* com o spec, não os *documentos* nem o *servido*.

**E uma remissão falsa sobrevive a ser citada.** O cabeçalho do cartesiano mandava, desde que existe, aplicar «a §7 do `USO-RESPONSAVEL.md`» — documento que vai só até à §4 e **nunca teve §7**. A `KAN-47` copiou a frase para dentro de si ao descrever o defeito, e nem assim se viu. ⚠️ Uma remissão por número não avisa quando o alvo não existe; se apontasse ao **título** da secção, um `grep` encontrava-a.

**Um desenho pode ficar errado sem ninguém lhe tocar.** O `ECRAS.md` esteve atrás da inversão da §1 três dias. Nenhuma dessas falhas partia um teste.

**Um teste que passa com as duas implementações não prova nenhuma.** Trocar o `eFalhaDaApi` por `instanceof` deixou a suite a passar. ⚠️ Nove reversões falharam e a décima passou — foi **a que passou** que encontrou o defeito.

**Os tipos garantem a forma, não que se leia bem o que lá está.** O `ate_mes` é `integer`: acumulado ou duração não se vê em tipo nenhum. Um `sort` com `NaN` devolve ordem arbitrária que parece ordenada. ⚠️ Nenhum dá erro.

**Um teste cujos dois lados se constroem do mesmo sítio não vê a diferença entre eles.** É a `KAN-45`: os testes construíam catálogo e pedido com o mesmo conjunto de bancos, e a diferença entre «medido» e «pedido» não tinha por onde aparecer. Só se via numa base nova, num banco novo, ou num banco cujo varrimento falhou — ⚠️ **os três momentos em que a app é mais vista.**

**Correr a forma de produção é um teste, e encontra o que nenhuma suite encontra.** Duas tardes de trabalho de servidor não apanharam nem os cabeçalhos em falta nem um alvo de `make` que não pode acabar verde.

## Notas de máquina

- Lint por `go tool golangci-lint run ./...` — o do `PATH` é v1.64.8 e não lê o `.golangci.yml` v2. O `gofumpt` arruma-se com `go tool golangci-lint fmt ./...` **antes** do portão.
- ⚠️ **O `make verificar` não corre em PowerShell** (sintaxe POSIX no alvo `gerado`). Corre-se em Git Bash. O alvo `gerado` compara com o **committado**: gerar, committar, verificar.
- ⚠️ **No Git Bash, um caminho absoluto num comando `docker` é reescrito** — `/simulador` vira `C:/Program Files/Git/simulador`. Prefixar `MSYS_NO_PATHCONV=1`.
- ⚠️ **A porta 5432 desta máquina é de um PostgreSQL nativo**, não dos contentores. Testar «a base está exposta?» pelo porto do host dá falso positivo — ver com `docker port <contentor>`.
- ⚠️ Ler respostas em Python nesta máquina precisa de `PYTHONIOENCODING=utf-8`, senão os acentos saem como `?` e parece corrupção de dados. ⚠️ E uma barra invertida dentro de um heredoc `<<'PY'` é comida antes de o Python a ver — `f.replace(os.sep, "/")` em vez de escapar à mão, ou o script num ficheiro.
- ⚠️ O `postgres:18` recusa o mount do v1: nas imagens 18+ é `/var/lib/postgresql`. E `pg_isready` sem `-h 127.0.0.1` dá pronto cedo demais.
- ⚠️ **Os alvos de rede são três desde a `KAN-47`**, separados pelo que custam a terceiros: `teste-rede` (parsers, dezenas de pedidos, **108 s**), `teste-fidelidade` (~2000 pedidos, 250 amostras por banco, até 3h) e `medicao` (o cartesiano e o e2e, >1000 pedidos só à CGD, 1h+). Ver a tabela no `CLAUDE.md`.
- ⚠️ **O `teste-rede` encolheu porque deixou de correr o que não é dele** (medido 2026-08-02, contra 2026-08-01): montepio 530→25 s, bancoctt 185→10 s, cgd 147→6 s, santander 119→7 s, novobanco 65→6 s, e o `varrimento` deixou de **falhar aos 601 s** para passar em 10 s.
- ⚠️ **Na app:** o `openapi-typescript` 7 rebenta com o TypeScript 7 — fica no `~5.9`. O `@testing-library/react-native` traz matchers embutidos desde a v12.4; apontar-lhe o `extend-expect` faz o Jest recusar arrancar. O `expo start` reescreve o `tsconfig.json` sozinho — confirmar o `git diff` antes de commitar.
- ⚠️ O `flyctl` está instalado (winget, v0.4.71) sem sessão. Ficou de um alojamento adiado; não é compromisso.

## Onde vive o quê

- **Tudo neste repositório**, desde 2026-08-01. `CLAUDE.md`, `PLAN.md`, `RESUME.md` e `docs/` voltaram a ser versionados. ⚠️ **Já não há Confluence** — as páginas do espaço `GDP` ficaram e não são a verdade.
- **JIRA**, projecto `KAN` — o backlog, e é ele que manda.
- `../simulador-credito-habitacao` (v1) — referência técnica sobre os bancos, não de arquitectura.
- `../viabilidade-imobiliaria` — consome `/api/rate-catalog` em produção, fronteira congelada com teste de contrato (`KAN-42`).
- `../simulador-v2-app` — o contrato chega lá por `npm run sincronizar-api`; os dois ficam commitados e o CI reprova divergência.
