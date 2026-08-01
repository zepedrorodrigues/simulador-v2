# Arquitectura — simulador-v2

Este documento é a decisão de desenho. É vinculativo: quando o código diverge dele, um dos dois está errado e resolve-se **antes** de continuar.

**Stack:** Go · PostgreSQL · `chi` · `pgx` + `sqlc` · OpenAPI. A interface com pessoas é uma app **React Native**, em repositório separado.

## 1. Âmbito

O `simulador-v2` faz **duas** coisas e mais nenhuma:

1. **Varrer os simuladores públicos dos bancos e reconstruir a função de preço de cada um.** Corre em horas mortas, ciclicamente ao longo do dia, por subcomando (§7). É o **único** caminho por onde este repositório fala com um banco.
2. **Responder a um pedido de comparação de imediato**, por cálculo local sobre o último varrimento. O cliente escolhe os bancos e recebe **uma** resposta, com todos dentro. Sem esperar por scraping nenhum.

A série temporal de mercado (`GET /api/rate-catalog`, §6) não é uma terceira coisa: é a publicação do que o ponto 1 já guardou.

Este repositório serve **JSON e mais nada** — sem ficheiros estáticos. A restrição é deliberada: no v1 não havia fronteira entre UI e servidor, e a lógica de apresentação acabou espalhada pelo servidor.

⚠️ **Consequência prática, executada a 2026-07-28 (KAN-22): a app web é um SERVIÇO À PARTE.** O `expo export` produz estáticos, e servi-los deste binário seria a primeira excepção a esta regra — que é como as regras deste género morrem. São dois serviços no mesmo alojamento, e o repositório Go continua a servir só JSON.

## 2. Camadas

```
cmd/
  simulador/        o binário: servidor HTTP e subcomandos
internal/
  dominio/          tipos e regras. Zero I/O, zero dependências do projecto.
  bancos/           um pacote por banco + as estratégias de transporte.
  aplicacao/        casos de uso: varrer, comparar, limitar. Sem HTTP, sem SQL.
  infra/            chi, pgx/sqlc, configuração, relógio.
api/                o contrato publicado — e o que dele se gera (fonte da verdade)
  openapi.yaml      o spec OpenAPI
  oapi-codegen.yaml config do gerador Go
  api.gen.go        tipos Go gerados (package api) — NÃO editar à mão
  tipos-app.d.ts    tipos TypeScript gerados para a app — NÃO editar à mão
db/                 migrações, queries .sql, sqlc.yaml
```

**A regra de dependência**, do interior para o exterior:

```
dominio    → (nada do projecto; stdlib e shopspring/decimal)
api        → (nada do projecto — só stdlib e as libs do gerado: net/http, chi)
bancos     → dominio
aplicacao  → dominio, bancos
infra      → dominio, bancos, aplicacao, api
cmd        → infra
```

### O que vive em cada camada

`internal/dominio/` — `Pedido` (o que o utilizador quer), `Oferta` (o que um banco responde), `Fase` (um troço do plano com taxa própria), `Produto`, `Requisitos` (o que um banco precisa e aceita), e as políticas puras: encaixar um período fixo numa lista válida, limitar o prazo por idade, decidir o que é comparável. Funções puras sobre estes tipos.

`internal/bancos/` — `contrato.go` define a interface que cada banco cumpre; `transporte/` tem as quatro estratégias (§5); `<banco>/` tem três ficheiros e uma pasta de capturas; `registo.go` mapeia `bancoID → construtor`.

`internal/aplicacao/` — `varrimento` (correr N bancos em paralelo sobre a grelha, com travão e resíduo), `comparar` (responder a um pedido por consulta e cálculo, sem tocar em banco nenhum), `limites` (tecto por IP). Recebe os bancos por injecção; nunca importa o registo directamente — é o que torna um caso de uso testável com bancos falsos.

⚠️ **O `comparar` não fala com bancos e o `varrimento` não responde a clientes.** É a fronteira que a §1 criou, e é o que faz de `comparar` uma função testável sem rede nem relógio. Note-se que quem tem fan-out concorrente passou a ser o `varrimento` — é lá que o `-race` do portão (§8) ganha o seu valor.

`internal/infra/` — `http` (routers `chi`, handlers, tradução de erros de domínio para códigos HTTP), `bd` (código gerado pelo `sqlc` + migrações), `travao` (o *advisory lock* por banco), `config`, `relogio`.

⚠️ **Os tipos que saem em JSON são distintos dos tipos de `dominio`, e são gerados.** Vivem em `api/` (`package api`), gerados do `openapi.yaml` pelo `oapi-codegen`; os handlers em `internal/infra/http` implementam a interface gerada e traduzem `dominio` ↔ contrato. Parece duplicação face ao domínio e não é: são a fronteira publicada e versionada, que não pode mudar só porque o domínio mudou. No v1 o modelo de domínio *era* o contrato HTTP, e por isso qualquer refactor arriscava partir o `viabilidade-imobiliaria`.

⚠️ **`api/` está fora de `internal/` de propósito, e é uma folha.** É a superfície pública do módulo — o contrato — e não importa nenhum pacote interno: o `depguard` reprova quem lá meta um `import` de `dominio`, `bancos`, `aplicacao` ou `infra`. O contrato descreve-se a si mesmo; não conhece a máquina que o serve. É `internal/infra/http` que o consome, nunca o contrário. O standard internacional (`/api` para os ficheiros de contrato) fixa o **spec**; co-locar aqui o Go e o TypeScript gerados é decisão nossa, para o contrato viver todo num sítio.

## 4. Modelo de dados

**Duas tabelas. Não há uma terceira sem uma entrada nova nesta secção.** ⚠️ Continuam a ser duas depois da inversão da §1 — ver «Porque é que não há uma terceira tabela», abaixo.

### `catalogo_taxas` — as observações do varrimento

Uma linha = um banco × um ponto da grelha, num varrimento. É a única coisa guardada a longo prazo, e não contém dados pessoais: os pontos da grelha são fixos e o titular é neutro.

⚠️ **Esta tabela é o motor, e não um produto secundário.** Era a série de mercado que alimentava o `/api/rate-catalog`; desde a inversão da §1 é a fonte de onde sai **toda** a resposta ao cliente.

O `cenario` é uma **chave estruturada** que identifica o ponto — tipo de taxa, período fixo e finalidade — e não um rótulo escolhido à mão. Tem de ser **derivável do pedido**, porque é isso que permite ir da pergunta de um cliente à linha certa. ⚠️ O LTV **não** entra nela: vive em duas colunas próprias, pela razão que está em «A resolução do LTV», abaixo.

É `<tipo>/<periodo>/<finalidade>`, **sempre três segmentos**, separados por `/`:

```
variavel/0/propria        fixa/10/propria        mista/5/arrendamento
```

Três decisões pequenas, e cada uma fecha um modo de falha:

- **arity fixa.** Três segmentos sempre, e o período é `0` na variável em vez de o segmento desaparecer. Uma chave de comprimento variável parte-se em silêncio quando alguém acrescenta uma dimensão — é literalmente a armadilha que o `DOSSIE-BANCOS.md` regista no `ConditionCode` do Montepio, onde partir a string pelo separador errado dá a finalidade errada.
- **vocabulário fechado dos dois lados.** Os três segmentos são valores de `dominio.TipoTaxa`, um inteiro, e `dominio.Finalidade`. Quem lê a chave valida-os, e uma chave que não seja reconhecida **falha alto** em vez de devolver um ponto plausível.
- **redonda.** `Cenario → chave → Cenario` e `Pedido → Cenario` são funções do `internal/aplicacao/grelha`, e há teste que fecha o ciclo. É o que sustenta a frase «derivável do pedido»: sem a volta completa, a chave é um rótulo bonito e a pergunta de um cliente não encontra a linha.

