# API

Duas fronteiras com estatutos diferentes. O esquema executável é `api/openapi.yaml` — é a **fonte da verdade**, de onde se geram os tipos Go (`oapi-codegen`) e os tipos TypeScript da app (`openapi-typescript`). Este documento explica as decisões; o esquema manda nos detalhes.

| fronteira | quem consome | estatuto |
| --- | --- | --- |
| `/api/v1/*` | a app React Native | nossa, versionada, evolui connosco |
| `/api/rate-catalog` | `viabilidade-imobiliaria` | ⚠️ **congelada** — compatível com o v1 |
| `/healthz` | a plataforma | trivial |

---

## 1. `/api/v1` — a app

Nomes em **português**, `snake_case`. Instantes em ISO 8601 com fuso. Valores monetários e taxas saem como **números** JSON, com as casas decimais que a base de dados guarda.

### `GET /api/v1/bancos`

Tudo o que a app precisa para montar o formulário adaptativo. **É a única fonte desta informação** — a app não tem listas de bancos escritas à mão.

```jsonc
{
  "bancos": [
    {
      "id": "novobanco",
      "nome": "Novo Banco",
      "inputs": [
        {"chave": "valor_imovel",      "usa": true},
        {"chave": "rendimento_mensal", "usa": true},
        {"chave": "profissao",         "usa": false,
         "nota": "O simulador do Novo Banco tem o campo, mas a API ignora-o:
                  vai um valor neutro."}
      ],
      "periodos_fixos": [2,3,4,5,10,15,20,25,30],
      "periodos_fixos_modo": "lista",       // "lista" | "da-api" | "do-html"
      "euribor_opcoes": ["3m","6m","12m"],  // vazio = o banco impõe o seu
      "euribor_imposto": null,
      "prazo_min": 1, "prazo_max": 40,
      "idade_maxima_fim": 75,
      "produtos": [
        {"id": "novobanco:primeiro_banco", "rotulo": "Primeiro Banco",
         "descricao": "Domiciliação de ordenado (−0,50 p.p.)", "por_omissao": true}
      ],
      "notas": ["O arrendamento tem spread agravado em 0,50 p.p."]
    }
  ],
  "inputs_canonicos": [
    {"chave": "valor_imovel", "rotulo": "Valor do imóvel", "tipo": "dinheiro"}
  ]
}
```

⚠️ `euribor_opcoes` vazio não é «não sei» — é «o banco impõe o seu e ignora a escolha». A app esbate o campo e mostra o imposto.

⚠️ **O vocabulário de **`inputs_canonicos`** é o do formulário, e pode conter campos que o **`Pedido`** ainda não transporta** — a profissão é o caso de hoje. É deliberado: vários simuladores pedem-na, não só os do grupo BCP, e o que cada API faz com ela apura-se banco a banco, por captura. Até isso estar apurado, o banco declara o campo com `usa` verdadeiro ou falso **e uma nota que diga o que enviamos**. O que não pode acontecer é um banco declarar uma `chave` que não exista no vocabulário: o vocabulário é fechado e vive no domínio, precisamente para não repetir a deriva do `INPUT_LABELS` do v1.

⚠️ **O `custo` saiu deste contrato a 2026-07-28 (KAN-32).** Existia para a app «poder dizer a verdade sobre o tempo», e essa verdade mudou com a inversão da §1: a resposta é imediata para todos os bancos, porque nenhum é interrogado no caminho do cliente. Publicá-lo convidava a app a avisar de uma espera que já não existe. Continua a existir no domínio (`Requisitos.Custo`), onde ainda decide o prazo de cada banco **no varrimento**.

### `POST /api/v1/comparacoes` → `200`

⚠️ **Este documento chamou-lhe `/api/v1/simulacoes` até 2026-07-28, e a rota nunca teve esse nome.** O `openapi.yaml` e o código servem `/api/v1/comparacoes` desde que ela existe (KAN-13). Quem escrevesse um cliente por este documento batia num 404 — e é o modo de falha mais caro que um documento de contrato tem, porque só aparece em runtime, do lado de quem confiou nele. **Quando este ficheiro e o `api/openapi.yaml` discordarem, o que está errado é este ficheiro.**

