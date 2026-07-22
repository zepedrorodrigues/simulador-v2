# API

Duas fronteiras com estatutos diferentes. O esquema executável é
`api/openapi.yaml` — é a **fonte da verdade**, de onde se geram os tipos Go
(`oapi-codegen`) e os tipos TypeScript da app (`openapi-typescript`). Este
documento explica as decisões; o esquema manda nos detalhes.

| fronteira | quem consome | estatuto |
|---|---|---|
| `/api/v1/*` | a app React Native | nossa, versionada, evolui connosco |
| `/api/rate-catalog` | `viabilidade-imobiliaria` | ⚠️ **congelada** — compatível com o v1 |
| `/healthz` | a plataforma | trivial |

---

## 1. `/api/v1` — a app

Nomes em **português**, `snake_case`. Instantes em ISO 8601 com fuso. Valores
monetários e taxas saem como **números** JSON, com as casas decimais que a base
de dados guarda.

### `GET /api/v1/bancos`

Tudo o que a app precisa para montar o formulário adaptativo. **É a única fonte
desta informação** — a app não tem listas de bancos escritas à mão.

```jsonc
{
  "bancos": [
    {
      "id": "novobanco",
      "nome": "Novo Banco",
      "custo": "barato",                    // "barato" | "caro" — dá à app uma
                                            // expectativa honesta de tempo
      "inputs": [
        {"chave": "valor_imovel",       "usa": true},
        {"chave": "first_income",       "usa": true},
        {"chave": "first_occupation_id","usa": false,
         "nota": "O Novo Banco não pergunta a profissão."}
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

⚠️ **`euribor_opcoes` vazio não é «não sei»** — é «o banco impõe o seu e ignora a
escolha». A app esbate o campo e mostra o imposto.

⚠️ **`custo` existe para a app poder dizer a verdade sobre o tempo.** Com os
bancos caros seleccionados, uma comparação demora dezenas de segundos. Esconder
isso produz a impressão de que a app está avariada.

### `POST /api/v1/simulacoes` → `202`

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

Resposta:

```jsonc
{"id": "01J8...", "estado": "em_curso",
 "bancos": ["cgd","novobanco","montepio","bancoctt"],
 "duracao_estimada_s": 6}