⚠️ **O que NÃO entra na chave**, e é deliberado: o LTV (colunas `ltv_min`/`ltv_max`), o **tenor da Euribor** (coluna `euribor_indexante`) e os **produtos** (coluna `produtos`). São colunas tipadas, não texto empacotado, pela razão da Decisão 2 da `ANALISE-KAN-35.md`. Consequência prática, e é preciso tê-la presente: **duas linhas do mesmo varrimento podem partilhar o `cenario`** e distinguir-se só por essas colunas — a linha com produtos e a linha sem eles, por exemplo. É assim que o desvio de cada produto se deriva na leitura, como esta secção já dizia em «Porque é que não há uma terceira tabela».
| coluna | tipo | nota |
| --- | --- | --- |
| `id` | `bigserial` PK |  |
| `varrimento_id` | `uuid` | agrupa as linhas da mesma corrida |
| `capturado_em` | `timestamptz` | ⚠️ **com** fuso |
| `cenario` | `text` | ⚠️ chave **estruturada** do ponto da grelha, derivável do pedido: `<tipo>/<periodo>/<finalidade>`, três segmentos sempre. **Sem LTV** — ver as duas colunas seguintes |
| `ltv_min`, `ltv_max` | `numeric(9,8)` | ⚠️ o intervalo de LTV com as fronteiras **medidas**, não uma banda de passo fixo. **Nulos** numa linha que não é um degrau da escala — ver «O que estas três colunas descrevem» |
| `spread_minimo` | `numeric(6,3)` | o lado **barato** de um degrau por resolver; **nulo** quando resolvido |
| `banco_id`, `banco_nome` | `text` |  |
| `rate_type` | `text` | `variavel` \| `fixa` \| `mista` |
| `valor_imovel`, `montante` | `numeric(12,2)` | ⚠️ **não** vírgula flutuante |
| `prazo_anos`, `fixed_period_years` | `int` |  |
| `euribor_indexante` | `text` |  |
| `tan`, `taeg`, `spread`, `euribor_valor` | `numeric(6,3)` | pontos percentuais |
| `prestacao_mensal`, `mtic` | `numeric(12,2)` |  |
| `residuo_prestacao` | `numeric(12,2)` | ⚠️ o resíduo da §7.4, em euros: prestação do banco **menos** a que a francesa dá sobre o plano dele. **Nulo** onde não se pode medir — ver «O resíduo mora numa coluna» |
| `base_fixa` | `numeric(6,3)` | ⚠️ só em `rate_type = 'fixa'`: a TAN **menos** o spread do degrau de LTV em que foi medida. É o que se consulta por período; a TAN sozinha vale só no LTV a que saiu — ver «Onde a base vive» |
| `produtos` | `jsonb` | ids dos produtos aplicados |
| `aplicado` | `jsonb` | o que o banco usou de facto, quando difere do pedido |
| `notas` | `jsonb` | avisos legíveis por pessoa |
| `sucesso` | `bool` |  |
| `erro` | `text` |  |

Índices: `(cenario, banco_id, ltv_min, capturado_em desc)` e `(capturado_em desc)`. ⚠️ O `ltv_min` entra no índice porque a consulta é um **intervalo que contém** o LTV do cliente, e não uma igualdade. O índice `(cenario, banco_id, capturado_em desc)` **fica** ao lado: não é prefixo do novo (o `ltv_min` entra ao meio) e é ele que serve o `/api/rate-catalog`, que não filtra por LTV. Numa tabela escrita uma vez por varrimento, o custo de um índice a mais não é argumento.

### ⚠️ O que estas três colunas descrevem, e quando são nulas

**1. A precisão: `numeric(9,8)` e não `numeric(6,3)`.** O LTV é uma fracção, e `numeric(_,3)` sobre uma fracção arredonda a milésimas — **0,1 p.p. de LTV**, erro máximo de **0,05 p.p.**, que é o orçamento inteiro da tolerância do refinamento: gastavam-se ~18 pedidos por banco a estreitar uma fronteira e a gravação desfazia-o. Medido na forma da CGD, as fronteiras têm até **sete casas** — 0,3325, 0,6659375, 0,67875 —, e a de 0,6659375 em `numeric(6,3)` fica 0,666. Oito casas representam tudo o que o plano de omissão produz e deixam uma de folga. ⚠️ Uma tolerância mais fina do que a de omissão obriga a rever isto.

**2. O `ltv_resolvido bool` sai, e entra o `spread_minimo`.** O `dominio.DegrauLTV` guarda o outro lado do degrau em vez de uma bandeira, e a razão está escrita lá: com uma bandeira, «não resolvido» e «sem o outro lado» são estados independentes, e o par impossível é representável. Pior, sem os dois números **a nota obrigatória não se consegue reconstruir a partir da linha** — ela nomeia o spread servido e o que se descartou, e sem isso diria «é aproximado», que não é informação. O `resolvido` deriva-se de `spread_minimo IS NULL`, e não se guarda duas vezes a mesma verdade. Há `CHECK` a impor que, existindo, o `spread_minimo` é **menor** do que o `spread` — a mesma guarda que o `NovaEscalaDeLTV` faz em Go.

**3. Nulos quando a linha não é um degrau.** Uma linha da grelha é uma de duas coisas, e a distinção não estava dita: ou é um **degrau da escala de LTV**, e aí traz o intervalo medido; ou é uma **observação num ponto** — um período fixo, um tenor, uma finalidade, um produto —, medida no LTV de referência, e aí não há intervalo nenhum a afirmar. Nesse caso as três colunas ficam nulas, e o LTV da observação continua derivável, porque `montante` e `valor_imovel` já estão na linha. ⚠️ Inventar um intervalo de largura zero à volta do ponto seria afirmar que o preço muda ali, que é precisamente o que não se mediu.

**4. Um degrau é uma linha completa, e quem a preenche é a observação do spread servido.** O `CHECK catalogo_taxas_resposta_completa_quando_sucesso` exige `tan`, `taeg`, `prestacao_mensal` e `mtic` em qualquer linha de sucesso, e um degrau não é excepção — ele é uma linha como as outras, «um banco × um ponto da grelha», com `ltv_min`/`ltv_max` a dizer **sobre que intervalo o spread dela vale**. Logo um degrau não se grava a partir do intervalo e do spread: grava-se a partir de **uma das observações que o mediram**, e qual delas não é indiferente.

**A regra: a observação representativa é a que traz o spread que se serve.**

- **Degrau resolvido** — todas as medições lá dentro têm o mesmo spread, e toma-se a de `ltv_max`: o maior LTV onde esse spread foi efectivamente medido.
- **Degrau por resolver** — o spread servido é o **mais alto dos dois lados** (alínea (d) do Anexo I da MCD, decidida acima), e a observação tem de ser a **desse** lado.

⚠️ **O modo de falha que isto fecha.** Com a observação do lado barato, a linha ficaria com o `spread` de um preço e a `tan`, o `taeg`, a `prestacao_mensal` e o `mtic` de **outro**. Nenhum `CHECK` apanha isso — a linha é coerente para a base e mentirosa para quem a lê —, e é exactamente o defeito da §7.4: um número errado com ar de certo, a envenenar todas as respostas até ao varrimento seguinte.

⚠️ **E uma consequência que fica escrita para não ser «corrigida» mais tarde.** Num degrau por resolver cujo lado caro seja o de **baixo**, a observação representativa está **em** `ltv_min` — ou seja, o LTV da própria linha, derivável de `montante`/`valor_imovel`, cai no extremo que o intervalo `(De, Ate]` exclui. Não é incoerência: é o único ponto onde o spread servido foi observado. Num degrau por resolver não há medições no meio — é isso que o torna por resolver —, e a alternativa era pôr na coluna `spread` um preço que aquela linha nunca mediu.

