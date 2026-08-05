# RESUME — simulador-v2

Estado actual e próximos passos. ⚠️ **Sem changelog** — o relato de sessões não vive aqui. O `RESUME.md` do v1 chegou a 1317 linhas antes de ser esvaziado à força.

**Actualizado:** 2026-08-05

⚠️ **A sonda corre de ponta a ponta e já se declara** (`KAN-48` + `KAN-49`): lê a escala guardada, mede ~4 pontos por banco, na divergência revarre **aquele** banco, grava o veredicto e a oferta sai com `fiabilidade`. A §7 decisão 6 está executada por inteiro.

⚠️ **E esse revarrimento apagava os outros bancos da resposta** — corrigido a 2026-08-05 (`KAN-50`). A leitura servia o último `varrimento_id` e o revarrimento cunhava um id novo com um banco lá dentro; medido contra Postgres, um varrimento completo dava `map[cgd:6 novobanco:1]` e revarrer só a CGD dava `map[cgd:6]`. **A série passa a compor-se por banco**, e a guarda da viragem do dia da §7.3 passou a existir em código em vez de ser só uma frase.

## Onde estamos

**O ciclo fecha de ponta a ponta, e já se viu fechar na forma de produção.** Varre-se em hora morta, grava-se, lê-se, responde-se por HTTP, e a app mostra-o no ecrã.

**Cinco bancos:** CGD, Novo Banco, Montepio, Banco CTT, Santander. O varrimento custa 96 pontos por corrida mais ~86 amostras de escala por banco.

**Fidelidade:** 3002 ofertas conferidas em cada banco, **zero divergências** em 147 098 comparações, erro de leitura ≤ 0,100 % a 95 % de confiança. ⚠️ Não se lê como «acertámos 99,9 %»: o observado é zero, e os 0,100 % são o que uma amostra de 3002 permite excluir.

**Os cinco parsers continuam a corresponder ao que os bancos devolvem** (medido 2026-08-01). Nenhum banco mudou por baixo de nós.

**A app pede, mostra, e foi confirmada.** `simulador-v2-app`: Expo SDK 57, os três passos do pedido (A3) e a lista de ofertas com detalhe (A5).

## O ensaio da KAN-49/KAN-50 (2026-08-05)

Correu-se o ciclo contra Postgres em contentor, com o binário a sério e a série semeada à mão — **sem varrimento**, portanto sem carga em bancos. O que se foi confrontar é o que **sai no JSON**, que é o que nenhum teste do portão vê.

| | |
|---|---|
| `simulador migrar` cria a `sondagens` | sim, na base a sério |
| oferta de banco com sonda divergente | `fiabilidade: "em_duvida"` **e a nota no `notas`** |
| oferta de banco com sonda confirmada | `fiabilidade: "confirmada"`, sem nota |
| revarrer só a CGD | o Novo Banco **fica** (era o defeito da `KAN-50`) |
| a dúvida depois do revarrimento | volta a `por_confirmar` — **expira sozinha**, como desenhado |
| banco varrido antes da viragem do dia | `serie_desactualizada`, com a frase em pt-PT |
| `simulador sondar --bancos cgd --sem-revarrer` | **1 pedido** à CGD; detectou, gravou na `sondagens`, e a oferta passou a `em_duvida` |

⚠️ **O que este ensaio NÃO confirmou: a app.** Confrontou-se o servidor, não os ecrãs — a app continua confirmada só a 2026-07-29, e é o passo 2.

