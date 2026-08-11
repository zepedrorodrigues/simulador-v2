# API

Uma fronteira, e uma sondagem de saúde. O esquema executável é **`api/openapi.yaml`** — é a fonte da verdade, e é dele que se geram os tipos Go e os TypeScript da app. **Este documento guarda as decisões; o esquema manda nos detalhes, e quando os dois discordarem é este que está errado.**

| fronteira | quem consome | estatuto |
|---|---|---|
| `/api/v1/*` | a app React Native | nossa, versionada, evolui connosco |
| `/healthz` | a plataforma | trivial |

⚠️ **Eram duas até 2026-08-07.** A `/api/rate-catalog` saiu com o varrimento — §2 —, e com ela a única superfície autenticada deste serviço. **Não há hoje credencial nenhuma**, e é decisão: o `/api/v1` é público porque é uma app sem contas a falar com ele, e quem o protege é o tecto por IP.

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

⚠️ **Um instante, e é obrigatório.** O `capturado_em` de cada oferta é quando se falou com o banco. Havia dois — o `calculado_em` do topo dizia quando a resposta se tinha calculado, e a distância entre os dois era a idade do preço —, e o de cima saiu com a `Comparacao`. **A idade não se perdeu:** com cada oferta a vir do banco no momento, o `capturado_em` já é a idade, e num acerto de cache é o instante da ida que a produziu — nunca o de agora. O servidor **recusa servir** uma oferta com preço e sem ele.

### ⚠️ A TAEG e o MTIC são os do banco, e deixaram de ser nossos (2026-08-07)

Dizia-se aqui que a `taeg` e o `mtic` eram **derivados**, que dependiam de um modelo de encargos nosso, e que `pressupostos` era **obrigatório** sempre que um deles vinha preenchido — o Anexo I, Parte II e o Anexo II da MCD mandam declarar as hipóteses junto do número que delas depende.

Era verdade enquanto os preços vinham da série varrida: ela era feita com um titular **neutro** sobre um cenário de referência fixo, logo não existia TAEG medida para a pessoa que perguntava, e o que se fazia era atribuir a encargos a diferença entre a TAEG e a TAN observadas e reamortizar sobre os fluxos deste pedido.

**Ao vivo pergunta-se com os valores desta pessoa, e o simulador do banco devolve a TAEG e o MTIC dele.** Não há o que derivar, não há hipóteses nossas a declarar, e o `pressupostos` **saiu do contrato**.

⚠️ **O que NÃO mudou:** a TAEG de um simulador continua a não ser a que vincula alguém — essa vem na ficha de informação normalizada, depois de o banco avaliar quem pede. É a distinção que a app é obrigada a mostrar, e continua obrigada. O que mudou é que passou a ser uma distinção entre **simulação e proposta**, e não entre **estimado e cotado**.

⚠️ **E as hipóteses do BANCO continuam a chegar**, onde sempre chegaram: em `notas`, palavra por palavra. O Montepio diz lá que projecta a taxa do período fixo para o resto do prazo, e é dele que a frase é.

⚠️ **Foi este modelo que a reversão da §1 matou, e não os parsers.** Quatro assunções dele caíram contra dados varridos num só dia — `KAN-54` a `KAN-57`, os números estão no `DECISAO-AO-VIVO.md` §2. Nenhuma foi apanhada por um teste.

⚠️ **`fiabilidade` saiu** (2026-08-07). Dizia o que a sonda tinha apurado sobre a **grelha** de onde o preço saía (KAN-49), com três valores dos quais só o `em_duvida` se mostrava. Sai com a sonda e com a grelha: ao vivo não há nada entre a resposta do banco e o que se serve. ⚠️ **A regra que ele carregava fica** e vale para o que vier: um estado que só se publica quando as notícias são más ensina quem o lê a tratar a ausência como boa notícia.

⚠️ **Não há `pedido_efectivo`.** Existia para mostrar o valor realmente simulado quando a quantização alterava o montante para aproveitar a cache. A cache voltou (§7.6) e a quantização **não** — a chave é o pedido exacto, e há teste a dizer que um cêntimo dá outra chave. O `aplicado` continua a declarar os ajustes do **banco** — período fixo fora da lista, prazo encolhido pela idade —, que são outra coisa.

### Os códigos de erro, e a distinção que cada um serve