⚠️ `numeric`, não `float`. O v1 guardava dinheiro e taxas em `Float`. Não partiu nada visível, e é esse o problema: erros de vírgula flutuante em comparações de cêntimos aparecem como diferenças de 0,01 € que ninguém consegue explicar nem reproduzir. Em Go: `pgtype.Numeric` na fronteira da BD e `shopspring/decimal` no domínio.

⚠️ **Armadilha de compatibilidade a verificar com teste.** O `shopspring/decimal` serializa para JSON como **string entre aspas** por omissão, e o v1 (Python) enviava **números**. Como o `/api/rate-catalog` tem de ficar compatível ao byte (§6), a conversão para `float64` faz-se no tipo de saída, e há um teste de contrato que compara a resposta com uma amostra real do v1.

⚠️ `timestamptz`, não instante ingénuo. O v1 guardava sem fuso e tinha uma `utcnow()` com um comentário a explicar que não se podia trocar por `datetime.now(UTC)` sem rebentar as comparações. Isso é dívida a render juros.

⚠️ `produtos` é obrigatório para a série ser comparável. Sem saber que condições cada taxa pressupõe, a CGD e o BPI aparecem caros por lhes faltar o desconto, não por cobrarem mais. O v1 descobriu isto tarde.

**E está medido quanto isso vale — inverte a resposta, não a agrava (KAN-33, 2026-07-27).** Até aqui o `dominio.Pedido` não transportava selecção de produtos, e cada banco decidia por si: a CGD servia o preçário base e o Novo Banco servia o seu preço já com as duas bonificações ligadas. 

O  que saía era 1,350 da CGD contra 0,900 do Novo Banco: **o Novo Banco 0,45 p.p. mais barato**. Em pé de igualdade é **a CGD a mais barata, por 0,25 p.p., nas duas colunas**. Cada preço estava certo; era a comparação que estava invertida — e é isso que a Directiva 2006/114/CE, art. 4.º, proíbe ao exigir características «material, relevant, verifiable and representative».

⚠️ **Daí a regra: quem escolhe produtos é o pedido, não o banco.** O `Produto.PorOmissao` diz à app o que pré-seleccionar e mais nada; um `Pedido` sem produtos pede o preço sem produtos. E a selecção vai por dois caminhos diferentes, medidos: na CGD é **de leitura** (as duas colunas vêm no mesmo `/calculate`, logo um pedido dá as duas linhas do varrimento), no Novo Banco é **de pedido** (o campo `bonificacoes` decide, e `[]` é aceite). ⚠️ O Montepio (KAN-11) é o terceiro caso e é **de pedido**, pelo campo `Counterparts`.

### ⚠️ O resíduo mora numa coluna, e é uma só

O resíduo da §7.4 guarda-se em `residuo_prestacao`, **e não numa tabela nova**: é uma medição por observação, e uma observação já é uma linha desta tabela. Uma tabela à parte seria a terceira, a manter de acordo com esta e sem nada em troca.

**O que a coluna contém:** a prestação que o banco devolveu **menos** a que a amortização francesa dá sobre o plano que o próprio banco descreveu. Em euros, com sinal — o sinal diz de que lado se está a divergir, e um valor absoluto perdia-o sem poupar nada.

**É a primeira fase e não outra.** A segunda fase de uma mista amortiza um capital que já vem do arredondamento aos cêntimos de sessenta prestações, e esse ruído é do banco e não um sinal de que alguma coisa mudou: medido no `e2e_cgd_test.go`, a primeira fase fecha dentro de 0,05 € e a segunda precisa de 2 € de tolerância. Um resíduo tem de ser pequeno quando está tudo bem, ou ninguém repara quando deixa de estar.

**Nulo onde não se pode medir**, e são três casos, todos legítimos: a observação é de falha; a oferta veio sem plano de fases (a §5 permite-o — um banco não publica a fase que não descreveu); ou o plano descreve zero meses. ⚠️ Nulo aqui é «não havia o que comparar», nunca «comparou-se e deu zero».

⚠️ **Uma coluna e não duas.** A outra identidade que a §7.4 nomeia — TAN da fase indexada = Euribor + spread — **já é derivável da linha**: `tan`, `spread` e `euribor_valor` estão lá as três, e quem lê a série reconstrói-a com uma subtracção. A da prestação não é: o cálculo honesto corre sobre o **prazo que o banco aplicou**, lido do plano de fases, e o plano de fases não se grava — a linha tem `prazo_anos`, que é o **pedido**. É essa assimetria que paga a coluna, e fica escrita para não ser «simplificada» mais tarde.

⚠️ **Guarda-se o número; não se julga.** Sem limiar, sem erro, sem nota ao cliente. A §8 já decidiu que o resíduo não é portão — depende de servidores de terceiros estarem de pé, e um portão que amarela por isso deixa de ser lido. O que o portão verifica é o **cálculo**, contra observações gravadas.

### ⚠️ A resolução do LTV: intervalos medidos, não bandas de passo fixo

**Decidido a 2026-07-26 (KAN-35)**, e está medido que uma banda de passo fixo não representa o preço. As medições e a base legal estão em [`ANALISE-KAN-35.md`](ANALISE-KAN-35.md); o que se segue são os números que decidem esta secção.

Três coisas saem da medição, e cada uma mata uma solução: **duas fronteiras da CGD não caem em LTV inteiro** (afinar para 1 p.p. tem o mesmo defeito, só mais pequeno); **afinar o passo não reduz o erro, reduz a exposição** — quem cai do lado errado erra a altura do degrau, 0,70 p.p. na CGD, ~79 €/mês em 200 000 € a 30 anos; e **o preço não é monótono**, o que mata a bissecção. Os números estão na [`ANALISE-KAN-35.md`](ANALISE-KAN-35.md).

⚠️ **E os bancos discordam sobre a forma da coisa, o que é o argumento decisivo.** O Novo Banco muda nos múltiplos de 5, com a fronteira fechada em cima; a CGD não; e o **Montepio não muda de todo** — spread 1,500 de 50 % a 100 % (KAN-11). Três bancos, três formas: uma que quebra fora dos inteiros, uma que quebra nos múltiplos de 5, e uma constante. **Logo a resolução não pode ser uma constante do domínio — tem de ser medida banco a banco**, e um banco não chegava para decidir isto. A grelha por intervalos representa as três com as linhas que cada uma precisa: uma só, no caso do Montepio.

**O que fica decidido:**

- O spread guarda-se por **intervalo de LTV com fronteiras medidas**, em `ltv_min`/`ltv_max`, e não por banda de passo fixo. ⚠️ Duas colunas `numeric` tipadas, não uma chave de texto a empacotá-las: uma chave composta parte-se em silêncio quando o formato muda, que é a armadilha que o `DOSSIE-BANCOS.md` regista no `ConditionCode` do Montepio.
- A representação exacta é também **a mais pequena**: a função em degraus que o banco pratica são ~4 linhas por banco, contra 20 a 5 p.p. e ~71 a 1 p.p. O custo desloca-se todo para o varrimento, que passa a descobrir as fronteiras em vez de as assumir — ~96 pedidos por banco, cerca de um minuto, uma vez por corrida (§7).
- **Dentro de um intervalo por resolver serve-se o spread mais alto, com nota obrigatória** a dizer que é um limite superior. Não se recusa e não se escolhe um lado em silêncio. É o que prescreve o Anexo I, Parte II, alínea (d) da Directiva 2014/17/UE (MCD) para «vários valores possíveis», e a regra geral que este projecto adopta com ele: **perante incerteza, assume-se o valor menos favorável ao consumidor e declara-se a assunção** — nunca uma sem a outra. É a mesma regra que já governa o TAEG que não se consegue dar.

