# Dossiê dos bancos

O que o v1 apurou sobre cada banco, ao longo de meses de sondagem ao vivo. É
material de referência para quem escreve o banco no v2 — evita redescobrir o que
já custou descobrir uma vez.

⚠️ **Isto é o estado em 2026-07-22.** Endpoints, identificadores e limites mudam
sem aviso. Nada aqui substitui uma captura fresca (ver `CONTRATO-BANCO.md` §3):
serve para saber **onde olhar** e **que armadilhas existem**, não para copiar
valores para o código.

Referências `v1:` apontam para `../simulador-credito-habitacao`.

---

# Fase 1 — HTTP puro, sem browser

## CGD `cgd`

**Transporte:** HTTP simples. **Sem autenticação nenhuma** — sem chave, cookies,
CSRF ou reCAPTCHA. O mais simples dos dez.

**Endpoints** (base `https://simuladorch.cgd.pt`):
1. `GET /` — HTML; dele extraem-se os períodos fixos válidos (widget Kendo).
2. `POST /limits` (form) — limites e elegibilidade.
3. `POST /calculate` (form-urlencoded) — a simulação.

**Cabeçalhos:** `Origin`/`Referer` = `https://simuladorch.cgd.pt`,
`X-Requested-With: XMLHttpRequest`, UA de Windows.

**Payload:** `SimulationSubOriginID=1`, `IsMedidaJovem`, `Purpose=1` (fixo),
`ProductPurpose` (finalidade: 1=própria, 2=secundária, 3=arrendamento),
`PropertyValue`, `Loan`, `tax` (1=fixa, 2=variável, 3=mista), `Years`,
`IndexRateMixedFixed`/`IndexFixedRate` (código do período fixo).
**Ignora dados pessoais por completo** — não pede idade, rendimento nem profissão.

**Resposta:** `BaseResult` e `DiscountedResult` → `AnualNominalRate` (TAN),
`APR` (TAEG), `Spread`, `Instalment`, `TotalPayableAmount` (MTIC),
`VariableIndexRate`. Formato numérico **português**, com espaço não-quebrável
(`"1\xa0303,23"`).

**Limites:** períodos fixos da mista lidos do HTML em runtime, com recurso a uma
lista estática `{5,10,15,20,25,30,35}`. Euribor imposta a 6M. Prazo 1-40 anos,
menor consoante a finalidade (40 na habitação própria, 30 nas outras). LTV ≤ 90 %.

**Produtos:** `cgd:packs` (Vinculação, Ligação, Proteção → usa `DiscountedResult`;
-0,25/-0,25/-0,20 p.p.), por omissão **desligado**.

⚠️ **Medida Jovem tem LTV *mínimo* de 85 %**, além do máximo de 100 %. Fora dessa
banda, erro dedicado.
⚠️ Os períodos fixos vêm de uma expressão regular sobre HTML embebido. Se a CGD
mudar o widget, isto cai calado na lista estática — vale a pena um aviso quando
acontece, não só um recurso silencioso.

## Novo Banco `novobanco`

**Transporte:** HTTP simples. Um só pedido. O endpoint certo **não** é o óbvio —
foi encontrado por captura assistida.

**Endpoint:**
`POST https://srv.novobanco.pt/web/ocb/simhb/site/simulacao/calculo?step=SIM_AVANCADO`

**Cabeçalho único:** `x-nb-oc-channel: 5.2`. Sem cookies, sem lead.

**Payload:** `proponentes[]` (data de nascimento e rendimento reais; NIF,
profissão, habilitações, estado civil e vínculo são **ignorados pela API** —
provado por sondagem, podem ir valores neutros), `contaNB=true` **obrigatório**
(senão erro `V126`), `imovel.localizacao`/`tipoPropriedade`/`tipologia`,
`emprestimos[].valorAquisicao`/`valorEmprestimo`, `prazo`, `tipoTaxa` e
`tipoTaxaIndexante`, `bonificacoes[]`, `nacionalidade="PORTUGUESA"`.

**Resposta:** `resultado.taxas.{tan,taeg,spread,taxaIndexada}`,
`resultado.prestacao.base`, `resultado.mtic`.

⚠️ **`taxaIndexada` só é uma Euribor na taxa variável.** Na mista é a taxa de
referência do período fixo. Reportá-la como Euribor é errado.

