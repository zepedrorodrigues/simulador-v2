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

### `POST /api/v1/comparacoes` → `200`

⚠️ **Um pedido, uma resposta** (KAN-32). Não há `202`, identificador para sondar, `GET /{id}` nem `503` de «demasiadas em curso». A resposta sai de uma consulta à grelha e de cálculo local.

⚠️ **E a ausência do `GET /{id}` é a decisão, não uma lacuna.** Sondar obrigava a **guardar o pedido que a gerou** — precisamente o dado pessoal que este serviço se recusa a ter (§4 do `ARQUITETURA.md`). Não há recurso a que voltar porque não há nada guardado.

⚠️ **A resposta traz uma oferta por banco PEDIDO, não por banco medido** (KAN-45). `bancos` vazio quer dizer todos os do registo. Um banco nomeado sem preços varridos vem com `sucesso: false` e código `sem_serie` — **não é omitido**. Antes disso pediam-se cinco e vinham dois, sem uma palavra sobre os três em falta, porque a lista se filtrava à saída e um banco que nunca lá chegou não podia ser filtrado.

**Erros:** `400` pedido inválido com o campo nomeado, `429` tecto por IP com `Retry-After`. ⚠️ Um id em `bancos` que **não é banco nenhum** é `400` com `campo: "bancos"`, e não uma linha de recusa: «ainda não temos preços deste banco» e «não há tal banco» são coisas diferentes, e responder à segunda com a primeira ensinava o cliente que um id mal escrito é um banco que existe.

### O que a resposta obriga a mostrar

⚠️ **`aplicado` e `notas` não são decoração.** Sempre que `aplicado` não está vazio, os números **não** correspondem ao que foi pedido, e a app é obrigada a mostrar a nota junto do valor — não numa gaveta. Numa comparação de crédito, devolver números diferentes sem o dizer é enganador.

⚠️ **Dois instantes, e são coisas diferentes.** O `calculado_em` do topo é quando esta resposta se calculou; o `capturado_em` de cada oferta é quando aquele preço foi **medido no banco**. A distância entre os dois é a idade do preço que a pessoa está a ver — por isso viajam ambos e nenhum é opcional numa oferta com sucesso.

⚠️ **`pressupostos` é obrigatório sempre que `taeg` ou `mtic` vêm preenchidos**, e é lista à parte de `notas` de propósito. Uma `nota` é um aviso sobre o que aconteceu a **este** pedido; um pressuposto é uma **hipótese de cálculo** que a MCD obriga a declarar junto do número que dela depende (Anexo I, Parte II; Anexo II). Empacotadas juntas, a app fica sem forma de as apresentar como o que são. Uma TAEG com `pressupostos` vazio é defeito nosso, não um caso legítimo.

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

⚠️ O `sem_serie` não é o `banco_indisponivel` — esse culpa o banco, e aqui a falta é nossa. E não é o `produto_indisponivel` — esse é sobre o **pedido**, este é sobre o **banco inteiro**. Empacotá-los mandava a pessoa esperar por uma coisa que não vai acontecer sozinha, ou mudar um pedido que estava bem.

## 2. `/api/rate-catalog` — congelada

⚠️ **Compatível com o v1, ao byte.** O consumidor existe e está a correr: `viabilidade-imobiliaria/src/viabilidade/taxas.py`. Os nomes ficam **em inglês e em `snake_case`**, ao contrário de todo o resto do repositório. Mudá-los parte o outro repositório sem aviso.

`GET /api/rate-catalog?scenario=&bank=&rate_type=&since=&limit=`, cabeçalho `X-API-Key`. E `GET /api/rate-catalog/snapshots`, que **esteve declarada sem handler desde que o contrato existe** e passou a ser servida na KAN-44 — o portão não a apanhava porque verifica que o **gerado** está em dia com o spec, não que o **servido** está.