### O que é consulta e o que é cálculo

A resposta ao cliente sai destas duas colunas. É a distinção central do desenho novo, e a razão por que a grelha é pequena.

| sai por **consulta** ao varrimento | sai por **cálculo** local |
| --- | --- |
| spread, por intervalo de LTV com fronteiras medidas | TAN da fase indexada = Euribor + spread |
| taxa da fase fixa, por período fixo | prestação, por amortização francesa |
| valor da Euribor, por tenor | ordenação e comparação entre bancos |
| desvio da finalidade (aditivo, em p.p.) | aplicação dos descontos de produtos escolhidos |
| desconto de cada produto (aditivo, em p.p.) | |

⚠️ **O que não se armazena é o resultado; é o parâmetro.** Guardar a prestação de cada cenário obrigaria a uma grelha do tamanho do espaço de pedidos. Guardar o spread do intervalo e calcular a prestação responde a pedidos que ninguém fez ainda. É a diferença entre uma cache e um modelo, e é a razão de a grelha ser de dezenas de pontos por banco e não de milhares.

⚠️ **A taxa fixa é consulta e não cálculo, e isso está medido.** Não é «Euribor + spread»: a parte que depende do período é a curva de funding do banco. No Novo Banco, fixa a 5 anos dá TAN 3,89 % e a 30 anos dá 5,24 % (`DOSSIE-BANCOS.md`). Como os bancos só vendem uma lista curta de períodos — `{5,10,15,20,25,30,35}` na CGD, `{2,3,4,5,10,15,20,25,30}` no Novo Banco, `{2,5,7,10,15,25,30}` no Montepio — basta uma observação por período da lista, e não há interpolação nem modelo ajustado. Ajustar uma curva a uma tabela só acrescentaria erro onde não havia.

### ⚠️ O que se guarda por período é a BASE, e não a TAN

**Medido a 2026-07-28 (KAN-16), no produto cartesiano da CGD: 1 210 pedidos, 121 valores de LTV × 10 famílias.** ⚠️ Uma observação por período **chega em número e não chega em conteúdo** — é a distinção que custou a medição.

**A TAN da taxa fixa muda com o LTV**, e muda nas **mesmas fronteiras** da variável e com a **mesma altura de degrau**:

| | LTV 0,30 | 0,335 | 0,67 | 0,68 |
| --- | --- | --- | --- | --- |
| **escala** (spread da variável) | 1,950 | 2,000 | 2,050 | 1,350 |
| fixa a 10 anos | 5,450 | 5,500 | 5,550 | 4,850 |
| fixa a 20 anos | 5,600 | 5,650 | 5,700 | 5,000 |
| fixa a 30 anos | 5,850 | 5,900 | 5,950 | 5,250 |

Subtraindo o spread do degrau a cada TAN, sobra um número **constante ao cêntimo** em todos os degraus: **3,500** a 10 anos, **3,650** a 20, **3,900** a 30. A queda de 0,70 p.p. aos 68 % é exactamente a mesma que a variável tem no mesmo sítio.

**A regra: `TAN_fixa(período, ltv) = base(período) + spread(ltv)`.**

⚠️ **A grelha não multiplica, e é essa a boa notícia.** Continua a ser **uma** observação por período — mas o que dela se extrai e se guarda é a **base**, isto é, a TAN menos o spread do intervalo em que foi medida. Quem responde soma o spread do intervalo do cliente, tal como já faz na variável com a Euribor. Uma grelha que guardasse a TAN estaria a servir o preço do LTV a que a mediu a toda a gente — e o erro seria de 0,70 p.p. para quem caísse do outro lado dos 68 %, que é dinheiro a sério numa prestação.

**Por medir, e continua em aberto para os outros bancos:** se a mesma relação vale fora da CGD. O `DOSSIE-BANCOS.md` tem o indício do Novo Banco — a mista «a 25 e 30 anos dá o preço de 20» —, que é outra coisa: ali é o **período** que não se distingue, não o LTV. Cada banco novo repete a medição; nenhum a herda.

### ⚠️ Onde a base vive: coluna `base_fixa`, e o critério que a paga

**Decidido a 2026-07-28 (KAN-41), a executar a subsecção acima.** Havia duas hipóteses:

1. **coluna nova**, `base_fixa`, preenchida ao gravar;
2. **derivar na leitura**, cruzando a linha de taxa fixa com os degraus do mesmo `varrimento_id`.

Ganhou a 1, e o argumento **não** é o cálculo — que é uma subtracção. É o **contexto**.

A base é a TAN da linha menos o spread do degrau em que o LTV dela cai. A TAN está na linha; o spread está **noutra linha** do mesmo lote. Ou seja: a subtracção precisa do **lote inteiro**, e quem lê para responder a um cliente lê **uma linha**. Na hipótese 2, cada leitura teria de recarregar os degraus do varrimento a que a linha pertence só para não mentir — e **uma consulta que precisa de outra consulta para não mentir é a definição de um dado que não está onde devia**. Pior: quem se esquecesse da junção não via erro nenhum, via a `tan`, que é um número real. Silencioso, como tudo o que a §7.4 existe para apanhar.

Grava-se, portanto, no momento em que o lote está todo à mão.

⚠️ **O critério não é «é derivável?», e a comparação com o `residuo_prestacao` mostra porquê.** O resíduo também se grava, e ali a razão é outra: ele **não** é reconstruível a partir da linha, porque corre sobre o prazo aplicado, lido de um plano de fases que não se guarda. A base é reconstruível — mas só com o lote. O critério comum aos dois é **estar onde se lê**: o que responde a um cliente tem de sair da linha que se leu, sem uma segunda consulta e sem um passo que alguém se possa esquecer de dar.

**Onde se faz a subtracção:** em quem grava o lote (`internal/infra/catalogo`). Reconstrói-se a escala de cada banco a partir das próprias linhas de degrau — que a trazem em `ltv_min`/`ltv_max`/`spread` — e calcula-se linha a linha. ⚠️ Não é um campo preenchido por quem constrói a observação: um campo desses esquece-se num dos caminhos, e o caminho da escala não passa por onde os pontos passam. É a lição que o `capturado_em` deixou na sétima fatia da `KAN-16`, e é o mesmo desenho do resíduo.

⚠️ **Uma linha de taxa fixa cujo varrimento não mediu escala nenhuma fica com a base nula, e isso é dado bruto válido mas não é resposta.** Grava-se na mesma — a TAN é um número real que o banco devolveu, e apagá-la seria perder a medição —, mas quem responde a um cliente não tem por onde a corrigir para o LTV dele. Nesse caso **não se serve**: não há base a inventar, e servir a TAN medida noutro LTV é o erro de 0,70 p.p. que esta secção existe para impedir. **Nula é «não há por onde corrigir», nunca «a base é zero».**

### ⚠️ O limite: onde o preço depende da pessoa

Um varrimento com titular neutro reconstrói o que não depende de quem pede. Não reconstrói o resto, e isto não é lacuna de implementação — é uma consequência da §4 exigir que esta tabela não tenha dados pessoais.