`codigo` é para a app decidir; `mensagem` é para a pessoa ler, em português. O v1 devolvia texto solto e a UI não distinguia «este banco não faz isto» de «este banco está em baixo».

| `codigo` | quer dizer | resolve-se |
|---|---|---|
| `prazo_impossivel` | nem o prazo mínimo do banco cabe na idade | mudando o pedido |
| `produto_indisponivel` | o banco não faz isto de todo — modalidade, período, LTV fora da banda | mudando o pedido, ou não se resolve |
| `banco_indisponivel` | foi-se lá e não respondeu, expirou, deu 5xx | esperando |
| `resposta_ilegivel` | respondeu, e não se consegue ler | do nosso lado |
| `erro_interno` | rebentou uma coisa **nossa**; o banco pode nem ter sido interrogado | do nosso lado, e não passa por esperar |

⚠️ **Eram seis, saíram dois e voltou um** (2026-08-07 e 2026-08-11). O `sem_serie` («não se foi lá: não há preços varridos deste banco», KAN-45) e o `serie_desactualizada` («há, e são do outro lado da viragem do dia», KAN-50) morrem com o varrimento: ao vivo vai-se sempre lá, e o que resta quando não se consegue responder é o banco não ter respondido. O `erro_interno` entra pela `KAN-30`.

⚠️ **A distinção que o `sem_serie` existia para fazer NÃO morre com ele:** uma falta **nossa** não se serve como falha do banco, porque isso manda a pessoa tirar sobre ele uma conclusão que os dados não sustentam. É a mesma razão por que o `503 banco_ocupado` é um estatuto e não uma oferta em falha.

✅ **E foi essa regra que fechou a `KAN-30`** (2026-08-11): um pânico nosso a simular saía como `banco_indisponivel` — dos quatro códigos era o único onde encaixava. O efeito prático é de contabilidade e não de ecrã: o banco ganha fama de instável, a pessoa lê «o CGD está em baixo» com o CGD bem, e o nosso defeito não aparece em métrica nenhuma **porque está contado na coluna errada**.

⚠️ **A `mensagem` de um `erro_interno` não leva o interior do programa** — nem valor do pânico, nem ficheiro, nem linha. Não se perde rasto por isso: rasto não havia. O diário regista método, caminho e estatuto, e o texto do pânico ia só para o telemóvel de quem o apanhou. Pô-lo onde se procura é a `KAN-61`.

⚠️ **O mesmo `erro_interno` já existia no envelope `RespostaErro`** (um `500`, §3), e a repetição é deliberada: as duas dizem «quem está avariado somos nós», em envelopes diferentes. A diferença é o alcance — no `OfertaErro` falhou **este banco** e a lista continua a encher-se; no `RespostaErro` falhou o pedido inteiro.

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

⚠️ **E vão nas respostas de erro**, não só no caminho feliz. O middleware é o **primeiro** da cadeia, antes do `Recoverer` e do `limitar`, porque um 429 ou um 500 mal interpretados fazem mais estrago do que um 200. Montado abaixo deles, os 200 levavam os cabeçalhos e os 429 não — e a suite passava na mesma. Está afirmado por reversão.

⚠️ **Este documento prometeu estes cabeçalhos durante meses sem ninguém os emitir.** Medido a 2026-08-01, no ensaio de produção: nenhum saía, e o `grep` sobre todo o `.go` não devolvia uma linha. A falta era silenciosa por natureza — não parte pedidos, não aparece em logs, não muda números.

⚠️ **CORS** (KAN-22): lista explícita em `ORIGENS_PERMITIDAS`, **default vazio**, e vazio quer dizer «só a mesma origem» e não «toda a gente». `*` é **recusado ao arranque**, e uma origem mal escrita — com barra final ou caminho — também: nunca casaria com o `Origin` que o browser envia, e falharia em silêncio.

⚠️ **O CORS cobre `/api/v1/*` e o `/healthz`, e hoje isso é tudo o que há.** Cobria-os por oposição ao `/api/rate-catalog`, que ficava **de fora**: aquele autenticava-se por `X-API-Key`, e uma chave dentro de um bundle de browser é uma chave pública. A rota saiu, e o grupo de rotas que a separava **fica no `Rotas()`** — é ele que mantém a política aplicada por decisão e não por omissão, e é onde entra a próxima superfície que não seja para browsers.