**Campos que o consumidor lê hoje e não podem desaparecer nem mudar de tipo:** `points[].tan`, `.spread`, `.euribor_valor`, `.euribor_indexante`, `.fixed_period_years`, `.bank_id`, `.bank_name`, `.scenario_key`, `.rate_type`, `.prazo_anos`, `.captured_at`; e `scenarios[]`.

⚠️ **Três armadilhas de compatibilidade**, cobertas por teste de contrato contra amostra real do v1:

1. **Números, não strings.** O v1 era Python e serializava `float`; a biblioteca de decimais de Go serializa para string entre aspas por omissão.
2. **`captured_at` sem fuso.** O v1 guardava instantes ingénuos (`2026-07-22T05:00:11`, sem `Z`). A base do v2 é `timestamptz`; a serialização **neste endpoint** replica o formato antigo.
3. **`products` é sempre lista, nunca `null`** — quem lê tem de distinguir «correu sem produtos» de «não sei».

Sem chave configurada o endpoint fica **aberto**: é o modo de desenvolvimento, e o arranque avisa alto.

## 3. Transversal

`X-Request-ID` em todas as respostas, partilhado por todas as linhas de log do mesmo pedido. ⚠️ **A query string nunca é registada** — leva chaves.

**Erros, em toda a API:** `{"erro": {"codigo", "mensagem", "campo"}, "request_id"}`. O detalhe interno (SQL, respostas de bancos, caminhos) só sai com a variável de depuração ligada.

⚠️ **Cabeçalhos de defesa: por fazer (KAN-46).** Este documento prometeu `X-Content-Type-Options: nosniff`, `Referrer-Policy` e HSTS. Medido a 2026-08-01: **nenhum é emitido**, e o `grep` por esses nomes sobre todo o `.go` não devolve uma linha. Fica escrito como falta, e não como promessa.

⚠️ **CORS** (KAN-22): lista explícita em `ORIGENS_PERMITIDAS`, **default vazio**, e vazio quer dizer «só a mesma origem» e não «toda a gente». `*` é **recusado ao arranque**, e uma origem mal escrita — com barra final ou caminho — também: nunca casaria com o `Origin` que o browser envia, e falharia em silêncio.

⚠️ **O CORS cobre `/api/v1/*` e o `/healthz`, e NÃO o `/api/rate-catalog`.** Aquele autentica-se por `X-API-Key`, e uma chave dentro de um bundle de browser é uma chave pública. São duas superfícies com públicos diferentes — uma app sem credenciais, uma máquina com chave — e não partilham política de acesso.

⚠️ **E os preflights não contam para o tecto por IP.** Um browser manda um `OPTIONS` antes de cada `POST` — o `Content-Type: application/json` obriga-o — e a contá-los cada comparação da app custava **dois** enquanto a mesma por `curl` custava **um**: o tecto passava a medir o cliente em vez do uso. Só conta como preflight o `OPTIONS` que traga `Access-Control-Request-Method`, senão bastava escolher o método para escapar ao tecto.

## 4. Versionar

`/api/v1` muda **por acrescento**: campos novos são opcionais e a app antiga continua a funcionar. Remover ou mudar o tipo de um campo obriga a `/api/v2` em paralelo até a app estar actualizada nas lojas — ⚠️ com uma app móvel publicada **não se pode assumir que o cliente actualiza**.

⚠️ **Um `codigo` de erro novo não é mudança de versão**, e é por desenho: o campo é `type: string` sem enum, e a app mostra a `mensagem` em vez de ramificar no código. Foi o que permitiu ao `sem_serie` nascer sem quebrar nada a jusante.

`/api/rate-catalog` não é versionada porque não muda — **por agora**. ⚠️ **A congelação é interina:** quando o `viabilidade-imobiliaria` levar o mesmo tratamento que este repositório levou, o contrato redesenha-se **em conjunto** — nomes em português, e a decomposição spread/Euribor (guardar o spread, que se move devagar, e recalcular a parte que depende da Euribor, que fixa todos os dias). Até lá o formato antigo é lei: o consumidor está em produção e não pede licença para ser partido. Quando chegar a altura, é um endpoint novo a nascer ao lado do antigo.