- **Spread, TAN e prestação:** não dependem da pessoa. A idade não entra no spread. Consulta e cálculo, exactos.
- **TAEG e MTIC:** dependem. Levam o prémio do seguro de vida, que é função da idade e do capital. Um varrimento neutro **não** dá o TAEG daquela pessoa. ⚠️ Medido no Montepio a 2026-07-27: no mesmo crédito, o prémio mensal do seguro de vida foi 15,25 € aos 30 anos e 20,32 € aos 36, com a **mesma** TAN e a mesma prestação — e o MTIC mexe-se com ele.
- **Prazo máximo:** depende da idade, mas é regra e não preço — vive em `Requisitos` e resolve-se nas políticas do domínio, sem varrimento.

⚠️ **Não se serve um número calculado como se fosse cotado pelo banco.** O v1 tropeçou nisto e registou a lição: quando o `/get_by_rates` do Millennium falhava, caía numa amortização francesa local com TAEG e MTIC a nulo, e a conclusão foi «**preferir falhar com clareza a servir um número inventado com ar de oficial**» (`DOSSIE-BANCOS.md`). Aqui isso significa: o que vem do varrimento vai identificado como tal, com a hora do varrimento, pelo mecanismo de `Notas` que a §5 já obriga a existir.

### ⚠️ A TAEG deriva-se com os encargos MEDIDOS

**Decidido a 2026-07-28**, e revoga «o TAEG que não se consegue dar omite-se — não se estima». A alternativa deixou de ser entre *omitir* e *inventar*: há uma terceira via, **derivar de uma medição e declarar as hipóteses**.

**O que mudou tecnicamente.** A TAEG excede a TAN pelos encargos, e os encargos não se observam directamente — o que se observa é o par (TAN, TAEG) que cada banco devolve. Essa distância é o custo dos encargos, mas não diz de que encargos se trata, e isso importa porque as duas naturezas reagem ao prazo **ao contrário** uma da outra:

| natureza | 3 000 € / 25 € por mês, num crédito de 320 000 € | a 10 anos | a 40 anos | razão |
| --- | --- | --- | --- | --- |
| **antecipado** (comissões, imposto do selo) | valor único, dilui-se | +0,204 p.p. | +0,061 p.p. | **3,34** |
| **recorrente** (seguro de vida) | renova-se todos os meses | +0,173 p.p. | +0,136 p.p. | **1,27** |

⚠️ **São essas duas razões diferentes que tornam a repartição identificável**, e só a partir de **dois prazos**. Com uma observação só, qualquer repartição reproduz exactamente a mesma TAEG naquele prazo — e escolher uma seria assunção nossa disfarçada de medição, que é o modo de falha da §7.4 aplicado ao número que a MCD trata como o de comparação por excelência. É por isto que a grelha ganhou a **7.ª família**: dois pontos por banco, nos **extremos** de prazo que ele serve. Não medem spread — está medido que o spread não depende do prazo — e é essa redundância que os torna também um travão, se essa medição deixar de valer.

**O que continua a ser hipótese, e é declarado como tal:** que os encargos se descrevem por estas duas naturezas e não por mais; que o recorrente incide sobre o capital **em dívida** e não sobre o inicial; e que não mudam com o prazo. Um banco que os contrarie **aparece como resíduo** — o ajuste ancora-se nos dois extremos e deixa as observações do meio livres para discordar. ⚠️ Um ajuste que consumisse todas as observações não teria com que se contradizer, e era um modelo que nunca podia estar errado: o oposto de uma medição.

**O que NÃO mudou, e é a condição de tudo isto:** a TAEG e o MTIC vão marcados no contrato como **derivados**, e cada oferta traz `pressupostos` — obrigatório, à parte das `notas`, em português — a dizer que o número não veio do banco, sobre que encargos foi calculado, e que a TAEG que vincula alguém vem na ficha de informação normalizada depois de o banco avaliar quem pede. É o Anexo I, Parte II e o Anexo II da MCD: **assumir e declarar**. Uma TAEG preenchida com `pressupostos` vazios é defeito nosso, e o contrato di-lo.

⚠️ E a razão de a §4 poder mudar de posição sem se contradizer está no princípio que ela já tinha escrito: «perante incerteza, o valor menos favorável ao consumidor — **e declarado**. Nunca uma sem a outra: assumir sem declarar é enganar, **declarar sem assumir é não responder**.» Omitir a TAEG era o segundo caso.

**A escolha de haver, ou não, um caminho de «confirmar no banco»** — uma simulação ao vivo de **um** banco, para a pessoa real, que devolve o TAEG oficial — está por tomar e não bloqueia nada. Fica registada aqui como opção conhecida, e não como promessa.

### Porque é que não há uma terceira tabela

A tentação era guardar o modelo de preço à parte, numa tabela de parâmetros. Não se faz: **uma observação do varrimento já é uma linha de `catalogo_taxas`** — banco, ponto da grelha, instante, spread, taxa, produtos. Os desvios aditivos (finalidade, produtos) são a diferença entre duas linhas, e derivam-se na leitura.

Uma tabela de parâmetros seria uma segunda cópia da mesma verdade, com a obrigação de as manter de acordo. E a §2 nasceu de um modelo de dados entulhado — a regra das duas tabelas não é um número bonito, é o travão.

### `limites` — tecto de pedidos por IP

| coluna | tipo |
| --- | --- |
| `chave` | `text` PK (ex.: `sim:ip:1.2.3.4`) |
| `janela_inicio` | `timestamptz` |
| `contagem` | `int` |
| `bloqueado_ate` | `timestamptz` null |

⚠️ **A razão desta tabela mudou a 2026-07-25, e ela fica.** Existia porque «cada submissão custa dezenas de segundos de scraping **a partir do nosso IP** contra os bancos» — e isso deixou de ser verdade: uma submissão custa uma consulta a Postgres. O tecto mantém-se porque o site é público e um endpoint público sem tecto é um convite; mas passa a ser uma medida contra abuso banal, e não o travão que protegia a nossa relação com os bancos. **Esse travão passou para o varrimento** (§7.2), que é agora o único a falar com eles.

A tabela tem nome próprio, e não o de uma funcionalidade apagada.

### O que **não** é uma tabela

- **Simulações de utilizador.** São calculadas e devolvidas. Não são guardadas.
- **A cache.** ⚠️ Não existe — deixou de ter razão de ser com a inversão da §1. Ver o fim da §7.
- **O modelo de preço de cada banco.** Deriva-se das observações; não é uma segunda cópia (ver «Porque é que não há uma terceira tabela»).
- **Sessões, contas, tokens, emails.** Não existem e não vão existir.

**Migrações:** `goose`, ficheiros SQL versionados em `db/migracoes/`, embebidos no binário. ⚠️ **Não há auto-migração.** Foi o `create_all` automático do v1 que deixou a base num estado híbrido que o Alembic desconhecia, e a migração seguinte morreu sobre uma tabela que já existia.

⚠️ **E isso passou a ser imposto pelo binário, não prometido (2026-07-28, KAN-22):** `simulador servir` contra uma base por migrar **recusa-se e sai**, a dizer «base de dados por migrar — corre `simulador migrar` antes de servir». É por isso que o deploy corre as migrações como passo próprio (`release_command`) e não como efeito secundário do arranque: não há caminho por onde uma versão nova comece a servir sobre um esquema que não é o dela.

## 5. Contrato dos bancos

Detalhe completo em `CONTRATO-BANCO.md`. O essencial de arquitectura:

**Cada banco são três ficheiros e uma pasta:**

```
internal/bancos/cgd/
  cgd.go         a implementação da interface; declara `Requisitos()`
  pedido.go      dominio.Pedido  →  payload do banco      (PURO)
  resposta.go    payload do banco →  dominio.Oferta       (PURO)
  capturas/      pares pedido/resposta reais, versionados
```

