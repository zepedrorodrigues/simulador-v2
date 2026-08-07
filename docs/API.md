# API

Duas fronteiras com estatutos diferentes. O esquema executável é **`api/openapi.yaml`** — é a fonte da verdade, e é dele que se geram os tipos Go e os TypeScript da app. **Este documento guarda as decisões; o esquema manda nos detalhes, e quando os dois discordarem é este que está errado.**

| fronteira | quem consome | estatuto |
|---|---|---|
| `/api/v1/*` | a app React Native | nossa, versionada, evolui connosco |
| `/api/rate-catalog` | `viabilidade-imobiliaria` | ⚠️ **congelada** — compatível com o v1 ao byte |
| `/healthz` | a plataforma | trivial |

## 1. `/api/v1` — a app

Nomes em **português**, `snake_case`. Instantes ISO 8601 com fuso. Dinheiro e taxas saem como **números** JSON.

### `GET /api/v1/bancos`

Tudo o que a app precisa para montar o formulário adaptativo. **É a única fonte desta informação** — a app não tem listas de bancos escritas à mão.

⚠️ **`euribor_opcoes` vazio não é «não sei»** — é «o banco impõe o seu e ignora a escolha». A app esbate o campo e mostra o imposto.

⚠️ **O vocabulário de `inputs_canonicos` é fechado e vive no domínio**, precisamente para não repetir a deriva do `INPUT_LABELS` do v1. Pode conter campos que o `Pedido` ainda não transporta — a profissão é o caso de hoje: vários simuladores pedem-na e o que cada API faz com ela apura-se banco a banco, por captura. Até lá o banco declara `usa` verdadeiro ou falso **e uma nota que diga o que enviamos**. O que não pode acontecer é um banco declarar uma `chave` fora do vocabulário.

⚠️ **O `custo` saiu deste contrato** (KAN-32). Existia para a app «dizer a verdade sobre o tempo», e a resposta passou a ser imediata para todos os bancos — publicá-lo convidava a avisar de uma espera que já não existe. Continua no domínio (`Requisitos.Custo`), onde decide o prazo de cada banco **no varrimento**.

### ⚠️ `POST /api/v1/ofertas/{banco}` → `200` — o caminho novo (2026-08-06)

Um pedido, **um banco**. É o que a §1 revertida manda e o que a app passa a usar: dispara um por banco escolhido e mostra a lista a encher-se (D2).

⚠️ **`POST` e não `GET`, e a razão é de privacidade e não de estilo.** Este documento chegou a dizer `GET`, e estava errado: o pedido leva `data_nascimento` e `rendimento_mensal`, e num `GET` isso viaja na **query string** — que fica no histórico do browser, nos logs de qualquer proxy pelo caminho e no `Referer`. É a mesma regra que a `KAN-43` já impõe do nosso lado ao registar `r.URL.Path` e nunca o URL inteiro; de nada serve cumpri-la em casa e mandar os dados na morada.

⚠️ **Consequência prática: não é cacheável por HTTP**, e a cache do §7.6 é nossa, em Postgres, com o pedido em claro **fora** do disco.

⚠️ **Este pedido fala com o banco.** Não sai de nenhuma fotografia — vai ao simulador público do banco com os valores que a pessoa introduziu, e devolve o que ele respondeu. Logo:

- **demora o que o banco demorar**, com timeout nosso por cima;
- um banco que não responde a tempo é `banco_indisponivel`, **nomeado**, e nunca substituído por preço antigo (D3);
- o `capturado_em` da oferta é o instante em que se falou com o banco, e não o de um varrimento.

⚠️ **O tecto por IP conta N pedidos por comparação**, e não um. Quem o dimensionar como se fosse um tranca um utilizador normal a meio da primeira comparação.

**Erros:** `400` pedido inválido com o campo nomeado, `404` id que não é banco nenhum, `429` tecto por IP, e **`503 banco_ocupado`** — o tecto de concorrência **por banco** (§7.2), com `Retry-After`.

⚠️ **O `503 banco_ocupado` e a oferta em falha `banco_indisponivel` dizem coisas diferentes, e a app tem de as tratar como diferentes.** O `200` com `banco_indisponivel` é o banco a não responder: aquele banco fica de fora desta comparação. O `503` é **nosso** — já vão pedidos nossos a mais em curso contra aquele banco —, o banco está bem, e passa sozinho: volta-se a pedir daí a um instante em vez de o riscar da lista. Servir o tecto nosso como oferta em falha culpava o banco por um limite que é nosso, e a app não tinha como distinguir.