```jsonc
{
  "bancos": ["cgd", "novobanco", "montepio", "bancoctt"],
  "pedido": {
    "valor_imovel": 250000, "montante": 200000, "prazo_anos": 30,
    "rate_type": "mista", "fixed_period_years": 5, "euribor_indexante": "6m",
    "titulares": [{"data_nascimento": "1990-04-12", "rendimento_mensal": 2200}],
    "finalidade": "propria",        // propria | secundaria | arrendamento
    "localizacao": "continente",
    "garantia_publica": false, "ja_cliente": false
  },
  "produtos": {"novobanco": ["novobanco:primeiro_banco"], "cgd": []}
}
```

⚠️ **Um pedido, uma resposta** (KAN-32, 2026-07-28). Não há `202`, não há identificador para sondar, não há `GET /api/v1/comparacoes/{id}` e não há `503` de «demasiadas em curso». O ciclo antigo descrevia o desenho anterior à inversão da §1: a resposta sai agora de uma consulta à grelha e de cálculo local, e nenhum banco é interrogado neste caminho.

⚠️ **E a ausência do `GET /{id}` é a decisão, não uma lacuna.** Poder sondar uma resposta obrigava a **guardar o pedido que a gerou** — que é precisamente o dado pessoal que este serviço se recusa a ter (§4 do `ARQUITETURA.md`). Não há recurso a que voltar porque não há nada guardado.

⚠️ **E com ele saiu a sondagem, que era uma boa decisão para o problema errado.** Ficava aqui escrito que SSE e streaming são frágeis no React Native e que a sondagem sobrevive a suspensão da app e a mudança de rede — continua a ser verdade, e deixou de ser preciso: não há nada que demore para se sondar.

⚠️ **A resposta traz uma oferta por banco PEDIDO, e não por banco medido** (KAN-45, 2026-08-01). `bancos` vazio quer dizer todos os do registo. Um banco nomeado de que ainda não há preços varridos vem com `sucesso: false` e o código `sem_serie` — **não é omitido**. Até 2026-08-01 era: pediram-se cinco e vieram dois, sem uma palavra sobre os três em falta, porque a lista se filtrava à saída e um banco que nunca lá chegou não podia ser filtrado. O `ECRAS.md` §3 di-lo ao contrário — «um banco que desaparece parece um esquecimento».

Erros: `400` pedido inválido (com o campo nomeado) e `429` tecto por IP excedido (com `Retry-After`).

⚠️ Um id em `bancos` que **não é banco nenhum** é `400` com `campo: "bancos"`, e não uma linha de recusa. «Ainda não temos preços deste banco» e «não há tal banco» são coisas diferentes, e responder à segunda com a primeira ensinava o cliente que um id que escreveu mal é um banco que existe.

Resposta:

```jsonc
{
  "calculado_em": "2026-07-28T18:42:03Z",      // ⚠️ quando ESTA resposta se
                                               // calculou — ver abaixo
  "ofertas": [
    {
      "banco_id": "novobanco", "banco_nome": "Novo Banco",
      "sucesso": true,
      "tan": 3.25, "taeg": 3.61, "spread": 0.90,
      "prestacao_mensal": 870.42, "mtic": 313351.20,
      "pressupostos": [                        // ⚠️ obrigatório com taeg/mtic
        "A TAEG e o MTIC não vêm do banco: são derivados dos encargos medidos
         nos dois extremos de prazo que ele serve.",
        "Assume-se que o encargo recorrente incide sobre o capital em dívida.",
        "A TAEG que vincula alguém vem na ficha de informação normalizada,
         depois de o banco avaliar quem pede."
      ],
      "euribor_indexante": "6m", "euribor_valor": 2.351,
      "fases": [
        {"ate_mes": 60,  "taxa": 3.25, "prestacao": 870.42},
        {"ate_mes": 360, "taxa": 3.25, "prestacao": 870.42}
      ],
      "produtos_aplicados": ["novobanco:primeiro_banco"],
      "aplicado": {},
      "notas": [],
      "capturado_em": "2026-07-28T05:00:11Z"   // ⚠️ obrigatório: a data do
                                               // VARRIMENTO, não a de agora
    },
    {
      "banco_id": "montepio", "banco_nome": "Banco Montepio",
      "sucesso": true, "tan": 3.40,
      "aplicado": {"rate_type": "mista", "fixed_period_years": 30},
      "notas": ["O Banco Montepio não tem taxa fixa pura. Simulado como taxa
                 mista com período fixo igual ao prazo (30 anos)."]
    },
    {
      "banco_id": "bancoctt", "banco_nome": "Banco CTT",
      "sucesso": false,
      "erro": {"codigo": "prazo_impossivel",
               "mensagem": "O crédito teria de terminar aos 78 anos; o Banco CTT
                            exige que termine até aos 75."}
    },
    {
      "banco_id": "santander", "banco_nome": "Santander",
      "sucesso": false,
      "erro": {"codigo": "sem_serie",
               "mensagem": "Ainda não há preços varridos do Santander, e por isso
                            não se lhe conhece oferta para este pedido. O banco
                            não foi consultado — a falta é nossa e não dele."}
    }
  ]
}
```

⚠️ `aplicado` e `notas` não são decoração. Sempre que `aplicado` não está vazio, os números **não** correspondem ao que foi pedido. A app é obrigada a mostrar a nota junto do valor — não numa gaveta escondida. Numa comparação de crédito, devolver números diferentes sem o dizer é enganador.

⚠️ **Dois instantes, e são coisas diferentes.** O `calculado_em` do topo é quando esta resposta se calculou; o `capturado_em` de cada oferta é quando aquele preço foi **medido no banco**. A distância entre os dois é a idade do preço que a pessoa está a ver, e é por isso que viajam ambos e nenhum é opcional numa oferta com sucesso. ⚠️ Este documento chamou-lhe `varrido_em` até 2026-07-28; o contrato nunca teve esse campo.

⚠️ `pressupostos` **é obrigatório sempre que `taeg` ou `mtic` vêm preenchidos**, e é uma lista à parte de `notas` de propósito. Uma `nota` é um aviso sobre o que aconteceu a **este** pedido — o banco encurtou o prazo, o degrau de LTV não estava resolvido. Um pressuposto é uma **hipótese de cálculo** que a MCD obriga a declarar junto do número que dela depende (Anexo I, Parte II; Anexo II). Empacotadas na mesma lista, a app fica sem forma de as apresentar como o que são. Uma TAEG preenchida com `pressupostos` vazio é defeito nosso, não um caso legítimo.

⚠️ **Não há `pedido_efectivo`, e a razão mudou a 2026-07-28.** Este campo existia para a app mostrar o valor que tinha sido mesmo simulado quando a quantização alterava o montante para aproveitar a cache. **A quantização morreu com a cache** (`ARQUITETURA.md` §7): um pedido é avaliado no seu valor exacto, porque o LTV é uma dimensão da grelha e o pedido cai no seu intervalo por construção, em vez de ser arredondado para perto dele. O que o `aplicado` continua a declarar são os ajustes do **banco** — período fixo fora da lista, prazo encolhido pela idade —, que são outra coisa.

⚠️ `erro` é estruturado. `codigo` é para a app decidir o que fazer; `mensagem` é para a pessoa ler, em português. O v1 devolvia texto solto e a UI não conseguia distinguir «este banco não faz isto» de «este banco está em baixo».

Os códigos, e a distinção que cada um serve:

| `codigo` | quer dizer | resolve-se |
|---|---|---|
| `prazo_impossivel` | nem o prazo mínimo do banco cabe na idade de quem pede | mudando o pedido |
| `produto_indisponivel` | varreu-se, e o banco não mede **este** cenário — modalidade, período, LTV fora do que pratica | mudando o pedido, ou não se resolve |
| `banco_indisponivel` | foi-se lá e o banco não respondeu, expirou, deu 5xx | esperando |
| `resposta_ilegivel` | respondeu, e o que veio não se consegue ler | do nosso lado, a corrigir o parser |
| `sem_serie` | **não se foi lá**: ainda não há preços varridos deste banco | correndo o varrimento |

⚠️ O `sem_serie` e o `banco_indisponivel` não são a mesma coisa, e a confusão é cara: o segundo culpa o banco, e no primeiro o banco não teve culpa nenhuma. ⚠️ E o `sem_serie` também não é o `produto_indisponivel`: esse é sobre o **pedido**, este é sobre o **banco inteiro**. Quem os empacotasse numa frase só mandaria a pessoa esperar por uma coisa que não vai acontecer sozinha, ou mudar um pedido que estava bem.

---

## 2. `/api/rate-catalog` — congelada

⚠️ **Compatível com o v1, ao byte.** O consumidor existe e está a correr: `viabilidade-imobiliaria/src/viabilidade/taxas.py`. Os nomes ficam **em inglês e em **`snake_case`, ao contrário de todo o resto do repositório. Mudá-los parte o outro repositório sem aviso nenhum.

`GET /api/rate-catalog?scenario=&bank=&rate_type=&since=&limit=` Cabeçalho: `X-API-Key`.

```jsonc
{
  "scenarios": [
    {"key": "ltv80_mista_30a", "label": "LTV 80% · mista · 30 anos",
     "ltv": 0.8, "valor_imovel": 250000, "montante": 200000,
     "prazo_anos": 30, "rate_type": "mista", "fixed_period_years": 5}
  ],
  "count": 1,
  "points": [
    {"snapshot_id": "…", "captured_at": "2026-07-22T05:00:11",
     "scenario_key": "ltv80_mista_30a",
     "bank_id": "cgd", "bank_name": "CGD", "rate_type": "mista",
     "valor_imovel": 250000, "montante": 200000, "prazo_anos": 30,
     "fixed_period_years": 5,
     "tan": 3.4, "taeg": 3.7, "spread": 1.0,
     "prestacao_mensal": 887.2, "mtic": 319392.0,
     "euribor_indexante": "6m", "euribor_valor": 2.351,
     "products": ["cgd:packs"]}
  ]
}
```

`GET /api/rate-catalog/snapshots` → `{"snapshots":[{"snapshot_id","captured_at","rows"}]}`

⚠️ **Esta rota esteve declarada e sem handler desde que o contrato existe**, e passou a ser servida a 2026-07-28 (KAN-44). O portão não a apanhava por uma razão estrutural: verifica que o código **gerado** está em dia com o spec, não que o **servido** está. Há agora um teste que percorre os caminhos do `openapi.yaml` e falha a nomear o que ficar sem handler.

**Campos que o consumidor lê hoje, e que portanto não podem desaparecer nem mudar de tipo:** `points[].tan`, `.spread`, `.euribor_valor`, `.euribor_indexante`, `.fixed_period_years`, `.bank_id`, `.bank_name`, `.scenario_key`, `.rate_type`, `.prazo_anos`, `.captured_at`; e `scenarios[]`.

⚠️ **Três armadilhas de compatibilidade**, todas cobertas por `teste de contrato` contra uma amostra real capturada do v1:

1. **Números, não strings.** O v1 era Python e serializava `float`. A biblioteca de decimais de Go serializa para string entre aspas por omissão — a conversão faz-se no tipo de saída.
2. `captured_at` sem fuso. O v1 guardava instantes ingénuos e serializava `2026-07-22T05:00:11`, sem `Z`. A base do v2 é `timestamptz`; a serialização **neste endpoint** replica o formato antigo.
3. `products` é sempre uma lista, nunca `null` — quem lê tem de poder distinguir «correu sem produtos» de «não sei», e aqui a lista vazia significa mesmo que nenhum foi aplicado.