⚠️ `pedido.go` e `resposta.go` não fazem I/O. Recebem e devolvem dados, sem rede e sem relógio. É isto que torna cada banco testável offline contra uma captura real, e é a diferença estrutural face ao v1 — onde os quatro bancos de browser não tinham forma nenhuma de ser testados sem rede.

**Quatro estratégias de transporte, partilhadas** (`internal/bancos/transporte/`), porque foram quatro as que o v1 provou serem necessárias:

| estratégia | quem a usa | o que faz |
| --- | --- | --- |
| `HTTPSimples` | CGD, Novo Banco, Banco CTT, Crédito Agrícola, Santander | um ou mais pedidos HTTP, sem estado |
| `HTTPComSessao` | Montepio | um `GET` de arranque que fixa cookies e extrai um token do HTML, depois o `POST` |
| `BrowserParaCredencial` | ActivoBank, Millennium BCP | browser só para cunhar um token, simulação em HTTP |
| `BrowserComoCliente` | Bankinter (Cloudflare), BPI (formulário) | o pedido parte de dentro da página |

No v1 cada scraper reimplementava a sua variante de «abrir browser / fixar cookies / cunhar token», e as variantes nunca convergiram. Aqui a estratégia é injectada — o que também a torna substituível por uma falsa nos testes.

⚠️ **As duas estratégias de browser continuam a ser um risco por avaliar, mas mudou de natureza a 2026-07-25.** O risco era duplo: técnico (em Go é `playwright-go`, binding da comunidade, ou `chromedp`; e o Bankinter atravessa Cloudflare com um disfarce anti-automação específico) e de latência (30-80 s por banco). **A parte da latência desapareceu com a §1:** um banco de browser é varrido em hora morta, e os 52 s do BPI não estão no caminho de cliente nenhum. Fica o risco técnico, que é real e continua a exigir prova de conceito antes do compromisso — com a mesma saída conhecida: correr os bancos de browser como serviço à parte, atrás da mesma interface.

⚠️ **E há agora um custo novo nessa decisão, medido a 2026-07-28 (KAN-22):** a imagem do serviço são **24,3 MB** sem Chromium. O v1 precisava de 4-8 GB, e quase tudo era o browser. Meter Chromium nesta imagem multiplica-a por cem — o que é, por si só, o argumento mais forte a favor da saída conhecida.

**Política de flexibilidade (herdada do v1, e boa):** quando o banco não aceita exactamente o que foi pedido — período fixo fora da lista, prazo acima do máximo por idade, Euribor imposta — simula-se no valor válido mais próximo, regista-se o que foi aplicado em `Oferta.Aplicado` e acrescenta-se uma nota. **Não falha.** Falhar é para quando o banco não consegue responder de todo.

⚠️ **Um banco avariado nunca derruba o varrimento.** No v1 isso obrigava a um `except Exception` dentro de cada scraper. Em Go a política é a mesma mas fica **num sítio só**: o orquestrador de `aplicacao/varrimento` corre cada banco numa goroutine com `recover` e converte pânico ou erro numa observação de falha. Dentro de um banco, os erros devolvem-se; não se engolem.

⚠️ **E um banco em falha já não estraga a resposta ao cliente — atrasa-a.** É a diferença que a §1 comprou: se um banco falhou o varrimento de hoje, a comparação responde com o valor do varrimento anterior e diz de quando é, em vez de devolver «este banco está em baixo». O que antes era uma falha visível passa a ser um número mais velho, declarado.

## 6. Fronteiras HTTP

Duas, com estatutos diferentes. Contrato completo em `API.md`, esquema executável em `api/openapi.yaml`.

`/api/v1/*` — a app React Native. Nossa, versionada, evolui connosco. Os tipos Go do servidor e os tipos TypeScript da app são **gerados** a partir do `openapi.yaml` (`oapi-codegen` e `openapi-typescript`). O contrato é código dos dois lados, não documentação.

⚠️ **O ciclo das simulações mudou com a inversão da §1, e está executado (KAN-32, 2026-07-28).** Era `POST` → `202` com identificador, seguido de sondagem a cada segundo com `estado`, `progresso` e ofertas parciais. É agora **`POST /api/v1/comparacoes` → `200` com a resposta completa**: o cliente escolhe os bancos e recebe todos de uma vez. Desapareceram o `GET /{id}`, o `EstadoSimulacao`, o `Progresso` e o `503 DemasiadasEmCurso`; a oferta traz o instante do varrimento de que o número saiu (`capturado_em`) e a resposta traz o instante em que foi calculada (`calculado_em`).

⚠️ **A rota chama-se `/api/v1/comparacoes` e nunca se chamou outra coisa.** O `API.md` prometeu `/api/v1/simulacoes` até 2026-07-28 — este parágrafo repetia-lhe o nome ao descrever o ciclo antigo, e assim **dois documentos concordavam um com o outro e ambos com o código nenhum**. O `make gerado` não apanha isto: compara o gerado com o spec, não os documentos com o spec. **Quando um documento e o `api/openapi.yaml` discordarem, o documento é que está errado.**

`/api/rate-catalog` — o `viabilidade-imobiliaria`. ⚠️ **Compatível ao byte com o v1.** O cliente que já existe (`src/viabilidade/taxas.py`) lê `points[]` com `tan, spread, euribor_valor, euribor_indexante, fixed_period_years, bank_id, bank_name, scenario_key, rate_type, prazo_anos, captured_at`, mais `scenarios[]`, com os parâmetros `scenario, bank, rate_type, since, limit` e o cabeçalho `X-API-Key`. Os nomes ficam **em inglês e em snake\_case** neste endpoint, ao contrário do resto do repositório, porque mudá-los parte o outro repositório sem aviso. Há um teste de contrato dedicado.

Sem chave configurada o catálogo fica aberto — é o comportamento de desenvolvimento, e é um aviso alto no arranque.

⚠️ **A congelação é interina, não permanente.** O `viabilidade-imobiliaria` vai levar o mesmo tratamento que este repositório está a levar. Quando isso acontecer, o contrato redesenha-se **em conjunto** — com os nomes em português e com a decomposição spread/Euribor que a issue #22 do v1 propunha. Até lá, o formato antigo é lei: quem consome está em produção e não pede licença para partir.

⚠️ **Uma rota declarada no spec e sem handler atravessa o portão inteiro em silêncio**, e o `/api/rate-catalog/snapshots` esteve assim desde que o contrato existe (corrigido a 2026-07-28, KAN-44). A razão é estrutural: o `make gerado` compara o **gerado** com o spec, não o **servido**. Há agora um teste que percorre os caminhos do `openapi.yaml` e falha a nomear o que ficar sem handler — fecha a classe, não o caso.

### ⚠️ CORS: quem pode chamar de um browser, e o que fica de fora

**Decidido a 2026-07-28 (KAN-22), ao pôr a app web de pé.** Até aqui não havia CORS **nenhum** — nem no código nem no contrato —, o que na prática queria dizer que a app web não conseguia falar com esta API a partir de outra origem. Não era uma escolha: era um buraco.

**A regra:** lista **explícita** de origens em `ORIGENS_PERMITIDAS`, com **default vazio**. Vazio não é «tudo»: é «sem cabeçalhos de CORS», ou seja, só a mesma origem. Nunca `*`.

**E `AllowCredentials` é falso, sempre.** Não há cookies, não há sessões e não vai haver (§4): a app não tem nada para autenticar. Com credenciais a falso, `*` seria tecnicamente aceitável para o browser — e continua a não se usar, porque uma lista de origens é também a lista de **quem sabemos que existe**, e essa informação vale por si no dia em que aparecer tráfego de uma origem que ninguém reconhece.