⚠️ E a divergência que a sonda encontrou era **da grelha semeada**, não do banco: a CGD respondeu 1,350 onde a série inventada dizia 2,250. Serviu para exercitar o caminho, e não é medição sobre a CGD.

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
- ⚠️ **A fiabilidade é declarada por OFERTA** (`KAN-49`, 2026-08-05): três estados, e só o `em_duvida` se mostra. Deriva-se de duas datas — a última sondagem e o último varrimento daquele banco — e por isso **expira sozinha**, sem prazo escolhido. Trouxe a **terceira tabela** (`sondagens`), com a entrada na §4 que o travão das duas exige.
- ⚠️ **A série serve-se por BANCO** (`KAN-50`, 2026-08-05), revogando «uma resposta mistura-se de um varrimento só»: de cada banco, o varrimento mais recente em que teve sucesso. Um banco do outro lado da viragem do dia sai com `serie_desactualizada` — código novo, e a §4 do `API.md` permite-o sem versão nova.
- ⚠️ **A resposta traz uma oferta por banco PEDIDO** (`KAN-45`). Lista vazia = todos os **conhecidos** (união do registo com a grelha). Um id que não é banco nenhum é 400 com `campo: "bancos"`.
- ⚠️ **Cinco códigos de erro por oferta:** `prazo_impossivel`, `produto_indisponivel` (varreu-se e não mede **este** cenário), `banco_indisponivel` (foi-se lá e não respondeu), `resposta_ilegivel`, `sem_serie` (**não se foi lá**). Um código novo não é mudança de versão: o campo é `type: string` sem enum.
- ⚠️ **A grelha confirma-se por sondagem barata** (§7, decisão 6; `simulador sondar`, `KAN-48`). ~4 pedidos por banco contra 96. Uma sonda por degrau, logo **abaixo do `Ate`**: apanha fronteira que desce, falha a que sobe — e o erro que fica é servir o spread mais alto, que é a direcção que a MCD manda presumir. Tolerância **lida do degrau**, não escolhida. Na divergência: servir o antigo com fiabilidade reduzida **e** revarrer aquele banco, seguro só por causa do travão em Postgres. ⚠️ Falta a **fiabilidade declarada** na resposta (`KAN-49`): hoje detecta-se e revarre-se, e quem lê a oferta não sabe que ela esteve em dúvida.
- ⚠️ **ALOJAMENTO ADIADO** (2026-08-01), revogando o Fly de 28-07. Não é o fornecedor: é que o primeiro ensaio a sério encontrou dois defeitos numa tarde, e pôr no ar antes de saber o que mais está assim seria escolher a data em vez do estado.
- ⚠️ **A documentação voltou ao repositório** (2026-08-01), revogando o `652eda2`. O Confluence deixou de ser usado. A razão não foi o argumento — o código continua aberto de propósito — foi o custo: duas moradas sem sincronização produziram divergência a sério (485 contra 505 linhas no `ARQUITETURA.md`).
- ⚠️ **Os documentos foram compactados a 39 %** (3055 → 1845 linhas), com o critério: estado actual, decisões do passado que importem **e que o código não explique**, e futuro. **Todos os cortes grandes foram cópias de artefactos que já existem** — maquetas de ecrãs construídos, exemplos JSON de um esquema executável, a interface `Banco` copiada para dentro de um documento, medições repetidas em dois ficheiros. Nenhum foi prosa a mais. ⚠️ O `DOSSIE-BANCOS` fica intacto de propósito: 468 linhas de factos medidos banco a banco, nenhum no código e nenhum duplicado.
- ⚠️ **Os comentários do código seguem a mesma regra** (ver `CLAUDE.md`): o mais curto que se perceba, e só se for estritamente necessário. Se o porquê já vive num documento, **remete em vez de repetir** — a cópia da interface `Banco` no `CONTRATO-BANCO.md` tinha perdido os parágrafos do `ctx` sem ninguém dar por isso.

## Próximo passo

1. **Acabar os comentários do código.** Ficou o padrão e a regra (ver «Comentários» no `CLAUDE.md`), não o trabalho: são **4506 linhas de comentário para 15 708 linhas não-teste, 29%**, com **148 blocos de 8+ linhas seguidas** por rever (medido 2026-08-05; os 4083/~140 anteriores são de antes de a sonda entrar). Os maiores estão em `dominio/` (taeg, encargos, oferta, dinheiro), `grelha/` e `bancos/`. ⚠️ A pergunta a fazer a cada um é «isto sobrevive noutro sítio?», e agora a maioria sobrevive — os documentos ficaram compactos e precisos de propósito, primeiro.
2. **Confirmar a app contra o servidor** — a confirmação da A5 (2026-07-29) é anterior à `KAN-45` **e** à `KAN-50`, as duas que mudaram quem aparece na lista.
3. A app, A6 e A7. ⚠️ Ver o ramo `wip/estados-a6-descartado` antes de começar a A6.
4. `KAN-19` — Crédito Agrícola. ⚠️ O `reference_rate_value` é o **spread**, não a Euribor, apesar de o `rateIndexType` dizer `EUR12TM`.
5. **Só então, alojamento.**