```

Erros: `400` pedido inválido (com o campo nomeado), `429` tecto por IP excedido
(com `Retry-After`), `503` demasiadas simulações em curso.

### `GET /api/v1/simulacoes/{id}`

**Sondagem, não streaming.** A app faz `GET` a cada ~1 s enquanto `estado` for
`em_curso`.

⚠️ A escolha é deliberada: SSE e streaming de resposta são frágeis no React
Native (o `fetch` do RN não expõe o corpo em fluxo de forma fiável em todas as
plataformas), e a sondagem sobrevive a suspensão da app, mudança de rede e
reentrada no ecrã. O v1 já sondava e funcionava.

```jsonc
{
  "id": "01J8...", "estado": "em_curso",     // em_curso | terminado
  "pedido_efectivo": {                        // ⚠️ o que foi mesmo simulado
    "montante": 200000, "valor_imovel": 250000,
    "quantizado": false
  },
  "progresso": {"prontos": 3, "total": 4},
  "ofertas": [
    {
      "banco_id": "novobanco", "banco_nome": "Novo Banco",
      "sucesso": true,
      "tan": 3.25, "taeg": 3.61, "spread": 0.90,
      "prestacao_mensal": 870.42, "mtic": 313351.20,
      "euribor_indexante": "6m", "euribor_valor": 2.351,
      "fases": [
        {"ate_mes": 60,  "taxa": 3.25, "prestacao": 870.42},
        {"ate_mes": 360, "taxa": 3.25, "prestacao": 870.42}
      ],
      "produtos_aplicados": ["novobanco:primeiro_banco"],
      "aplicado": {},
      "notas": [],
      "em_cache": true, "capturado_em": "2026-07-22T09:14:02Z"
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
    }
  ]
}
```

⚠️ **`aplicado` e `notas` não são decoração.** Sempre que `aplicado` não está
vazio, os números **não** correspondem ao que foi pedido. A app é obrigada a
mostrar a nota junto do valor — não numa gaveta escondida. Numa comparação de
crédito, devolver números diferentes sem o dizer é enganador.

⚠️ **`pedido_efectivo`** existe pela mesma razão, um nível acima: se a
quantização (`ARQUITETURA.md` §7) alterou o montante para aproveitar a cache, a
app mostra o valor que foi mesmo simulado.

⚠️ **`erro` é estruturado.** `codigo` é para a app decidir o que fazer;
`mensagem` é para a pessoa ler, em português. O v1 devolvia texto solto e a UI
não conseguia distinguir «este banco não faz isto» de «este banco está em baixo».

---

## 2. `/api/rate-catalog` — congelada

⚠️ **Compatível com o v1, ao byte.** O consumidor existe e está a correr:
`viabilidade-imobiliaria/src/viabilidade/taxas.py`. Os nomes ficam **em inglês e
em `snake_case`**, ao contrário de todo o resto do repositório. Mudá-los parte o
outro repositório sem aviso nenhum.

`GET /api/rate-catalog?scenario=&bank=&rate_type=&since=&limit=`
Cabeçalho: `X-API-Key`.

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

**Campos que o consumidor lê hoje, e que portanto não podem desaparecer nem mudar
de tipo:** `points[].tan`, `.spread`, `.euribor_valor`, `.euribor_indexante`,
`.fixed_period_years`, `.bank_id`, `.bank_name`, `.scenario_key`, `.rate_type`,
`.prazo_anos`, `.captured_at`; e `scenarios[]`.

⚠️ **Três armadilhas de compatibilidade**, todas cobertas por
`teste de contrato` contra uma amostra real capturada do v1:

1. **Números, não strings.** O v1 era Python e serializava `float`. A biblioteca
   de decimais de Go serializa para string entre aspas por omissão — a conversão
   faz-se no tipo de saída.
2. **`captured_at` sem fuso.** O v1 guardava instantes ingénuos e serializava
   `2026-07-22T05:00:11`, sem `Z`. A base do v2 é `timestamptz`; a serialização
   **neste endpoint** replica o formato antigo.
3. **`products` é sempre uma lista**, nunca `null` — quem lê tem de poder
   distinguir «correu sem produtos» de «não sei», e aqui a lista vazia significa
   mesmo que nenhum foi aplicado.

Sem chave configurada, o endpoint fica **aberto**. É o comportamento de
desenvolvimento e é um aviso alto no arranque.

---

## 3. Transversal

**`X-Request-ID`** em todas as respostas; todas as linhas de log do mesmo pedido
partilham-no. ⚠️ **A query string nunca é registada** — leva chaves.

**Erros**, em toda a API:

```jsonc
{"erro": {"codigo": "pedido_invalido", "mensagem": "…", "campo": "montante"},
 "request_id": "…"}
```

O detalhe interno (SQL, respostas de bancos, caminhos) só sai com a variável de
depuração ligada.

**Cabeçalhos de defesa:** `X-Content-Type-Options: nosniff`, `Referrer-Policy`,
e HSTS apenas quando já se serve HTTPS — ligar HSTS num host de desenvolvimento
prende-o a HTTPS no browser durante meses.

⚠️ **CORS.** O v1 servia a UI da mesma origem e não precisava. A app React Native
nativa também não — mas a versão web do Expo precisa. A lista de origens
permitidas é explícita e configurável; **nunca** `*` em conjunto com credenciais.

## 4. Versionar

`/api/v1` muda por acrescento: campos novos são opcionais e a app antiga
continua a funcionar. Remover ou mudar o tipo de um campo obriga a `/api/v2` a
correr em paralelo até a app estar actualizada nas lojas — ⚠️ com uma app móvel
publicada **não se pode assumir que o cliente actualiza**.

`/api/rate-catalog` não é versionada porque não muda — **por agora**.

⚠️ **A congelação é interina.** O `viabilidade-imobiliaria` vai levar o mesmo
tratamento que este repositório está a levar, e nessa altura o contrato
redesenha-se **em conjunto**: nomes em português como o resto, e a decomposição
spread/Euribor que a issue #22 do v1 propunha (guardar o spread, que se move
devagar, e recalcular a parte que depende da Euribor, que fixa todos os dias).

Até lá o formato antigo é lei, e por uma razão simples: o consumidor está em
produção e não pede licença para ser partido. Quando chegar a altura, é um
endpoint novo a nascer ao lado do antigo, e o antigo só desaparece depois de o
outro repositório ter migrado.