⚠️ **O CORS aplica-se a `/api/v1/*` e ao `/healthz`, e NÃO ao `/api/rate-catalog`.** É a decisão menos óbvia desta secção, e a razão é esta: o catálogo autentica-se por `X-API-Key`, e **uma chave dentro de um bundle de browser é uma chave pública** — qualquer pessoa a lê nas ferramentas de programador. Não abrir a porta custa uma linha; confiar que ninguém a atravessa custa a chave. O consumidor daquele endpoint é o `viabilidade-imobiliaria`, que é servidor e não browser, e para quem o CORS é irrelevante.

⚠️ **A dupla fronteira é o ponto.** Este repositório serve **duas** superfícies com públicos diferentes — uma app pública sem credenciais, e uma máquina com chave — e elas não partilham política de acesso. Tratá-las como uma só, em qualquer dos sentidos, é o erro: `*` na app abre o catálogo; a chave do catálogo na app publica-a.

**O que o browser não recebe, e é de propósito:** a app **não** manda `X-API-Key` nenhuma. Os endpoints `/api/v1` são públicos e o que os protege é o tecto por IP (§7), não uma credencial. Uma credencial que viaja para o cliente não é uma credencial.

⚠️ **E ligar o CORS partia o tecto por IP ao meio, o que só apareceu por se ir medir.** Um browser manda um `OPTIONS` de sondagem antes de cada `POST /api/v1/comparacoes` — o `Content-Type: application/json` obriga-o —, portanto cada comparação feita da app custava **dois** do tecto e a mesma feita por `curl` custava **um**: o tecto passava a medir o cliente em vez do uso. Os preflights não contam. E não abre buraco, porque só é reconhecido como preflight o `OPTIONS` que traga `Access-Control-Request-Method` — senão bastava escolher o método para escapar ao tecto.

## 7. O varrimento, e o que substitui a cache

1. **O varrimento é o único caminho para os bancos.** Corre por **subcomando** (`cmd/simulador/`), não por rota HTTP. Uma rota teria de ser protegida, e autenticação foi o que o v1 ganhou sem decidir e teve de apagar em três migrações. Se um dia for preciso disparar de fora, acrescenta-se a rota então.
2. **⚠️ Com travão: nunca dois varrimentos do mesmo banco ao mesmo tempo.** Dois arranques não valem duas cargas em cima do banco. O travão vive na base, não no processo (ver 5).
3. **Cíclico ao longo do dia, e ⚠️ nunca a servir através da viragem do dia.** A Euribor fixa diariamente e a TAN depende dela. 

⚠️ **Executado a 2026-07-28 (KAN-22), e a decisão de varrer é do SUBCOMANDO, não do agendador.** A máquina agendada acorda **de hora a hora** e corre `simulador varrer`; quem decide se há alguma coisa a fazer é a guarda `--se-antigo`, com omissão de **6 horas**. Dá ~4 varrimentos por dia e 20 arranques que saem a dizer «varrimento saltado» — com código **0**, não com erro, porque um subcomando cíclico que saísse com 1 por não ter de correr enchia o log do agendador de falhas que não são falhas, e quem as visse deixava de as ler. ⚠️ A alternativa era pedir ao agendador uma hora exacta, e o do Fly não a tem (`hourly`/`daily`, sem escolha): pôr a regra no lado que a sabe é o que a torna independente da plataforma. Dois arranques sobrepostos não fazem mal — o travão do ponto 2 vive na base precisamente para travar **entre máquinas**.
4. **⚠️ Cada varrimento mede o seu próprio resíduo.** É o travão contra o modo de falha deste desenho, e é obrigatório. Ao vivo, um campo mal lido estraga **uma** resposta; aqui envenena **todas** até ao varrimento seguinte, e envenena-as com ar de certas. Por isso cada varrimento leva um punhado de cenários em que se compara o número que o banco devolveu com o número que o nosso cálculo daria, e guarda a diferença. Se o resíduo cresce, alguma coisa mudou do lado do banco — e aparece como número, não como silêncio. O `DOSSIE-BANCOS.md` já registou a versão pequena desta lição: limites de idade medidos e escritos como constantes «sem nada que avise quando o banco os mudar».

⚠️ **Executado a 2026-07-28 (KAN-16), e não é «um punhado de cenários»: são todos.** A comparação é a prestação da primeira fase contra a amortização francesa sobre o plano que o banco descreveu, e custa uma multiplicação — escolher um punhado seria escolher onde não olhar. Vive em `varrimento.Residuo`, grava-se na coluna `residuo_prestacao` (§4, «O resíduo mora numa coluna») e sai resumido no relatório do `simulador varrer`, por banco. ⚠️ **A dimensão do LTV mede-se pelo mesmo caminho**: um degrau é uma linha como as outras e leva o seu resíduo — foi por o caminho da escala não passar por onde os pontos passam que o `capturado_em` quase ficou a zero, e o mesmo esquecimento aqui deixava a escala sem travão nenhum.
5. **⚠️ Nada de estado em-processo.** O v1 tinha o gate por banco, o dedup e a cache dentro do processo, e um aviso no arranque a dizer que com mais do que um worker o mesmo banco levava N scrapes em paralelo — precisamente o que o gate existia para evitar. Várias PaaS definem a concorrência sozinhas. A regra mantém-se inteira; o que muda é onde: **o varrimento, o travão e os limites vivem em Postgres**, que já existe por causa da §4.

O travão é o `internal/infra/travao`, sobre `pg_try_advisory_lock` (KAN-39).

6. **⚠️ A grelha confirma-se por sondagem barata, e não por revarrimento.** Decidido a **2026-08-01**. Entre varrer tudo e não saber nada havia um vazio: uma grelha varrida há seis horas pode já não descrever o banco, e a única forma de o saber era varrer outra vez — 96 pedidos por banco para descobrir que nada mudou. A **sonda** fecha esse vazio com \~4 pedidos por banco.

⚠️ **A sonda não é o resíduo do ponto 4, e confundi-los perde as duas.** O resíduo compara a prestação que o banco devolveu com a que a francesa dá **sobre o plano do próprio banco** — é coerência interna da resposta dele, e mede-se em cada observação. A sonda compara **o que a nossa grelha prevê** com **o que o banco responde agora** — é deriva da nossa fotografia contra a realidade. O resíduo apanha um campo mal lido; a sonda apanha um preço que mudou. Um está a 0,00 € e o outro nunca foi medido.

**Uma sonda por degrau, colocada logo abaixo do `Ate`** — e não no meio, que custaria o mesmo e veria menos. A escolha do sítio é uma assimetria deliberada:

- se a fronteira **descer**, a sonda passa a cair no degrau seguinte e vê spread diferente → **detecta**;
- se a fronteira **subir**, a sonda continua no degrau antigo e não vê nada → **não detecta**. O erro que daí resulta é servirmos o spread mais **alto** a quem já qualificava para o mais baixo — a direcção que o Anexo I, Parte II, alínea (d) da MCD manda presumir, e a mesma que o `SpreadMinimo` já codifica.

⚠️ **Ou seja, a sonda deteta a direcção que nos faria servir barato de mais, e falha a que nos faz servir caro de mais.** É um erro aceite com conhecimento de causa, não uma lacuna.

**A tolerância não é escolhida — é lida do degrau.** Num degrau resolvido é **zero**, e isso é defensável porque a medição de fidelidade deu zero divergências em 147 098 comparações: se hoje reproduzimos o banco exactamente, qualquer diferença é sinal. Num degrau por resolver é `Spread − SpreadMinimo`, que é a largura da incerteza que já foi medida. ⚠️ Uma tolerância constante escolhida à cabeça seria o defeito que a §7.4 nomeia — um número com ar de certo — aplicado ao próprio detector.