Sem chave configurada, o endpoint fica **aberto**. É o comportamento de desenvolvimento e é um aviso alto no arranque.

---

## 3. Transversal

`X-Request-ID` em todas as respostas; todas as linhas de log do mesmo pedido partilham-no. ⚠️ **A query string nunca é registada** — leva chaves.

**Erros**, em toda a API:

```jsonc
{"erro": {"codigo": "pedido_invalido", "mensagem": "…", "campo": "montante"},
 "request_id": "…"}
```

O detalhe interno (SQL, respostas de bancos, caminhos) só sai com a variável de depuração ligada.

**Cabeçalhos de defesa:** `X-Content-Type-Options: nosniff`, `Referrer-Policy`, e HSTS apenas quando já se serve HTTPS — ligar HSTS num host de desenvolvimento prende-o a HTTPS no browser durante meses.

⚠️ **CORS** (executado a 2026-07-28, KAN-22). O v1 servia a UI da mesma origem e não precisava. A app React Native nativa também não — mas a versão web do Expo precisa. Lista explícita em `ORIGENS_PERMITIDAS`, **default vazio**, e vazio quer dizer «só a mesma origem», não «toda a gente». `*` é **recusado ao arranque**, e uma origem mal escrita — com barra final ou caminho — também: nunca casaria com o `Origin` que o browser envia, e falharia em silêncio.

⚠️ **O CORS cobre `/api/v1/*` e o `/healthz`, e NÃO o `/api/rate-catalog`.** Aquele autentica-se por `X-API-Key`, e uma chave dentro de um bundle de browser é uma chave pública. São duas superfícies com públicos diferentes — uma app sem credenciais, uma máquina com chave — e não partilham política de acesso. A app **não manda `X-API-Key` nenhuma**: os endpoints `/api/v1` são públicos e o que os protege é o tecto por IP, não uma credencial. Uma credencial que viaja para o cliente não é uma credencial.

⚠️ **E os preflights não contam para o tecto por IP.** Um browser manda um `OPTIONS` antes de cada `POST /api/v1/comparacoes` — o `Content-Type: application/json` obriga-o —, e a contá-los cada comparação feita da app custava **dois** enquanto a mesma feita por `curl` custava **um**: o tecto passava a medir o cliente em vez do uso. Só é reconhecido como preflight o `OPTIONS` que traga `Access-Control-Request-Method`, senão bastava escolher o método para escapar ao tecto.

## 4. Versionar

`/api/v1` muda por acrescento: campos novos são opcionais e a app antiga continua a funcionar. Remover ou mudar o tipo de um campo obriga a `/api/v2` a correr em paralelo até a app estar actualizada nas lojas — ⚠️ com uma app móvel publicada **não se pode assumir que o cliente actualiza**.

⚠️ **Um `codigo` de erro novo não é uma mudança de versão**, e é por desenho: o campo é `type: string` no `openapi.yaml`, sem enum, e a app mostra a `mensagem` em vez de ramificar no código. Foi o que permitiu ao `sem_serie` nascer sem quebrar nada a jusante. Uma app antiga que receba um código que não conhece mostra a frase em português, que é o que ela já fazia.

`/api/rate-catalog` não é versionada porque não muda — **por agora**.

⚠️ **A congelação é interina.** O `viabilidade-imobiliaria` vai levar o mesmo tratamento que este repositório está a levar, e nessa altura o contrato redesenha-se **em conjunto**: nomes em português como o resto, e a decomposição spread/Euribor que a issue #22 do v1 propunha (guardar o spread, que se move devagar, e recalcular a parte que depende da Euribor, que fixa todos os dias).

Até lá o formato antigo é lei, e por uma razão simples: o consumidor está em produção e não pede licença para ser partido. Quando chegar a altura, é um endpoint novo a nascer ao lado do antigo, e o antigo só desaparece depois de o outro repositório ter migrado.