**Limites:** mista `MISTA_{2,3,4,5,10,15,20,25,30}_ANOS` (25 e 30 dão o preço de
20); variável sem período; fixa `FIXA_<n>_ANOS` onde **n tem de igualar o prazo**,
e só nos mesmos nove valores. Euribor **escolhível** 3/6/12M — o único dos bancos
de HTTP puro que o permite. Prazo 1-40 anos.

**Produtos:** `novobanco:primeiro_banco` (-0,50 p.p., domiciliação de ordenado) e
`novobanco:protecao` (-0,20 p.p., seguros), ambos **ligados** por omissão.

⚠️ **Erros estruturados como sinal, e isto é o melhor padrão dos dez.** `V159` =
prazo acima do máximo, **e o máximo vem dentro do próprio erro** → reaplica-se e
anota-se. `V126` = `contaNB` falso. `V137` = campos obrigatórios em falta.
⚠️ **O arrendamento muda o preço**: spread +0,50 p.p. medido (0,9 → 1,4). É dos
poucos bancos onde a finalidade não é decorativa.
⚠️ Prazo fixo longo é caro: fixa a 5 anos → TAN 3,89 %; a 30 anos → 5,24 %.

## Montepio `montepio`

**Transporte:** HTTP **com sessão** — não é sem estado. É o banco que prova a
estratégia `HTTPComSessao`.

**Endpoints** (base
`https://simuladores.bancomontepio.pt/ITSCredit.External/Calculator/ITSCredit.Calculator.UI.External`):
1. `GET {base}/calculator/HOUSINGJOURNEY` — fixa os cookies (`ASP.NET_SessionId`
   + Incapsula) e traz o `HashRequest` no HTML (`id="HashRequest" ... value="..."`).
2. `POST {base}/gateway/Calculator/api/Calculator/Calculate?hash={HashRequest}`

⚠️ **Sem o `GET` primeiro, o gateway responde `410` «New open window with
different context».** O hash é estável (é do módulo); os cookies não.

**Payload:** o campo determinante é `ConditionCode`, uma string composta
`{ProductCode}{Fam}-{Filtro}--{Fam}00-{Sufixo}` (ex.: `21H9---H900-M30`):
- `ProductCode` = finalidade (21 própria, 23 secundária, 24 arrendamento — nativas);
- `Fam` = tenor Euribor (`H0`=12M, `H5`=6M, `H9`=3M) — **é assim que se escolhe o
  indexante**;
- `Sufixo` = `V` (variável) ou `M{n}` (mista com n anos de fixo).
- Filtro vazio = preçário base. Com `C029` viriam condições de campanha, que não
  são comparáveis.

Mais `Ammount` (montante) e `Term` em **meses**.

⚠️ **`Ammount` com dois «m» é literal da API do banco.** Não corrigir.
⚠️ **Não reconstruir o código de finalidade partindo o `ConditionCode` por
`--`** — o `---` do filtro vazio parte esse raciocínio.

**Resposta:** `Result.PeriodInstallment[]` por fase (`Duration`, `Installment`,
`TAN`, `Rate.{BaseRateCode,BaseRateValue,Spread}`), mais `TAEG`, `MTIC`, `Spread`,
e `CalculateOutputDetails[]` (encargos).

**Limites:** períodos fixos `{2,5,7,10,15,25,30}`. Euribor 3/6/12M via família,
3M por omissão. Prazo 5-40 anos, limitado por escalão de idade (≤30 anos → 480
meses; ≤35 → 444; resto → 420) **e** por o contrato terminar até aos **76 anos**
(medido por busca binária ao vivo em nove idades).

**Produtos:** `montepio:contrapartidas` (0/2/3/4 produtos detidos), **desligado**
por omissão.

⚠️ **Não existe taxa fixa pura.** A fixa aproxima-se com mista do prazo todo, e
isso **tem** de ser anotado. É o caso que valida o mecanismo de ajuste.
⚠️ **O que o banco anuncia não é o que a API aplica**: o tooltip diz -0,1 p.p. por
contrapartida até -0,4; medido, o spread caiu 1,5 → 0,7 com quatro (-0,8, o dobro).
⚠️ A plataforma é **ITSCredit**, partilhada por vários bancos — se algum dia
entrar outro banco desta plataforma, o trabalho é reaproveitável.