**O que a sonda NÃO apanha, e fica escrito:** uma fronteira que suba, e um patamar novo mais estreito do que a distância entre sondas. O segundo é o caso que a KAN-35 já mediu na CGD — o 2,050 entre 66,75 % e 67,75 %, 1 p.p. de largura e não monótono — e continua a exigir a descoberta densa. **A sonda não substitui o varrimento; encurta o intervalo em que se está às escuras.**

**Na divergência, serve-se o antigo com menor fiabilidade declarada, e tenta-se revarrer AQUELE banco.** Não se recusa: uma grelha suspeita ainda é a melhor informação que há, e recusar dava um ecrã vazio onde havia um preço provavelmente certo. Não se cala: a oferta sai com a fiabilidade reduzida dita, pela mesma regra da §5 que faz um degrau por resolver sair com nota. E não se revarre o mundo: o âmbito é **um banco**, o que limita a carga a \~96 pedidos e a mantém previsível.

⚠️ **A escalada automática é segura por causa do ponto 2, e só por causa dele.** O travão em Postgres impede dois varrimentos do mesmo banco em paralelo, portanto uma sonda que diverja repetidamente não multiplica carga — a segunda tentativa não toma o travão e sai. Sem esse travão, isto seria um amplificador: um banco que mudou de preço faria cada sonda disparar um varrimento.

**Custo, com os números da KAN-35:** \~4 degraus por banco, logo \~20 pedidos para confirmar os cinco, contra \~480 de um varrimento completo dos cinco. **Vinte e quatro vezes mais barato**, o que é o que a torna corrível com frequência — e uma confirmação que se pode correr de hora a hora vale mais do que um varrimento que se corre quatro vezes por dia.

### O que morreu com a cache, e o que sobreviveu

**Morreu: a quantização do pedido.** Arredondar o montante ao milhar e o valor do imóvel aos 5 000 € existia para aumentar acertos de cache, e não há cache. Um pedido é avaliado no seu valor exacto.

**Sobreviveu a razão pela qual ela era perigosa.** O aviso antigo dizia: «se o arredondamento cruzar um degrau de 5 % de LTV, não arredonda — o spread é uma função em degraus e dar a banda errada é pior do que perder o acerto». Essa observação continua verdadeira e passou a ser **estrutural em vez de defensiva**: o LTV é uma dimensão da grelha (§4), portanto o pedido cai no seu intervalo por construção, e não é arredondado para perto dele.

⚠️ **E o «degrau de 5 %» daquele aviso estava errado por outra razão, medida a 2026-07-26 (KAN-35):** os degraus são os do banco, não os nossos, e na CGD nem sequer caem em LTV inteiro. Uma guarda escrita contra degraus de 5 % teria deixado passar exactamente os casos que interessam — ver «A resolução do LTV» na §4.

⚠️ **A idade continua a não se quantizar**, e agora nem entra: não é dimensão da grelha porque não entra no spread. Entra no seguro de vida — logo no TAEG e no MTIC, que é o limite honesto da §4 — e no prazo máximo, que é regra do domínio e não preço.

**⚠️ IP atrás de proxy — bug de produção do v1, corrigido de origem.** No v1, o servidor não aceitava o `X-Forwarded-For` de um proxy noutro contentor e o IP do cliente passava a ser o do proxy, igual para toda a gente: o tecto «10 pedidos por 10 min por IP» virava o tecto do **site inteiro**, e o primeiro visitante trancava os restantes. A correcção **não** é confiar em toda a gente — isso deixa qualquer cliente escolher o seu IP num cabeçalho. É uma lista explícita de proxies de confiança, com default vazio, e um teste que confirma que um `X-Forwarded-For` enviado directamente à app é ignorado.

⚠️ **E em produção isto passa a ser uma medição por fazer, não uma configuração a adivinhar (2026-07-28).** Atrás de um alojamento com proxy, a rede de onde ele fala tem de ser **medida** e escrita: larga de mais deixa quem estiver nela escolher o seu IP e contornar o tecto; vazia reproduz o bug do v1. Fica **vazia** até estar medida, e a razão é assimétrica — um tecto apertado de mais é um problema visível, e um tecto contornável não é.

## 8. Verificação

Regra da casa: um critério de pronto vale pelo que se mediu. Reverte-se o defeito e confirma-se que o teste **falha**, e que falha a nomear a coisa certa.

Cinco portões, todos automáticos:

1. `depguard` — a regra de dependência de §3.
2. **Teste de contrato do** `/api/rate-catalog` — o formato de §6 que o `viabilidade-imobiliaria` consome, contra uma amostra real do v1.
3. **Testes de captura por banco** — cada `resposta.go` contra as capturas versionadas, offline e sem rede.
4. `golangci-lint run ./...` limpo, sobre o repositório inteiro. ⚠️ Não sobre um subconjunto. O v1 tinha 368 erros permanentes fora de `src/`, o que cegou o portão por completo: ninguém lê uma saída de 368 linhas para descobrir a 369.ª. Aqui não há pasta de scripts onde eles se acumulem, e o portão é o repositório todo ou não é portão.
5. `go test -race ./...` — o detector de corridas é obrigatório no portão. O `varrimento` é fan-out concorrente sobre N bancos; uma corrida de dados aqui produz números errados, não um crash.

⚠️ **O resíduo da §7.4 não é um sexto portão, e não pode ser.** Mede-se contra os bancos reais, portanto depende de servidores de terceiros estarem de pé — e um portão que amarela por isso deixa de ser lido. Vive no varrimento e é registado; o que o portão verifica é que o **cálculo** do resíduo está certo, contra observações gravadas.

⚠️ **Nem o `-race` é detector de fugas de goroutines.** Medido a 2026-07-25: um teste que deixa uma goroutine bloqueada para sempre passa `go test -race` a verde, sem uma palavra. Onde isso importar — e importa no `varrimento` — a afirmação é separada, com uma sonda que espera a contagem voltar ao valor de partida (medida: 0 falsos positivos em 300 corridas, 50 fugas apanhadas em 50).

⚠️ **E há um sexto portão que não é automático: a corrida de fidelidade por banco.** Cada banco implementado tem um `TestFidelidade…` atrás de `//go:build rede` que lê cada resposta duas vezes — por dois caminhos escritos de propósito para não se parecerem — e compara campo a campo. Não entra no `make verificar` pela mesma razão do resíduo. Corrido nos **cinco** bancos implementados: 3002 ofertas em cada um, **zero divergências** em 147 098 comparações somadas — 36 024 na CGD, 33 022 no Novo Banco, 36 024 no Montepio, 24 016 no Banco CTT e 18 012 no Santander; erro de leitura ≤ 0,100 % a 95 % de confiança, pela regra dos três. ⚠️ Não se lê isso como «acertamos 99,9 %» — o observado é zero, e os 0,100 % são o que a amostra permite excluir. ⚠️ **Nem se lê a coluna das comparações como medida de rigor**: compara-se o que cada banco afirma, e o Santander nomeia metade dos campos da CGD porque publica o preço num plano de troços. Inflacioná-la obrigaria a comparar campos que somos nós a derivar — mediria o derivador, não a leitura.

⚠️ **E a corrida mede uma segunda coisa, que decide desenho: o que custa sondar cada banco.** Pedidos HTTP por simulação, medidos: **1,00** no Banco CTT, 2,03 no Montepio, **4,00** no Santander — que repaga em cada simulação a descoberta da configuração, os limites e o catálogo. Quatro vezes mais carga no banco pela mesma informação. É o argumento medido para reaproveitar config e catálogo dentro de uma corrida do varrimento (§7), e está no `DOSSIE-BANCOS.md` com a tabela completa.