⚠️ **Quantos são «pedidos a mais» está em `VAGAS_POR_BANCO`, e são 2 por omissão.** É o que está medido sem falhas (1, 2 e 4 em paralelo contra a CGD, 2026-07-26) e escolheu-se o apertado, não o rápido.

⚠️ **Nem todo o `200` custou um pedido ao banco: o `em_cache` diz qual.** A resposta pode vir da cache do §7.6 — chave = pedido exacto, validade curta (`CACHE_VALIDADE`, 5 min por omissão). Um acerto **não gasta vaga** do tecto por banco, e é essa a razão de a cache se ler antes dele.

⚠️ **O `em_cache` e o `capturado_em` não se substituem, e a app precisa dos dois.** O `capturado_em` diz de **quando é o preço** — num acerto é o instante em que se falou com o banco, nunca o de agora. O `em_cache` diz se **este** pedido chegou a sair. Uma resposta fresca e um acerto de há um segundo têm `capturado_em` quase igual e `em_cache` diferente. ⚠️ E o campo **voltou** (2026-08-07): tinha saído com a inversão da §1, quando tudo vinha da série varrida e ele era sempre verdadeiro.

⚠️ **Uma oferta em falha nunca fica em cache.** Um `banco_indisponivel` é sempre uma ida ao banco agora, e não a memória de um soluço de há minutos — guardá-lo transformava segundos de avaria em indisponibilidade por toda a validade, para toda a gente com o mesmo pedido.

### ⛔ `POST /api/v1/comparacoes` — **retirada a 2026-08-07**

Comparava todos os bancos numa resposta, a partir da série varrida e de cálculo local. Saiu com o varrimento (Fase 6, passo 5): ficou **sem fonte de dados**, e o que a substitui é a app a fazer fan-out sobre o `/ofertas/{banco}`.

⚠️ **Foi uma mudança que PARTE o `/api/v1`**, e a §4 deste documento diz que ele só muda por acrescento. A regra existe por causa de apps nas lojas, e a A8 está bloqueada pela `KAN-24` — **não havia nada publicado**, e era esta a única janela em que sair custava zero. Depois de haver uma versão no terreno, deixa de haver.

⚠️ **O que ela afirmava e continua a valer** está no `/ofertas/{banco}`: um pedido uma resposta, sem `202` e sem `GET /{id}` — porque sondar obrigava a **guardar o pedido**, que é o dado pessoal que este serviço se recusa a ter.

### O que a resposta obriga a mostrar

⚠️ **`aplicado` e `notas` não são decoração.** Sempre que `aplicado` não está vazio, os números **não** correspondem ao que foi pedido, e a app é obrigada a mostrar a nota junto do valor — não numa gaveta. Numa comparação de crédito, devolver números diferentes sem o dizer é enganador.

⚠️ **Dois instantes, e são coisas diferentes.** O `calculado_em` do topo é quando esta resposta se calculou; o `capturado_em` de cada oferta é quando aquele preço foi **medido no banco**. A distância entre os dois é a idade do preço que a pessoa está a ver — por isso viajam ambos e nenhum é opcional numa oferta com sucesso.

⚠️ **`pressupostos` é obrigatório sempre que `taeg` ou `mtic` vêm preenchidos**, e é lista à parte de `notas` de propósito. Uma `nota` é um aviso sobre o que aconteceu a **este** pedido; um pressuposto é uma **hipótese de cálculo** que a MCD obriga a declarar junto do número que dela depende (Anexo I, Parte II; Anexo II). Empacotadas juntas, a app fica sem forma de as apresentar como o que são. Uma TAEG com `pressupostos` vazio é defeito nosso, não um caso legítimo.

⚠️ **`fiabilidade` diz o que se sabe sobre o preço, e é por OFERTA** (KAN-49). Três valores, e só um deles se mostra:

| valor | quer dizer | a app mostra |
| --- | --- | --- |
| `por_confirmar` | ninguém sondou este banco desde que ele foi varrido | nada |
| `confirmada` | a última sonda confirmou a grelha deste banco | nada |
| `em_duvida` | a última sonda **discordou** da grelha, e ainda não se revarreu | ⚠️ a nota, junto do número |

⚠️ **O `em_duvida` obriga a mostrar, os outros dois obrigam a calar.** Uma marca de «confirmada» em toda a gente é ruído com aspecto de informação, e treina quem lê a saltá-la — exactamente o que faria falta no dia em que aparecesse a que importa. A nota vem em `notas`, agarrada ao número, e não numa gaveta.