## Banco CTT `bancoctt`

**Transporte:** HTTP simples, um só pedido, sem autenticação.

**Endpoint:** `POST https://simuladorch.bancoctt.pt/api/simulation/simulate`

**Payload:** `AmortizationPeriod` em **meses**, `IndexTypeSubCategoryID` (1 fixa,
2 variável, 3 mista), `IndexTypeID` (mista: 87/82/89/75 = 1/2/3/5 anos de fixo;
fixa: 80/86 = 30/34 anos), `Purpose`, `PropertyType`, `HasCrossSelling=true`
sempre, `YouthMeasuresIsActive`.

**Resposta:** `TAN{With,Without}Benefits`, `TAEG…`, `Spread…`/`VariableSpread…`,
`MTIC…`, `MonthlyInstallmentWith(out)Bonification`, `IndexRate`/`VariableIndexRate`.
Formato numérico português.

**Limites:** mista `{1,2,3,5}` anos; fixa `{30,34}` anos (todo o contrato); prazo
5-40 anos; contrato tem de terminar até aos **75 anos** — imposto por nós, porque
a **API não valida** (aceitou 66 anos de idade com 40 de prazo: fim aos 106).

**Produtos:** `bancoctt:vendas_associadas` (**ligado**),
`bancoctt:sustentavel` (classe energética ≥ A, -0,10 p.p. medido, desligado).

⚠️ **Na mista, `Spread*` é o da fase fixa e vem igual à TAN** (uma fase fixa não
tem indexante). Reportá-lo faria o Banco CTT parecer caríssimo. O correcto é
`VariableSpread*`, o contratual da fase indexada. Mesma armadilha no Santander e
no Bankinter.
⚠️ **Euribor não é escolhível**: variável sempre 12M, pós-fixo da mista sempre 3M
— o próprio bundle do banco força os identificadores. Os outros identificadores
respondem por API, mas são preço que o banco não comercializa. **Ler o tenor real
da resposta** (`IndexRateDescription`/`VariableIndexRateDescription`), não confiar
no nosso mapa.
⚠️ `Purpose=1` e `Purpose=5` provocam um 404 interno embrulhado num `500
INTERNAL_ERROR`. Válidos: 2, 3, 4. Nenhum muda o preço.

---

# Fase 2 — HTTP puro, mais complexo

## Santander `santander`

**Transporte:** HTTP simples, mas com descoberta de configuração em runtime.

**Endpoints:**
1. `GET https://simulador-credito-habitacao.santander.pt/pt-PT/gln-key-simuladorchspa/config.json`
   — traz `client_id` e `bff_url`.
2. `POST {bff}/credit_limit` — `{"type":"SIMULATION_LIMITS", …}`.
3. `GET {bff}/rates` — catálogo de taxas.
4. `POST {bff}/get_by_rates` — a simulação oficial (TAEG e MTIC **do banco**).

**Autenticação:** só o cabeçalho `x-ibm-client-id`, lido do `config.json`.

⚠️ **O `bff_url` remoto é validado antes de ser usado** (exige `https` e host
`*.santander.pt`), senão cai num valor estático. Um `config.json` adulterado ou um
ataque de intermediário mandaria os nossos pedidos para outro lado. **Manter esta
validação.**
⚠️ `/credit_limit` exige `"type":"SIMULATION_LIMITS"` desde 2026-07; sem ele, `400 E309`.
⚠️ Se `/get_by_rates` falhar, o v1 caía numa amortização francesa local com TAEG
e MTIC a nulo. **Preferir falhar com clareza a servir um número inventado com ar
de oficial** — rever esta decisão no v2.

**Limites:** mista `{2,3,4}` anos apenas. Euribor imposta a 6M. Não pede
profissão nem rendimento. Só aceita `HOUSE_PURCHASE` — não distingue arrendamento.

**Produtos:** `santander:bonified` (plano bonificado), **ligado**.

## Crédito Agrícola `creditoagricola`

**Transporte:** HTTP simples. O Feedzai da página não protege a API.

**Endpoints** (base `https://www.creditoagricola.pt`):
1. `POST /api/credit/credit-destinations` — resolve o identificador do produto.
2. `POST /api/credit/calculator` — a simulação.