⚠️ **E os preflights não contam para o tecto por IP.** Um browser manda um `OPTIONS` antes de cada `POST` — o `Content-Type: application/json` obriga-o — e a contá-los cada comparação da app custava **dois** enquanto a mesma por `curl` custava **um**: o tecto passava a medir o cliente em vez do uso. Só conta como preflight o `OPTIONS` que traga `Access-Control-Request-Method`, senão bastava escolher o método para escapar ao tecto.

## 4. Versionar

`/api/v1` muda **por acrescento**: campos novos são opcionais e a app antiga continua a funcionar. Remover ou mudar o tipo de um campo obriga a `/api/v2` em paralelo até a app estar actualizada nas lojas — ⚠️ com uma app móvel publicada **não se pode assumir que o cliente actualiza**.

⚠️ **Um `codigo` de erro novo não é mudança de versão**, e é por desenho: o campo é `type: string` sem enum, e a app mostra a `mensagem` em vez de ramificar no código. Foi o que permitiu ao `sem_serie` nascer — e depois morrer — sem quebrar nada a jusante, e ao `erro_interno` entrar a 2026-08-11 sem regenerar tipo nenhum.

⚠️ **A 2026-08-07 esta regra foi quebrada de propósito, uma vez.** Saíram o `POST /api/v1/comparacoes`, a `Comparacao`, o `pressupostos` e o `fiabilidade` — remoções, não acrescentos. A regra não caiu; caiu a premissa em que ela assenta: **não há app publicada**. A A8 está bloqueada pela `KAN-24`, e esta era a única janela em que partir o `/api/v1` custava zero. **Ela fecha no dia em que houver uma versão no terreno**, e a partir daí uma remoção obriga a `/api/v2` em paralelo.

✅ **O caminho de «esta versão é demasiado antiga» está feito** (2026-08-08), e era o que faltava para essa janela poder fechar em segurança. Feito **antes** de haver uma app publicada de propósito: acrescentá-lo depois não serve de nada, porque as versões que precisavam de o entender já teriam saído sem ele.

Como funciona:

- a app diz quem é no cabeçalho **`X-App-Versao`**, em três números (`1.2.0`);
- o servidor compara com o **`APP_VERSAO_MINIMA`**, e responde **`426 Upgrade Required`** com o código `versao_demasiado_antiga` e uma mensagem que diz **para que versão** actualizar;
- por omissão a verificação está **desligada** — sem `APP_VERSAO_MINIMA` não se recusa ninguém.

⚠️ **O cabeçalho é opcional, e tem de continuar a ser.** Ausente ou ilegível, serve-se na mesma. Exigi-lo era, em si, uma mudança que parte o `/api/v1` — o alvo web e quem experimenta a API por `curl` não o mandam. E tratar o ilegível como velho transformava um defeito de escrita do cliente num bloqueio total: para o corrigir teria de sair uma versão nova, que é o que o bloqueio impede de instalar. **Falha aberto, como o tecto.**

⚠️ **A comparação é numérica e nunca textual.** Em texto, `1.10.0` vem **antes** de `1.9.0` — e o efeito seria mandar parar exactamente as apps mais recentes no dia em que a versão menor passasse de 9 para 10. Há teste a afirmá-lo, e visto a falhar.

⚠️ **Isto não substitui a regra de só mudar por acrescento.** É o travão de emergência para o dia em que ela não chegue: servir uma app velha que interpreta mal o que se lhe manda é pior do que dizer-lhe que pare.

⚠️ **Duas coisas que um browser impõe a este caminho, e que custaram um dia a encontrar** (2026-08-11):

- o `X-App-Versao` **tem** de estar no `Access-Control-Allow-Headers` do preflight. Não é um cabeçalho simples, portanto o que o browser bloqueia sem essa permissão não é o cabeçalho — é o **pedido inteiro**, e do lado do servidor não fica rasto nenhum;
- o `426` **tem** de sair com `Access-Control-Allow-Origin`, e isso decide onde o middleware se monta: por baixo do CORS e não por cima. Escrito acima, a resposta é recusada pelo browser e a app lê-a como falha de rede — ou seja, a única resposta que este caminho existe para entregar é a única que não chega.

**É a regra geral da §3 aplicada aqui:** uma resposta de erro leva os cabeçalhos como as outras.