⚠️ **O estado por omissão é `por_confirmar` e não `confirmada`**, e hoje é o estado de quase tudo: a sonda existe há dias e ainda não corre agendada. Uma omissão que valesse «confirmada» afirmaria sobre toda a série uma coisa que ninguém mediu.

⚠️ **Não há `pedido_efectivo`.** Existia para mostrar o valor realmente simulado quando a quantização alterava o montante para aproveitar a cache. A quantização morreu com a cache: um pedido é avaliado no seu valor exacto, porque o LTV é dimensão da grelha e o pedido cai no seu intervalo por construção. O `aplicado` continua a declarar os ajustes do **banco** — período fixo fora da lista, prazo encolhido pela idade —, que são outra coisa.

### Os códigos de erro, e a distinção que cada um serve

`codigo` é para a app decidir; `mensagem` é para a pessoa ler, em português. O v1 devolvia texto solto e a UI não distinguia «este banco não faz isto» de «este banco está em baixo».

| `codigo` | quer dizer | resolve-se |
|---|---|---|
| `prazo_impossivel` | nem o prazo mínimo do banco cabe na idade | mudando o pedido |
| `produto_indisponivel` | varreu-se, e o banco não mede **este** cenário | mudando o pedido, ou não se resolve |
| `banco_indisponivel` | foi-se lá e não respondeu, expirou, deu 5xx | esperando |
| `resposta_ilegivel` | respondeu, e não se consegue ler | do nosso lado |
| `sem_serie` | **não se foi lá**: não há preços varridos deste banco | correndo o varrimento |
| `serie_desactualizada` | há preços deste banco, e são do outro lado da viragem do dia | correndo o varrimento |

⚠️ O `sem_serie` não é o `banco_indisponivel` — esse culpa o banco, e aqui a falta é nossa. E não é o `produto_indisponivel` — esse é sobre o **pedido**, este é sobre o **banco inteiro**. Empacotá-los mandava a pessoa esperar por uma coisa que não vai acontecer sozinha, ou mudar um pedido que estava bem.

⚠️ **E o `serie_desactualizada` não é o `sem_serie`, apesar de os dois se resolverem varrendo** (`KAN-50`). A diferença é o que se sabe: no `sem_serie` não há preço nenhum deste banco; no `serie_desactualizada` **há**, e não se serve porque a Euribor mudou de dia entretanto e compará-lo com os outros seria comparar preços de fixings diferentes. Servi-lo à mesma era o defeito que a §7.3 do `ARQUITETURA.md` proíbe; dá-lo como `sem_serie` era dizer que não se foi lá, quando se foi.

## 2. ⛔ `/api/rate-catalog` — **retirada a 2026-08-07**

Publicava a série do preçário, congelada e compatível ao byte com o v1, autenticada por `X-API-Key`. Saiu com o varrimento que a alimentava, e com ela saiu a **única superfície autenticada** deste serviço: hoje não há `API_KEYS` nem chave nenhuma.

⚠️ **A D1 resolveu-se por os factos não serem os que estavam escritos.** Cinco documentos deste repositório diziam que o `viabilidade-imobiliaria` consumia **esta** rota **em produção**, e as duas metades eram falsas — verificado a 2026-08-07:

- ele consome a do **v1** (`simulador-credito-habitacao`, o `chmonitor`), como o README e o `CLAUDE.md` dele dizem, e como confirma o cliente em `src/viabilidade/taxas.py` («o chmonitor raspa 10 bancos»);
- **não há produção**: o v2 nunca foi alojado, e o v1 também não — a issue #13 dele, «Verificar o deployment no ambiente real», continua aberta.

Logo a retirada **não partiu consumidor nenhum**, e o v1 continua a servir o que servia.

⚠️ **Um dia o v2 responderá às perguntas do `viabilidade`** — mas **nunca por esta rota**. Ela publica uma série temporal, e sem varrimento não há como a produzir. Esse dia é uma decisão de desenho nova, e o caminho é o ao vivo: perguntar aos bancos um cenário de referência quando alguém precisar dele.

⚠️ **O que se perdeu, e fica dito:** as três armadilhas de compatibilidade que o teste de contrato cobria — números e não strings, `captured_at` sem fuso, `products` sempre lista e nunca `null`. Quem reconstruir uma fronteira para o `viabilidade` volta a encontrá-las, e elas estão no histórico deste ficheiro.

## 3. Transversal

`X-Request-ID` em todas as respostas, partilhado por todas as linhas de log do mesmo pedido. ⚠️ **A query string nunca é registada** — leva chaves.