**Payload:** `rate_id` (M/I/F), `amount`, `term` em **meses**, `evaluation`,
`purpose_id` (1 própria, 2 secundária, **4 arrendamento nativo**),
`date_of_birth`, `insurances=[1,6]`, `is_ca_property`, `with_bonus`,
`is_dl442024`, `is_promotional_spread`, `rates[]`, `vafs: []` **sempre vazio**.

**Resposta:** `rates[]` por fase → `interest_rate` (TAN), `reference_rate_value`,
`term`, `monthly_installment`; mais `taeg`, `totalCostCredit`, `installmentMonth`.

⚠️ **A armadilha mais séria de toda a colecção: `reference_rate_value` e
`rateIndex` são o *spread*, não a Euribor** — apesar de `rateIndexType` dizer
`EUR12TM`. A Euribor obtém-se por `interest_rate - reference_rate_value` na fase
indexada. Validado ao cêntimo contra o Banco CTT (12M = 2,798; 3M = 2,339).
⚠️ **O spread promocional é exclusivo**: combinado com imóvel do banco, ou pedido
em taxa fixa, dá um erro genérico «situação anómala» que não nomeia a causa.
⚠️ DL 44/2024 exige LTV ≥ 85 % e idade ≤ 35 — validar **antes** de chamar a API,
para dar um erro que se entende.

**Limites:** Euribor **1/3/6/12M** (o leque mais largo). Mista `{2,3,5}` anos, com
o prazo a ter de exceder. Fixa `{5,10,15}` anos, com o prazo a ter de **igualar**.
Prazo 2-40 anos, limitado por escalão de idade e por fim até aos 74.

**Produtos:** `creditoagricola:bonificacao` (**ligado**),
`creditoagricola:imovel_ca`, `creditoagricola:spread_promocional`.

---

# Fase 3 — Browser (risco por avaliar)

⚠️ Antes de comprometer, ver `ARQUITETURA.md` §5: em Go isto é `playwright-go` ou
`chromedp`, e há uma saída conhecida (serviço à parte) se não resultar.

## ActivoBank `activobank` e Millennium BCP `millenniumbcp`

**Transporte:** browser **só para cunhar um token OAuth**; a simulação é HTTP.
Partilham a mesma API do grupo BCP, distinguidos por três campos:

| | ActivoBank | Millennium BCP |
|---|---|---|
| `bank` | `ActivoBank` | `MillenniumBcp` |
| `targetSystem` | `Blue` | `PTRetail` |
| `application` | `18` | `26` |

**Endpoints** (base `https://api.millenniumbcp.pt/cj/mortgageexperience/api/rpc`):
1. `POST /SimulationConfigurations` — contexto, regras, limites de prazo.
2. `POST /SimulationScenarios` (`showAll:true`) — lista as opções.
3. `POST /SimulationScenarios` (com `scenarioContext.scenarioId`) — o detalhe.

**Autenticação:** token Bearer cunhado ao carregar o simulador num browser
headless e interceptar o primeiro pedido à API. Capturam-se em runtime
`Authorization`, `ocp-apim-subscription-key`, `accept` e `mbcpobctx`. O v1 tinha
cache do token por processo (600 s) com bloqueio, e repetia o mint em 401/403.

⚠️ **Campos a nulo têm de ser removidos do payload** — a API rejeita `null` com
«Serialization error» desde 2026-07-17.
⚠️ **O Millennium exige uma sequência de cliques** antes de o token ser cunhado
(«não sou», «comprar casa», «casa onde vou viver», vários «Continuar»). Essa
sequência já mudou duas vezes; é o ponto mais frágil dos dois.
⚠️ O mint demora ~25 s. Sem cache, cada simulação paga-o.
⚠️ A Garantia Pública Jovens só devolve opções com LTV ~85-100 %.

**Produtos:** `bcp:life_insurance`, **ligado**.

## Bankinter `bankinter`

**Transporte:** browser a chamar a API **de dentro da página**
(`fetch` via `evaluate`), porque o domínio está atrás de **Cloudflare** e um
cliente HTTP puro leva `403`.

**Endpoints:**
1. `GET https://banco.bankinter.pt/such/proposta-credito-habitacao/home` — limpa
   o desafio do Cloudflare e fixa cookies.