## O que está por resolver

- ⚠️ **O tamanho da imagem está escrito em dois sítios com dois valores:** 24,3 MB no `README.md`, 24,4 MB na tabela do ensaio aqui em cima. Um dos dois não foi medido na corrida que diz medir. Não se escolheu nenhum — mede-se e escreve-se o mesmo número nos dois.
- ⚠️ **O `PROXIES_DE_CONFIANCA` continua a decidir uma coisa:** enquanto não estiver medido, o HSTS não sai do serviço — fica no proxy, que é quem termina o TLS (`KAN-46`).
- ⚠️ **O `PROXIES_DE_CONFIANCA` por medir.** Atrás de um proxy nosso deixa de ser medição e passa a valor conhecido; só é problema atrás da rede opaca de uma plataforma.
- ⚠️ **O domínio de LTV a varrer não sai do banco.** Hoje 30-100 % para todos; a CGD financia até 90 % na própria. É por isto que a app não afirma limites de LTV.
- ⚠️ **Os degraus da CGD abaixo dos 32 % nunca foram varridos.** Sabe-se que há pelo menos uma fronteira em (33,00 ; 33,50]; abaixo disso não há medição. Ficou fora de âmbito da `KAN-35` e está registado no `DOSSIE-BANCOS.md`.
- ⚠️ **Se a relação da taxa fixa vale fora da CGD.** Mediu-se lá e **não se herda**.
- ⚠️ **Tensão na §4:** o `CHECK` exige TAEG numa linha de sucesso e a §4 diz que a que não se consegue dar se omite. Hoje não morde.
- **Registadas:** `KAN-25` (profissão), `KAN-26` (ordenar ofertas ajustadas), `KAN-30` (pânico nosso sai como `banco_indisponivel` — ficou mais fácil com o precedente do `sem_serie`), `KAN-34`, `KAN-36`, `KAN-37`, `KAN-38`.
- ⚠️ **Detalhe de voz:** o corpo do 429 diz «Tenta daqui a 1m0s» — **tu**, onde os `textos` da app usam **você**.
- ⚠️ **Bancos de browser** (`KAN-20`, `KAN-21`) e **perguntas jurídicas** (`KAN-24`, bloqueia as lojas e não a web).

## Lições

**Um PR pode dizer que fecha uma issue e não a fundir.** O #72 anunciava a `KAN-48` no título e trazia-lhe o corpo inteiro — tabela de reversões incluída —, mas o merge levou como cabeça o commit **anterior** ao subcomando, que foi escrito dezasseis minutos depois e empurrado para um ramo já fechado. Ficaram no `development` o `CLAUDE.md` a anunciar `simulador sondar` numa tabela de custos e o binário sem o subcomando. ⚠️ **Nada disto dá erro:** o portão passa (o código que falta não é importado por ninguém), o CI passa, e o JIRA fica com a issue fechada. Encontrou-se por se ler a **árvore** — `git ls-tree origin/development internal/infra/` — e não os documentos. Corrigido pelo #73. ⚠️ **O que o PR diz que fez e o que o merge levou são duas afirmações diferentes**, e só a segunda é verificável.

**Os documentos mentiam, e concordavam uns com os outros.** O `API.md` prometia `/api/v1/simulacoes`, que nunca existiu, e a §6 do `ARQUITETURA` repetia-lhe o nome. ⚠️ **O portão não apanha isto por construção** — compara o *gerado* com o spec, não os *documentos* nem o *servido*.

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