**Erros, em toda a API:** `{"erro": {"codigo", "mensagem", "campo"}, "request_id"}`. O detalhe interno (SQL, respostas de bancos, caminhos) só sai com a variável de depuração ligada.

**Cabeçalhos de defesa** (KAN-46), em **todas** as respostas e em todas as rotas:

| cabeçalho | valor | porquê |
|---|---|---|
| `X-Content-Type-Options` | `nosniff` | serve-se JSON e mais nada; sem isto, um corpo adivinhado como HTML executa-se como HTML |
| `Referrer-Policy` | `no-referrer` | não há HTML nem ligação para fora, logo não há referrer que valha a pena emitir — e os caminhos dizem o que se está a comparar |

⚠️ **O HSTS não sai daqui, e é decisão.** O serviço fala HTTP em claro atrás do proxy e não tem como saber se o que está à frente serve TLS — a condição «apenas quando já se serve HTTPS» não é observável de dentro. Inferi-la do `X-Forwarded-Proto` obrigaria ao `PROXIES_DE_CONFIANCA`, que está por medir, e passariam a ser duas coisas a falhar juntas. **Emite-o quem termina o TLS.** Há um teste que falha se o serviço começar a emiti-lo.

⚠️ **Aplicam-se também ao `/api/rate-catalog`**, apesar de congelado: a congelação é do **corpo**, e um cabeçalho de resposta não é corpo. O teste de contrato confirma-o — não se assumiu.

⚠️ **E vão nas respostas de erro**, não só no caminho feliz. O middleware é o **primeiro** da cadeia, antes do `Recoverer` e do `limitar`, porque um 429 ou um 500 mal interpretados fazem mais estrago do que um 200. Montado abaixo deles, os 200 levavam os cabeçalhos e os 429 não — e a suite passava na mesma. Está afirmado por reversão.

⚠️ **Este documento prometeu estes cabeçalhos durante meses sem ninguém os emitir.** Medido a 2026-08-01, no ensaio de produção: nenhum saía, e o `grep` sobre todo o `.go` não devolvia uma linha. A falta era silenciosa por natureza — não parte pedidos, não aparece em logs, não muda números.

⚠️ **CORS** (KAN-22): lista explícita em `ORIGENS_PERMITIDAS`, **default vazio**, e vazio quer dizer «só a mesma origem» e não «toda a gente». `*` é **recusado ao arranque**, e uma origem mal escrita — com barra final ou caminho — também: nunca casaria com o `Origin` que o browser envia, e falharia em silêncio.

⚠️ **O CORS cobre `/api/v1/*` e o `/healthz`, e NÃO o `/api/rate-catalog`.** Aquele autentica-se por `X-API-Key`, e uma chave dentro de um bundle de browser é uma chave pública. São duas superfícies com públicos diferentes — uma app sem credenciais, uma máquina com chave — e não partilham política de acesso.

⚠️ **E os preflights não contam para o tecto por IP.** Um browser manda um `OPTIONS` antes de cada `POST` — o `Content-Type: application/json` obriga-o — e a contá-los cada comparação da app custava **dois** enquanto a mesma por `curl` custava **um**: o tecto passava a medir o cliente em vez do uso. Só conta como preflight o `OPTIONS` que traga `Access-Control-Request-Method`, senão bastava escolher o método para escapar ao tecto.

## 4. Versionar

`/api/v1` muda **por acrescento**: campos novos são opcionais e a app antiga continua a funcionar. Remover ou mudar o tipo de um campo obriga a `/api/v2` em paralelo até a app estar actualizada nas lojas — ⚠️ com uma app móvel publicada **não se pode assumir que o cliente actualiza**.

⚠️ **Um `codigo` de erro novo não é mudança de versão**, e é por desenho: o campo é `type: string` sem enum, e a app mostra a `mensagem` em vez de ramificar no código. Foi o que permitiu ao `sem_serie` nascer sem quebrar nada a jusante.

`/api/rate-catalog` não é versionada porque não muda — **por agora**. ⚠️ **A congelação é interina:** quando o `viabilidade-imobiliaria` levar o mesmo tratamento que este repositório levou, o contrato redesenha-se **em conjunto** — nomes em português, e a decomposição spread/Euribor (guardar o spread, que se move devagar, e recalcular a parte que depende da Euribor, que fixa todos os dias). Até lá o formato antigo é lei: o consumidor está em produção e não pede licença para ser partido. Quando chegar a altura, é um endpoint novo a nascer ao lado do antigo.