2. `POST https://banco.bankinter.pt/such/api/v1/simulation/executeDetailedSimulation`
   — com `credentials:'include'`, de dentro da página.

⚠️ **O que o Cloudflare bloqueia é a impressão digital de automação, não o
binário.** Chromium normal com UA real,
`--disable-blink-features=AutomationControlled` e `navigator.webdriver` escondido
passa (3/3 medido). O `channel="chrome"` foi abandonado por não haver build para
linux/arm64. **Esta é a peça mais arriscada de portar para Go.**

⚠️ **Números em formato anglo-saxónico** (ponto decimal) — ao contrário de todos
os outros bancos. Fácil de trocar sem reparar; merece teste dedicado.
⚠️ **O MTIC não acompanha a selecção de produtos** — fica preso no valor de «todos
os produtos». O v1 só reportava MTIC quando a selecção era exactamente a de
omissão; caso contrário omitia com nota. Manter essa honestidade.
⚠️ O prazo máximo tem de ser **pré-calculado** por nós (contrato a terminar até
aos 83 anos), porque a API dá `400` mudo, sem dizer qual é o máximo.

**Produtos:** `bankinter:seguro_vida` (-0,20), `bankinter:seguro_multirriscos`
(-0,05), `bankinter:domiciliacao_ordenado` (-0,10) — **todos ligados**.

## Banco BPI `bpi`

**Transporte:** browser a conduzir um formulário OutSystems e a **ler texto da
página**. Não há API JSON conhecida.

**Endpoint:** só a página
`https://www.bancobpi.pt/particulares/credito/credito-habitacao/simulador-credito-habitacao`.
Tudo o resto são postbacks internos.

⚠️ **É o banco mais problemático dos dez, e por larga margem:**
- **52 s de um pedido de 52 s.** Os outros nove ficam prontos aos 26 s. Havia 5 s
  de espera fixa após o banner de cookies, e um poll que dormia 2 s **antes** de
  cada verificação.
- **Extração instável** (~1 em 3 corridas falhava): clicar no `input` de rádio
  não pegava; a correcção foi clicar no `label` associado.
- **Não pede o valor do imóvel** → o LTV não é comparável com os outros bancos.
- Parsing por expressão regular sobre `innerText` inteiro, recortado por
  `str.find`. É o código mais frágil de todo o v1.

**Limites:** períodos fixos `{3,5,10}`; prazo 11-36 anos; distrito fixo em Lisboa;
prazo máximo por idade descoberto **em runtime**, a partir do banner de erro.

**Produtos:** `bpi:vendas_associadas`, **desligado**.

⚠️ **Antes de escrever este banco, gastar meio dia a procurar a API por baixo.**
O OutSystems expõe *screen services*; o Banco CTT e o Crédito Agrícola acabaram
ambos em HTTP puro abaixo de 2,5 s por essa via. Se existir, os 52 s colapsam para
~2 s **e** a instabilidade desaparece com eles. Se não existir, usar selectores
estruturados e esperas por elemento — nunca esperas por tempo nem recorte de
texto.

---

# Padrões transversais

**Bom, a manter:**
- Erros do banco usados como sinal estruturado (o `V159` do Novo Banco traz o
  prazo máximo lá dentro; o banner do BPI também). Muito melhor do que valores
  fixos no nosso código.
- `Produto` como abstracção de bonificação — consistente e reutilizável.
- «Ajusta e anota» em vez de falhar.
- Ler da resposta o que o banco aplicou, em vez de confiar no que pedimos.

**A não repetir:**
- Duas cópias da mesma função de idade e duas de amortização francesa, em
  ficheiros diferentes, por assinaturas ligeiramente distintas.
- Cada banco a reimplementar o seu «abrir browser / fixar cookies / cunhar
  token», sem nunca convergirem.
- Parsing por expressão regular sobre texto de página (BPI).
- Limites de idade medidos empiricamente e depois escritos como constantes
  (83/76/75/74 anos) sem nada que avise quando o banco os mudar.

**Formatos numéricos:** nove bancos devolvem formato português (vírgula decimal,
por vezes com espaço não-quebrável); o **Bankinter** devolve formato anglo-saxónico.
Isto está certo — os bancos devolvem mesmo formatos diferentes — mas é fácil de
trocar sem reparar. Cada banco leva um teste que fixa o seu formato.
