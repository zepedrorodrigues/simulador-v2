# Dossiê dos bancos

O que o v1 apurou sobre cada banco, ao longo de meses de sondagem ao vivo. É material de referência para quem escreve o banco no v2 — evita redescobrir o que já custou descobrir uma vez.

⚠️ **Isto é o estado em 2026-07-22**, excepto onde uma secção diga outra data. Endpoints, identificadores e limites mudam sem aviso. Nada aqui substitui uma captura fresca (ver `CONTRATO-BANCO.md` §3): serve para saber **onde olhar** e **que armadilhas existem**, não para copiar valores para o código.

Referências `v1:` apontam para `../simulador-credito-habitacao`.

---

# Fase 1 — HTTP puro, sem browser

## CGD `cgd`

✅ **Implementado no v2** (`internal/bancos/cgd/`, KAN-9). O que se segue foi **remedido a 2026-07-26** contra o simulador a sério; as capturas estão em `internal/bancos/cgd/capturas/`. Cinco linhas mudaram face ao que aqui estava, e estão marcadas com ⚠️ **corrigido**.

**Transporte:** HTTP simples. **Sem autenticação nenhuma** — sem chave, cookies, CSRF ou reCAPTCHA. O mais simples dos dez.

**Endpoints** (base `https://simuladorch.cgd.pt`):

1. `GET /` — HTML; dele extraem-se os períodos fixos válidos (widget Kendo).
2. `POST /limits` (form) — limites e elegibilidade.
3. `POST /calculate` (form-urlencoded) — a simulação.

**Cabeçalhos:** `Origin`/`Referer` = `https://simuladorch.cgd.pt`, `X-Requested-With: XMLHttpRequest`, UA de Windows.

**Payload:** `SimulationSubOriginID=1`, `IsMedidaJovem`, `Purpose=1` (fixo), `ProductPurpose` (finalidade: 1=própria, 2=secundária, 3=arrendamento), `PropertyValue`, `Loan`, `tax` (1=fixa, 2=variável, 3=mista), `Years`, `IndexRateMixedFixed`/`IndexFixedRate` (código do período fixo). **Ignora dados pessoais por completo** — não pede idade, rendimento nem profissão.

**Resposta:** `BaseResult` e `DiscountedResult` → `AnualNominalRate` (TAN), `APR` (TAEG), `Spread`, `Instalment`, `TotalPayableAmount` (MTIC), `VariableIndexRate`. Formato numérico **português**, com espaço não-quebrável (`"361\xa0670,15"` — bytes C2 A0, UTF-8 limpo).

Na mista, as duas fases vêm explícitas: `FixedDurationMonths`/`FixedAnualNominalRate`/`FixedInstalment` e `Variable*`, e somam exactamente o prazo.

⚠️ **corrigido — a Euribor é o `VariableIndexRate`, e o `IndexValue` não é.** Numa mista o `IndexValue` traz a taxa base da fase fixa (3,000 quando a Euribor está a 2,596); numa fixa traz 3,500, que não é Euribor nenhuma. Numa fixa não há indexante nenhum a declarar.

**Limites:** Euribor imposta a 6M — o dropdown `#IndexRate` tem uma opção e uma só.

⚠️ **corrigido — são duas listas de períodos, não uma.** O widget `#IndexFixedRateView` (fixa) tem **todos os anos de 5 a 40**; o `#IndexRateMixedFixed` (mista) tem só `{5,10,15,20,25,30,35}`. O v1 lia o da mista e usava-o nos dois, o que encaixava uma fixa de 12 anos em 10 — quando a CGD vende os 12.

⚠️ **corrigido — a taxa fixa é ao prazo todo, e o código do período manda no prazo.** O campo `Years` é decorativo em `tax=1`: código de 30 anos com `Years=10` devolve `TotalDuration: 30`; código de 10 anos com `Years=30` devolve 10. Não existe "fixa a 10 anos dentro de um prazo de 30" — isso é a mista.

⚠️ **corrigido — os limites dependem do destino, e a Medida Jovem sobrepõe-se-lhes.** Do `POST /limits`:

| destino | LTV | prazo | idade máx. ao fim | montante máx. |
|---|---|---|---|---|
| habitação própria | ≤ 90 % | ≤ 40 | 70 | 1 000 000 € |
| secundária / arrendamento | ≤ 80 % | ≤ 30 | **75** | 1 000 000 € |
| **com Medida Jovem** (qualquer destino) | 85 % a 100 % | ≤ 40 | 70 | **450 000 €** |

Mínimos: montante 5 000 €, imóvel 10 000 €.

⚠️ **corrigido — a finalidade não mexe no preço.** Própria, arrendamento e Medida Jovem deram todos spread 1,350 e TAN 3,946 no mesmo cenário. A finalidade move os **limites**, não o spread. (Ao contrário do Novo Banco, onde o arrendamento custa +0,50 p.p.)

⚠️ **O que mexe no spread é o LTV, e não em degraus de 5 %.** Varrido ponto a ponto a 2026-07-26 (variável, 30 anos, própria, montante fixo em 200 000 €, a variar o valor do imóvel):

| LTV | spread |
| --- | --- |
| 30 % a 32,5 % | 1,950 |
| 35 % a 66 % | 2,000 |
| **67 %** | **2,050** |
| 68 % a 92 % | 1,350 |

Duas coisas a reter. **O preço desce quando o LTV sobe** — acima de 68 % é 0,65 p.p. mais barato do que abaixo, ao contrário do que se esperaria. E **os degraus não são de 5 %**: 66 %, 67 % e 68 % caem todos na banda 70 do `dominio.BandaLTV` e têm três spreads diferentes, com 0,70 p.p. entre os extremos. Uma grelha com uma linha por banda de 5 % serve a dois deles o preço de outro cliente — está em **KAN-35**.

⚠️ **E a 0,25 p.p. de resolução a tabela acima fica grosseira** (medido 2026-07-26, imóvel 400 000 € e montante a variar de 1 000 €). À volta dos 67 %:

| LTV | 65,00–66,50 | 66,75–67,75 | 68,00–69,00 |
|---|---|---|---|
| spread | 2,000 | **2,050** | 1,350 |

À volta dos 33 %: 1,950 em 32,00–33,00 e 2,000 em 33,50–36,00.

**Duas destas fronteiras não contêm LTV inteiro nenhum** — a de 2,000→2,050 está em (66,50 ; 66,75] e a de 1,950→2,000 em (33,00 ; 33,50]. É a prova de que afinar a grelha para 1 p.p. teria o mesmo defeito da de 5 p.p., só mais pequeno. E o **2,050 é um patamar isolado de \~1,25 p.p.** (\~66,6 % a \~67,9 %), mais caro do que os dois vizinhos: o preço **não é monótono**, o que elimina a bissecção como forma de o descobrir. O argumento completo está na §4 do `ARQUITETURA.md`.

⚠️ **Por medir: os degraus abaixo dos 32 %.** Sabe-se que há pelo menos uma fronteira em (33,00 ; 33,50]; abaixo disso nunca foi varrido.

O prazo e o montante, esses, não mexem: 10 a 40 anos e 100 000 a 400 000 € deram todos 1,350 a LTV 80 %.

**Produtos:** `cgd:packs` (Vinculação, Ligação, Proteção → usa `DiscountedResult`), por omissão **desligado**. Medido: spread 1,350 → 0,650, ou seja **-0,70 p.p.**, e 948,61 € → 869,97 € de prestação. ⚠️ As duas variantes vêm sempre na mesma resposta, e o `dominio.Pedido` não tem por onde se pedir a segunda (KAN-33).

⚠️ **A recusa não diz nada.** Prazo de 45 anos, montante abaixo do mínimo e código de período inválido dão os três exactamente `{"success":false}` — 17 bytes, sem código nem mensagem. E o `/calculate` **aceita um LTV de 96 %** e devolve um preço completo que a CGD não pratica. Os limites têm de ser verificados contra o `/limits` **antes** de calcular: depois da recusa não há nada para explicar a ninguém.

⚠️ Os períodos vêm de uma expressão regular sobre HTML embebido. Se a CGD mudar o widget, isto cai na lista conhecida — no v2 a oferta leva uma **nota** a dizê-lo, em vez de recorrer em silêncio como o v1.

**Números ao vivo a 2026-07-26** (250 000 € de imóvel, 200 000 € financiados, 30 anos, habitação própria, sem packs), para se ver quando envelhecerem:

| taxa | TAN | TAEG | spread | prestação |
|---|---|---|---|---|
| variável | 3,946 | 4,5 | 1,350 | 948,61 € |
| mista 5 anos | 4,350 | 4,9 | 1,350 | 995,62 €, e 954,71 € depois |
| fixa 10 anos | 4,850 | 5,5 | 1,350 | 2 106,68 € (o prazo é 10, não 30) |

**Curvas medidas no mesmo dia** — é o que faz da taxa fixa uma consulta e não um cálculo:

| período | TAN da fixa | TAN da fase fixa da mista |
| --- | --- | --- |
| 5 anos | 4,350 | 4,350 |
| 10 anos | 4,850 | 4,850 |
| 20 anos | 5,000 | 5,000 |
| 30 anos | 5,250 | — |

**Concorrência:** oito pontos contra a CGD, com um pedido de cada vez, custaram 1,086 s cada; com dois, 603 ms; com quatro, 367 ms — **sem falhas em nenhum dos três**. O varrimento ficou nos dois (`PorBancoOmissao`), por ser o que chega em hora morta.

**Os 36 códigos da taxa fixa foram varridos um a um, e todos devolvem exactamente o prazo que anunciam.** Não há armadilha no mapa: se um prazo sair diferente do pedido, o problema é nosso — a lista de recurso `{5,10,15,20,25,30,35}` encaixa 31 e 32 em 30, e 29 em 25.

⚠️ **A curva da fixa satura aos 25 anos.** São 36 prazos e apenas **21 preços distintos**: sobe de 4,350 (5 anos) até 5,250 (25 anos) e depois fica plana — 25, 30, 35 e 40 anos custam todos o mesmo. Para a grelha (KAN-16) são 21 pontos nesta dimensão, não 36.

**Fidelidade da leitura, medida a 2026-07-26:** 3002 ofertas confrontadas campo a campo com o corpo em bruto que as originou, por um leitor independente — 36 024 comparações, **zero divergências**. Limite superior do erro de leitura: **0,100 %** a 95 % de confiança. O harness é o `TestFidelidadeDoQueGuardamosFaceAoQueOBancoRespondeu`, atrás de `//go:build rede`.

⚠️ Foi essa corrida que apanhou o defeito da lista de recurso: com o HTML ilegível, um prazo de 32 anos vira 30 — aceitável — mas a nota que saía dizia «este banco não os aceita (a taxa fixa da CGD só existe entre 5 e 40 anos)», o que é **falso**, porque a CGD vende os 32. Pôr na boca do banco uma recusa que ele não fez é pior do que encolher o prazo.

## Novo Banco `novobanco`

✅ **Implementado no v2** (`internal/bancos/novobanco/`, KAN-10). O que se segue foi **remedido a 2026-07-26** contra o simulador a sério; as capturas estão em `internal/bancos/novobanco/capturas/`. Quatro linhas mudaram face ao que aqui estava, e estão marcadas com ⚠️ **corrigido**.

**Transporte:** HTTP simples. Um só pedido. O endpoint certo **não** é o óbvio — foi encontrado por captura assistida.

**Endpoint:** `POST https://srv.novobanco.pt/web/ocb/simhb/site/simulacao/calculo?step=SIM_AVANCADO`

**Cabeçalho único:** `x-nb-oc-channel: 5.2`. Sem cookies, sem lead.

✅ **Confrontado com o bundle do simulador a 2026-07-26.** O bundle está em `srv.novobanco.pt/web/ocp/simhb/site/assets/index-*.js` (⚠️ `ocp`, e não o `ocb` da API). Confirma-se que é o mesmo caminho — `CALCULATE_SIMULATION: /simulacao/calculo` com `?step=`, e o `x-nb-oc-channel` como único cabeçalho — logo, **o preço que o site mostra é o que este endpoint devolve, por construção**. Confirmam-se também, do próprio bundle, as três listas que o v2 declara: mista e fixa nos mesmos nove valores `{2,3,4,5,10,15,20,25,30}`, e a Euribor nos três tenores. Nenhuma delas tem no bundle opções que a API aceite e o simulador não venda — que era o risco a despistar (ver «Uma API aceitar não é o banco vender», nos padrões transversais).

**Payload:** `proponentes[]` (data de nascimento e rendimento reais; NIF, profissão, habilitações, estado civil e vínculo são **ignorados pela API** — podem ir valores neutros), `contaNB=true` **obrigatório** (senão erro `V126`), `imovel.localizacao`/`tipoPropriedade`/`tipologia`, `emprestimos[].valorAquisicao`/`valorEmprestimo`, `prazo`, `tipoTaxa` e `tipoTaxaIndexante`, `bonificacoes[]`, `nacionalidade="PORTUGUESA"`.

⚠️ **corrigido — o rendimento também não mexe no preço.** Remedido campo a campo a 2026-07-26 contra a mesma simulação: profissão, habilitações, estado civil, vínculo, situação profissional, NIF, tipologia, localização e **rendimento** deram todos o mesmo cêntimo. O rendimento só entra no `recomendacao.dsti`. O que a API lê de facto é a **data de nascimento**, e só para o prazo máximo. ⚠️ Isto não dispensa a nota: o simulador **exige** os campos, e o que se preenche por nós declara-se com `Usa: true` (CONTRATO-BANCO.md §5) — foi exactamente aqui que o v1 declarou `requires_occupation=False` enquanto os enviava.

**Resposta:** `resultado.taxas.{tan,taeg,spread,taxaIndexada}`, `resultado.prestacao.base`, `resultado.mtic` — e as mesmas em `…SemBonificacao`, na mesma resposta. Números nativos de JSON (não texto em formato português, ao contrário da CGD).

⚠️ `taxaIndexada` só é uma Euribor na taxa variável. Na mista é a taxa de referência do período fixo. Reportá-la como Euribor é errado.

⚠️ **A mista não traz plano nenhum.** A resposta dá uma TAN só — a do período fixo — e **não diz** que taxa, que indexante nem que prestação se aplicam depois dele. No v2 a mista sai por isso **sem fases** e com uma nota que o diz; publicar aquela TAN como se durasse o prazo todo era inventar.

**Limites:** mista `MISTA_{2,3,4,5,10,15,20,25,30}_ANOS` (20, 25 e 30 dão o mesmo preço); variável sem período; fixa `FIXA_<n>_ANOS` onde **n tem de igualar o prazo**, e só nos mesmos nove valores. Euribor **escolhível** 3/6/12M — o único dos bancos de HTTP puro que o permite; a omissão do próprio simulador é 12M. Prazo 1-40 anos. Um código fora dos nove dá `omc.fwk.genericError`, que o banco não nomeia.

⚠️ **corrigido — na mista o período tem de ser estritamente menor do que o prazo.** Uma mista de 25 anos num prazo de 25 é recusada com **`V158`**; a 26 passa. O dossiê não registava nem a regra nem o código.

⚠️ **corrigido — o prazo máximo não é só «75 menos a idade».** Varrido a 2026-07-26 em 16 idades, o máximo é `min(escalão, 75 − idade)`, com o escalão a descer por patamares: **≤ 30 anos → 40**, **31-35 → 37**, **≥ 36 → 35**. Medido nas fronteiras: 30→40, 31→37, 35→37, 36→35, 41→34, 46→29, 64→11. ⚠️ São **os mesmos escalões do Montepio** (480/444/420 meses). Manda o titular **mais velho**, independentemente da ordem em que vão no payload.

**Produtos:** `novobanco:primeiro_banco` (-0,50 p.p., domiciliação de ordenado) e `novobanco:protecao` (-0,20 p.p., seguros), ambos **ligados** por omissão. Medido: as duas juntas levam o spread de 1,600 a 0,900, e cada uma isolada dá 1,100 e 1,400.

⚠️ **O preço que o v2 publica para este banco é, por isso, um preço com desconto** — ao contrário da CGD, cujos packs estão desligados por omissão. Enquanto o `dominio.Pedido` não transportar selecção de produtos (**KAN-33**), a diferença sai como nota na oferta. Compará-los sem essa nota é comparar coisas diferentes. Medido no cenário de referência: **80,95 €/mês** e **29 819 €** de MTIC entre ter e não ter as bonificações.

⚠️ **Erros estruturados como sinal, e isto é o melhor padrão dos dez.** Os seis códigos medidos:

| código | quer dizer | traz o limite? |
|---|---|---|
| `V159` | prazo acima do máximo para a idade | **sim**, o máximo em `parameters["0"]` |
| `V157` | na fixa, prazo ≠ período | **sim**, o prazo exigido em `parameters["0"]` |
| `V158` | na mista, período ≥ prazo | não |
| `V126` | `contaNB` falso | não |
| `V118` | prestação acima do tecto | **sim**, 7500 € em `parameters["1"]` |
| `V137` | campos obrigatórios em falta | não |

Os parâmetros vêm prefixados com `DONT_TRANSLATE:` (`"DONT_TRANSLATE:35"` = 35) e há-os inteiros e decimais (`"7500.0"`).

⚠️ **O arrendamento muda o preço**: spread +0,50 p.p. medido (0,9 → 1,4). É dos poucos bancos onde a finalidade não é decorativa. A **segunda habitação tem o preço da própria**.

⚠️ **corrigido — a curva da fixa satura aos 20 anos, não aos 30.** Medido a 2026-07-26: 3 anos → 3,972; 5 → 3,992; 10 → 5,124; 15 → 5,240; e **20, 25 e 30 dão todos 5,270**. São 9 prazos e **7 preços distintos**. (Os valores de 2026-07-22 — 3,89 aos 5 anos e 5,24 aos 30 — envelheceram com o preçário.)

⚠️ **O LTV mexe no spread, e aqui em degraus alinhados com os 5 %.** Varrido ponto a ponto a 2026-07-26 (variável 12M, 30 anos, própria, montante fixo em 200 000 €, a variar o valor do imóvel):

| LTV | spread |
| --- | --- |
| 30 % a 50 % | 0,750 |
| 51 % a 70 % | 0,800 |
| 71 % a 80 % | 0,900 |
| 81 % a 100 % | 0,950 |

Duas coisas a reter, e as duas contrariam a CGD. **O preço sobe quando o LTV sobe**, que é a direcção que se esperaria — na CGD desce. E **os degraus caem exactamente nas fronteiras do** `dominio.BandaLTV`: as quebras são em 50/51, 70/71 e 80/81, e as bandas de 5 % representam este banco sem perder um cêntimo. ⚠️ É o contra-exemplo que o **KAN-35** precisava: a grelha de 5 % serve o Novo Banco na perfeição e **não** serve a CGD, cuja quebra aos 67 % não é representável. Um banco não decide a resolução da grelha; dois já mostram que ela tem de ser mais fina do que 5 % ou definida por banco.

⚠️ **A 0,25 p.p. vê-se de que lado a fronteira fecha** (medido 2026-07-26, mesmas condições da CGD): 0,75 em 49,50–50,00 e 0,80 em 50,25–51,50; 0,90 em 79,50–80,00 e 0,95 em 80,25–81,50. As quebras caem em (50,00 ; 50,25] e (80,00 ; 80,25] — logo **acima** do múltiplo de 5, que é o que «LTV até 50 %» quer dizer: a fronteira é **fechada em cima**. Confirma a leitura da tabela grosseira em vez de a contrariar, ao contrário do que acontece na CGD.

⚠️ **corrigido — há um endpoint de limites, e o dossiê dizia que não havia.** Confrontado com o bundle do simulador a 2026-07-26 (`srv.novobanco.pt/web/ocp/simhb/site/assets/index-*.js`): o `GET /configuracoes` devolve 432 bytes, sem parâmetros nenhuns, com os limites todos.

| campo | valor |
|---|---|
| `montanteFinanciarMinimo` / `Maximo` | 2 000 € / 1 800 000 € |
| `valorImovelMinimo` / `Maximo` | 11 000 € / 2 000 000 € |
| `prestacaoMinima` / `Maxima` | 30 € / **7 500 €** |
| `idadeMinima` / `Maxima` | 18 / **75** |
| `montanteMaximoDeficiente` | 238 273,23 € |

O `prestacaoMaxima: 7500` confirma ao euro a leitura que se tinha feito do parâmetro do `V118`, e o `idadeMaxima: 75` confirma o tecto do contrato. ⚠️ **O v2 consulta este endpoint desde 2026-08-15 (KAN-37)**: pede-o antes do `/calculo`, guarda-o em catálogo (24 h, como os períodos da CGD), e recusa **em casa** — com uma mensagem que nomeia o limite e o valor — um montante ou imóvel abaixo do mínimo ou acima do máximo, e um titular mais velho acima dos 75. ⚠️ **Falha aberto**: se o endpoint não responder ou não se deixar ler, simula-se na mesma — o `/calculo` é a autoridade, e uma guarda que trava por não saber é pior do que não existir.

⚠️ **E há um `POST /simulacao/prazo-maximo`**, que devolve `{"data":{"prazo":N}}` a partir da data de nascimento — sem ser preciso provocar o `V159`. Medido a 2026-07-26, dá exactamente os mesmos números que o erro: 1996→40, 1990→35, 1980→29, 1962→11. É confirmação independente da regra dos escalões. O v2 continua a usar o `V159`, e isso é uma escolha e não um esquecimento: o `V159` só custa um pedido a mais **quando o prazo excede**, enquanto perguntar antes custaria um pedido a mais **sempre**.

⚠️ **O endpoint preça LTV de 100 %**, que o banco não comercializa — como o `/calculate` da CGD aceita 96 %. E o `/configuracoes` **não traz tecto de LTV nenhum**: nem aí a elegibilidade por LTV é verificável.

**Números ao vivo a 2026-07-26** (250 000 € de imóvel, 200 000 € financiados, 30 anos, própria, titular nascido a 1990-01-15, com as duas bonificações), para se ver quando envelhecerem:

| taxa | TAN | TAEG | spread | prestação |
|---|---|---|---|---|
| variável 12M | 3,698 | 4,4 | 0,900 | 920,34 € |
| variável 6M | 3,496 | 4,2 | 0,900 | 897,64 € |
| variável 3M | 3,239 | 3,9 | 0,900 | 869,21 € |
| mista 5 anos | 3,992 | 4,7 | 0,900 | 953,91 € (só o período fixo) |
| fixa 10 anos | 5,124 | 5,9 | 0,900 | 2 133,45 € (o prazo é 10) |

**Concorrência e custo:** 3392 simulações com dois trabalhadores custaram 230 ms cada, **sem uma única falha**. São 4218 pedidos HTTP para 3392 simulações — os 826 a mais são reaplicações do V159. Ao todo, **7,3 MB**: um décimo do que a mesma amostra custou à CGD, porque aqui não há página de 77 KB a puxar por cada fase fixa.

**Fidelidade da leitura, medida a 2026-07-26:** 3002 ofertas confrontadas campo a campo com o corpo em bruto que as originou, por um leitor independente — 33 022 comparações, **zero divergências**. Limite superior do erro de leitura: **0,100 %** a 95 % de confiança. O harness é o `TestFidelidadeDoQueGuardamosFaceAoQueOBancoRespondeu`, atrás de `//go:build rede`.

⚠️ Foi a preparação dessa corrida que apanhou o defeito da **cadeia de ajustes ao prazo**: com a idade a encolher o prazo pelo V159 *e* a fixa a encolhê-lo outra vez para a lista dos nove, saíam dois ajustes, e o segundo dizia «Pediu 35 anos» a quem tinha pedido 40. É o mesmo defeito que a corrida da CGD tinha apanhado lá, e a correcção foi a mesma: **um** ajuste ao prazo, sempre contra o prazo que a pessoa pediu. A corrida seguinte, já com a correcção, não encontrou nenhuma cadeia em 3002 ofertas.

## Montepio `montepio`

✅ **Implementado no v2** (`internal/bancos/montepio/`, KAN-11). O que se segue foi **remedido a 2026-07-27** contra o simulador a sério; as capturas estão em `internal/bancos/montepio/capturas/`. O que aqui estava confirmou-se todo, e sete linhas são novas — marcadas com ⚠️ **novo**.

**Transporte:** HTTP **com sessão** — não é sem estado. É o banco que prova a estratégia `HTTPComSessao`.

**Endpoints** (base `https://simuladores.bancomontepio.pt/ITSCredit.External/Calculator/ITSCredit.Calculator.UI.External`):

1. `GET {base}/calculator/HOUSINGJOURNEY` — fixa os cookies (`ASP.NET_SessionId` + Incapsula) e traz o `HashRequest` no HTML (`id="HashRequest" ... value="..."`).
2. `POST {base}/gateway/Calculator/api/Calculator/Calculate?hash={HashRequest}`

⚠️ **Sem o** `GET` primeiro, o gateway responde `410` «New open window with different context». O hash é estável (é do módulo); os cookies não.

**Payload:** o campo determinante é `ConditionCode`, uma string composta `{ProductCode}{Fam}-{Filtro}--{Fam}00-{Sufixo}` (ex.: `21H9---H900-M30`):

- `ProductCode` = finalidade (21 própria, 23 secundária, 24 arrendamento — nativas);
- `Fam` = tenor Euribor (`H0`=12M, `H5`=6M, `H9`=3M) — **é assim que se escolhe o indexante**;
- `Sufixo` = `V` (variável) ou `M{n}` (mista com n anos de fixo).
- Filtro vazio = preçário base. Com `C029` viriam condições de campanha, que não são comparáveis.

Mais `Ammount` (montante) e `Term` em **meses**.

⚠️ `Ammount` com dois «m» é literal da API do banco. Não corrigir.

⚠️ **Não reconstruir o código de finalidade partindo o** `ConditionCode` por `--` — o `---` do filtro vazio parte esse raciocínio.

**Resposta:** `Result.PeriodInstallment[]` por fase (`Duration`, `Installment`, `TAN`, `Rate.{BaseRateCode,BaseRateValue,Spread}`), mais `TAEG`, `MTIC`, `Spread`, e `CalculateOutputDetails[]` (encargos).

⚠️ **novo — o código da Euribor a 12 meses é `EH2`, não «EH12».** Os três: `EH3` = 3M, `EH6` = 6M, `EH2` = 12M. Um mapa a partir da família que se pediu acerta por acaso e nunca vê uma renumeração do lado do banco — lê-se da resposta (`CONTRATO-BANCO.md` §5).

⚠️ **novo — na mista, a fase indexada repete a TAN e a prestação da fase fixa.** Medido: com `M5` num prazo de 30, a segunda fase vem com `Duration: null`, TAN 4,350 e prestação 995,62 — os mesmos da fase fixa — apesar de a `Rate` dela declarar `EH3` a 2,339 mais 1,500 de spread, que dariam 3,839. A aritmética diz qual é qual: 995,62 × 300 − 181 898,17 = 116 788, que é o `TotalInterest` dessa fase (116 790,53); a 3,839 % seriam 101 320. **O banco projecta a cauda à taxa do período fixo**, e é daí que saem o MTIC e a TAEG dele. O v2 não publica plano de fases na mista por causa disto, e diz porquê numa nota.

⚠️ **novo — a tabela de prazo máximo por idade vem no HTML do arranque.** O `model.Configs` traz `maxmortgageterm: "00-30:40;31-35:37;36-99:35;"` — as mesmas faixas que o v1 tinha medido por busca binária, mas ditas pelo banco. O v2 lê-as de lá, com a tabela medida só como reserva: uma tabela nossa envelhece em silêncio, a dele não.

⚠️ **novo — no arrendamento a prestação inclui o Imposto do Selo sobre os juros.** `HasIS: true` e `IsISInstallmentOut: false`, e a prestação sobe de 936,36 para 953,97 **com a mesma TAN de 3,839 %** — são 4 % dos juros. Na própria e na segunda habitação fica fora. Sem o dizer, o Montepio aparece ao lado dos outros 1,9 % mais caro sem razão visível.

⚠️ **novo — o LTV não muda o preço do crédito.** Medido a 50, 70, 80, 90 e 100 %: spread 1,500 e TAN 3,839 em todos. O que muda com o valor do imóvel são os encargos iniciais (IMT, imposto do selo, avaliação) e, por eles, o MTIC. Para a grelha de preço (KAN-16) isto quer dizer um degrau de LTV só.

⚠️ **novo — a idade entra no MTIC pelo seguro de vida.** No mesmo crédito, o prémio mensal do `008 PPCH Vida` foi 15,25 € aos 30 anos, 18,84 € e 20,32 € a duas datas diferentes dos 36. A TAN, o spread e a prestação não se mexem. Um MTIC de varrimento neutro não é o MTIC de ninguém em concreto.

**Limites:** períodos fixos `{2,5,7,10,15,25,30}` — confirmados um a um a 2026-07-27 (1, 3, 12, 20 e 35 são recusados). Euribor 3/6/12M via família, 3M por omissão. Prazo 5-40 anos (o mínimo medido: 4 anos são recusados, 5 passam), limitado por escalão de idade (≤30 anos → 480 meses; ≤35 → 444; resto → 420) **e** por o contrato terminar até aos **76 anos** (com 70 anos, 6 anos de prazo passam e 7 não). ⚠️ Os escalões são **os mesmos do Novo Banco** (40/37/35 anos) — se um terceiro banco os repetir, vale a pena perguntar de onde vêm.

⚠️ **novo — as recusas não classificam a causa, e chegam a nomear a errada.** Três modos medidos: (a) período fixo inexistente → `Status: NOk` com `Message` **vazia** e `Code` nulo; (b) prazo acima do máximo por idade, prazo abaixo do mínimo **e** período fixo maior do que o prazo → todos com «Não existem condições disponíveis para a idade dos proponentes», mesmo a um titular de 36 anos com prazo de 20; (c) prazo inválido por outro caminho → `ITSCredit...Calculate: term is invalid`. **Ao contrário do Novo Banco, não há aqui erro estruturado que se possa reaplicar** — por isso o v2 impõe prazo, período e modalidade antes de ir à rede.

**Produtos:** `montepio:contrapartidas` (0/2/3/4 produtos detidos), **desligado** por omissão.

⚠️ **Não existe taxa fixa pura.** A fixa aproxima-se com mista do prazo todo, e isso **tem** de ser anotado. É o caso que valida o mecanismo de ajuste. ⚠️ **novo:** quando o prazo é um dos períodos praticados (30, por exemplo), o banco devolve **uma** fase e o que sai é taxa fixa a todo o contrato — não há ajuste a fazer, só a nota de que o produto contratado é o de taxa mista. Quando não é (20 anos, por exemplo), o mais longo que cabe são 15 e sobra cauda indexada: aí o produto **é** misto, e isso vai como ajuste de tipo de taxa.

⚠️ **O que o banco anuncia não é o que a API aplica**: o tooltip diz -0,1 p.p. por contrapartida até -0,4; medido, o spread caiu 1,5 → 0,7 com quatro (-0,8, o dobro). ⚠️ **novo — a curva completa**, medida a 2026-07-27: 0 → 1,500; **2 → 1,100**; **3 → 0,900**; **4 → 0,700**. Não é linear por produto: os dois primeiros valem -0,4 juntos, e cada um a seguir -0,2.

**Fidelidade da leitura, medida a 2026-07-27:** 3002 ofertas confrontadas campo a campo com o corpo em bruto que as originou, por um leitor independente — 36 024 comparações, **zero divergências**. Limite superior do erro de leitura: **0,100 %** a 95 % de confiança. O harness é o `TestFidelidadeDoQueGuardamosFaceAoQueOBancoRespondeu`, atrás de `//go:build rede`, e vigia também as afirmações próprias deste banco: que o indexante sai da fase que o banco marcou, que a mista **não** publica plano, que o ajuste ao prazo é um só, e que as notas do Imposto do Selo e das contrapartidas dizem os números que o corpo dá.

**Concorrência e custo, medidos na mesma corrida:** 3002 simulações com dois trabalhadores custaram **1,721 s cada**, sem uma única falha, em 1h27m. São **6097 pedidos HTTP** para 3002 simulações — 3049 arranques e 3048 cálculos — e **39,6 MB**. ⚠️ É **o mais lento dos cinco a sondar** — e continua a sê-lo depois de o Banco CTT e o Santander entrarem —, e a razão é estrutural: cada simulação paga o arranque que fixa os cookies. Sete vezes o custo por simulação do Novo Banco (230 ms). ⚠️ Lento não é o mesmo que caro **para o banco**: em pedidos, o Santander custa o dobro (4,00 contra 2,03) — ver a tabela nos padrões transversais. Para a grelha (KAN-16), isto é o argumento para varrer o Montepio com mais folga de tempo do que os outros dois.

Das 3049 amostras, 46 foram recusadas pelo banco e 1 travada por nós antes da rede — e nas 46 confirmou-se que a recusa era mesmo dele, e não uma leitura falhada de uma resposta boa.

⚠️ A plataforma é **ITSCredit**, partilhada por vários bancos — se algum dia entrar outro banco desta plataforma, o trabalho é reaproveitável.

## Banco CTT `bancoctt`

✅ **Implementado no v2** (`internal/bancos/bancoctt/`, KAN-12), confrontado ao vivo a 2026-07-28; as capturas estão em `internal/bancos/bancoctt/capturas/`.

**Transporte:** HTTP simples, um só pedido, sem autenticação.

**Endpoint:** `POST https://simuladorch.bancoctt.pt/api/simulation/simulate`

**Payload:** `AmortizationPeriod` em **meses**, `IndexTypeSubCategoryID` (1 fixa, 2 variável, 3 mista), `IndexTypeID` (mista: 87/82/89/75 = 1/2/3/5 anos de fixo; fixa: 80/86 = 30/34 anos), `Purpose`, `PropertyType`, `HasCrossSelling=true` sempre, `YouthMeasuresIsActive`.

**Resposta:** `TAN{With,Without}Benefits`, `TAEG…`, `Spread…`/`VariableSpread…`, `MTIC…`, `MonthlyInstallmentWith(out)Bonification`, `IndexRate`/`VariableIndexRate`. Formato numérico português.

**Limites:** mista `{1,2,3,5}` anos; fixa `{30,34}` anos (todo o contrato); prazo 5-40 anos; contrato tem de terminar até aos **75 anos** — imposto por nós, porque a **API não valida** (aceitou 66 anos de idade com 40 de prazo: fim aos 106).

**Produtos:** `bancoctt:vendas_associadas` (**ligado**), `bancoctt:sustentavel` (classe energética ≥ A, -0,10 p.p. medido, desligado).

⚠️ **Na mista,** `Spread*` é o da fase fixa e vem igual à TAN (uma fase fixa não tem indexante). Reportá-lo faria o Banco CTT parecer caríssimo. O correcto é `VariableSpread*`, o contratual da fase indexada. Mesma armadilha no Santander e no Bankinter.

⚠️ **Euribor não é escolhível**: variável sempre 12M, pós-fixo da mista sempre 3M — o próprio bundle do banco força os identificadores. Os outros identificadores respondem por API, mas são preço que o banco não comercializa. **Ler o tenor real da resposta** (`IndexRateDescription`/`VariableIndexRateDescription`), não confiar no nosso mapa.

⚠️ `Purpose=1` e `Purpose=5` provocam um 404 interno embrulhado num `500 INTERNAL_ERROR`. Válidos: 2, 3, 4. Nenhum muda o preço.

**Fidelidade da leitura, medida a 2026-07-28:** 3002 ofertas confrontadas campo a campo com o corpo em bruto que as originou, por um leitor independente — 24 016 comparações, **zero divergências**. Limite superior do erro de leitura: **0,100 %** a 95 % de confiança. O harness é o `TestFidelidadeDoQueGuardamosFaceAoQueOBancoRespondeu`, atrás de `//go:build rede`, e vigia as duas armadilhas próprias deste banco: que na mista se lê `VariableSpread*` e não `Spread*`, e que o tenor sai da **descrição que o banco devolve** e não do nosso mapa de identificadores.

**Concorrência e custo, medidos na mesma corrida:** 3002 simulações com dois trabalhadores custaram **703 ms cada**, sem uma única falha, em 35m11s — **3002 pedidos HTTP** (um por simulação, o mínimo possível) e **14,6 MB**. ⚠️ São 4,9 kB por resposta, o dobro do Santander: o Banco CTT devolve o plano de amortização inteiro. Custo por simulação a meio da tabela, mas **o mais barato em pedidos**: quatro vezes menos do que o Santander para a mesma informação.

---

# Fase 2 — HTTP puro, mais complexo

## Santander `santander`

✅ **Implementado no v2** (`internal/bancos/santander/`, KAN-18), confrontado ao vivo a 2026-07-28; as capturas estão em `internal/bancos/santander/capturas/`. ⚠️ **Três linhas desta entrada estavam erradas** — ver a subsecção abaixo.

**Transporte:** HTTP simples, mas com descoberta de configuração em runtime.

**Endpoints:**

1. `GET https://simulador-credito-habitacao.santander.pt/pt-PT/gln-key-simuladorchspa/config.json` — traz `client_id` e `bff_url`.
2. `POST {bff}/credit_limit` — `{"type":"SIMULATION_LIMITS", …}`.
3. `GET {bff}/rates` — catálogo de taxas.
4. `POST {bff}/get_by_rates` — a simulação oficial (TAEG e MTIC **do banco**).

**Autenticação:** só o cabeçalho `x-ibm-client-id`, lido do `config.json`.

⚠️ **O** `bff_url` remoto é validado antes de ser usado (exige `https` e host `*.santander.pt`), senão cai num valor estático. Um `config.json` adulterado ou um ataque de intermediário mandaria os nossos pedidos para outro lado. **Manter esta validação.**

⚠️ `/credit_limit` exige `"type":"SIMULATION_LIMITS"` desde 2026-07; sem ele, `400 E309`.

⚠️ Se `/get_by_rates` falhar, o v1 caía numa amortização francesa local com TAEG e MTIC a nulo. **Preferir falhar com clareza a servir um número inventado com ar de oficial** — rever esta decisão no v2.

**Limites:** mista `{2,3,4}` anos. Euribor imposta a 6M. Não pede profissão nem rendimento. Só aceita `HOUSE_PURCHASE` — não distingue arrendamento. Prazo 1-40 anos, idade 18-80 (o `/credit_limit` publica-os).

**Produtos:** `santander:bonificado` (plano bonificado), **ligado**.

### ⚠️ Três coisas que esta entrada dizia mal, medidas a 2026-07-28

Corrigidas ao escrever o banco (KAN-18), contra o simulador a sério.

**1. O Santander TEM taxa fixa** — a 10, 20 e 30 anos (`P10`, `P20`, `P30`), e esta entrada dizia «mista {2,3,4} anos apenas». ⚠️ E a fixa é **ao contrato todo**: o `rateDurationYears` dela é o PRAZO, não um período dentro dele. Quem pede uma fixa de 28 anos recebe 30, com ajuste. A 30 anos dá TAN 4,400 %.

**2. ⚠️ O spread promocional da variável é TEMPORÁRIO, e é a armadilha deste banco.** O plano bonificado tem duas fases sobre o MESMO indexante: spread **0,5 nos primeiros 36 meses e 0,8 a partir do 37.º**. O catálogo `/rates` anuncia «SPREAD PROMOCIONAL 0,5%», e publicá-lo como o spread do contrato seria anunciar o preço de três anos de trinta. O plano não bonificado tem 1,9 constante.

A regra que daqui saiu: **publica-se o spread da última fase indexada**. A mesma regra resolve a mista, onde a última fase indexada é a que vem depois do período fixo — uma regra, e não dois casos.

**3. Na taxa fixa, o plano bonificado é IGUAL ao não bonificado.** Medido: TAEG 5,1 e MTIC 384 541,08 nos dois. O produto não desconta nada ali, e a oferta di-lo — deixar passar em silêncio fazia a pessoa acreditar que domiciliar o ordenado lhe valeu alguma coisa naquele produto.

**Confrontado ao vivo a 2026-07-28** (250 000 € / 200 000 € / 30 anos, titular de 30, com bonificação): variável 6M TAN 3,096 % e TAEG 4,0 %; mista 3a TAN 2,850 % e TAEG 3,9 %; fixa 30a TAN 4,400 % e TAEG 5,1 %.

⚠️ **A TAN de topo e o spread publicado não fecham, e é suposto**: 3,096 é 2,596 + 0,5 (o promocional, que é o que se começa a pagar) e o spread publicado é 0,800 (o que vigora a partir do mês 37).

**Fidelidade da leitura, medida a 2026-07-28:** 3002 ofertas confrontadas campo a campo com o corpo em bruto que as originou, por um leitor independente — 18 012 comparações, **zero divergências**. Limite superior do erro de leitura: **0,100 %** a 95 % de confiança. O harness é o `TestFidelidadeDoQueGuardamosFaceAoQueOBancoRespondeu`, atrás de `//go:build rede`, e vigia sobretudo a armadilha do ponto 2: em cada resposta reconstrói a lista de troços pelo lado de lá e afirma que o spread publicado é o da **última** fase indexada — reverter isso para a primeira faz o teste dizer «o spread da última fase indexada é 0.8 e a oferta traz 0.5 — o promocional é o da primeira».

⚠️ **São 6 comparações por oferta e não 12 como na CGD**, e a diferença é do banco, não do harness: o corpo do `/get_by_rates` publica o preço num plano de troços, e campos que noutros bancos vêm nomeados no topo — o indexante, a bonificação — aqui derivam-se. O que se compara é o que o banco afirma: TAEG, MTIC, TAN e prestação do primeiro troço, a regra do spread, e o número de fases a fechar no prazo devolvido.

**Concorrência e custo, medidos na mesma corrida:** 3002 simulações com dois trabalhadores custaram **460 ms cada**, sem uma única falha, em 23m00s — **12 008 pedidos HTTP** e 6,7 MB. ⚠️ São **exactamente quatro pedidos por simulação**, e é o que faz deste o banco mais caro dos cinco a sondar por unidade de informação: cada simulação repaga a descoberta da configuração, os limites e o catálogo. Para a grelha (KAN-16) isto quer dizer que o Santander é o candidato mais forte a reaproveitar configuração e catálogo dentro da mesma corrida — não está feito, e é medição para justificar a mudança, não argumento.

## Crédito Agrícola `creditoagricola`

**Transporte:** HTTP simples. O Feedzai da página não protege a API.

**Endpoints** (base `https://www.creditoagricola.pt`):

1. `POST /api/credit/credit-destinations` — resolve o identificador do produto.
2. `POST /api/credit/calculator` — a simulação.

**Payload:** `rate_id` (M/I/F), `amount`, `term` em **meses**, `evaluation`, `purpose_id` (1 própria, 2 secundária, **4 arrendamento nativo**), `date_of_birth`, `insurances=[1,6]`, `is_ca_property`, `with_bonus`, `is_dl442024`, `is_promotional_spread`, `rates[]`, `vafs: []` **sempre vazio**.

**Resposta:** `rates[]` por fase → `interest_rate` (TAN), `reference_rate_value`, `term`, `monthly_installment`; mais `taeg`, `totalCostCredit`, `installmentMonth`.

⚠️ **A armadilha mais séria de toda a colecção:** `reference_rate_value` e `rateIndex` são o *spread*, não a Euribor — apesar de `rateIndexType` dizer `EUR12TM`. A Euribor obtém-se por `interest_rate - reference_rate_value` na fase indexada. Validado ao cêntimo contra o Banco CTT (12M = 2,798; 3M = 2,339).

⚠️ **O spread promocional é exclusivo**: combinado com imóvel do banco, ou pedido em taxa fixa, dá um erro genérico «situação anómala» que não nomeia a causa.

⚠️ DL 44/2024 exige LTV ≥ 85 % e idade ≤ 35 — validar **antes** de chamar a API, para dar um erro que se entende.

**Limites:** Euribor **1/3/6/12M** (o leque mais largo). Mista `{2,3,5}` anos, com o prazo a ter de exceder. Fixa `{5,10,15}` anos, com o prazo a ter de **igualar**. Prazo 2-40 anos, limitado por escalão de idade e por fim até aos 74.

**Produtos:** `creditoagricola:bonificacao` (**ligado**), `creditoagricola:imovel_ca`, `creditoagricola:spread_promocional`.

---

# Fase 3 — Browser (risco por avaliar)

⚠️ Antes de comprometer, ver `ARQUITETURA.md` §5: em Go isto é `playwright-go` ou `chromedp`, e há uma saída conhecida (serviço à parte) se não resultar.

## ActivoBank `activobank` e Millennium BCP `millenniumbcp`

**Transporte:** browser **só para cunhar um token OAuth**; a simulação é HTTP. Partilham a mesma API do grupo BCP, distinguidos por três campos:

|  | ActivoBank | Millennium BCP |
| --- | --- | --- |
| `bank` | `ActivoBank` | `MillenniumBcp` |
| `targetSystem` | `Blue` | `PTRetail` |
| `application` | `18` | `26` |

**Endpoints** (base `https://api.millenniumbcp.pt/cj/mortgageexperience/api/rpc`):

1. `POST /SimulationConfigurations` — contexto, regras, limites de prazo.
2. `POST /SimulationScenarios` (`showAll:true`) — lista as opções.
3. `POST /SimulationScenarios` (com `scenarioContext.scenarioId`) — o detalhe.

**Autenticação:** token Bearer cunhado ao carregar o simulador num browser headless e interceptar o primeiro pedido à API. Capturam-se em runtime `Authorization`, `ocp-apim-subscription-key`, `accept` e `mbcpobctx`. O v1 tinha cache do token por processo (600 s) com bloqueio, e repetia o mint em 401/403.

⚠️ **Campos a nulo têm de ser removidos do payload** — a API rejeita `null` com «Serialization error» desde 2026-07-17.

⚠️ **O Millennium exige uma sequência de cliques** antes de o token ser cunhado («não sou», «comprar casa», «casa onde vou viver», vários «Continuar»). Essa sequência já mudou duas vezes; é o ponto mais frágil dos dois.

⚠️ O mint demora \~25 s. Sem cache, cada simulação paga-o.

⚠️ A Garantia Pública Jovens só devolve opções com LTV \~85-100 %.

**Produtos:** `bcp:life_insurance`, **ligado**.

## Bankinter `bankinter`

**Transporte:** browser a chamar a API **de dentro da página** (`fetch` via `evaluate`), porque o domínio está atrás de **Cloudflare** e um cliente HTTP puro leva `403`.

**Endpoints:**

1. `GET https://banco.bankinter.pt/such/proposta-credito-habitacao/home` — limpa o desafio do Cloudflare e fixa cookies.
2. `POST https://banco.bankinter.pt/such/api/v1/simulation/executeDetailedSimulation` — com `credentials:'include'`, de dentro da página.

⚠️ **O que o Cloudflare bloqueia é a impressão digital de automação, não o binário.** Chromium normal com UA real, `--disable-blink-features=AutomationControlled` e `navigator.webdriver` escondido passa (3/3 medido). O `channel="chrome"` foi abandonado por não haver build para linux/arm64. **Esta é a peça mais arriscada de portar para Go.**

⚠️ **Números em formato anglo-saxónico** (ponto decimal) — ao contrário de todos os outros bancos. Fácil de trocar sem reparar; merece teste dedicado.

⚠️ **O MTIC não acompanha a selecção de produtos** — fica preso no valor de «todos os produtos». O v1 só reportava MTIC quando a selecção era exactamente a de omissão; caso contrário omitia com nota. Manter essa honestidade.

⚠️ O prazo máximo tem de ser **pré-calculado** por nós (contrato a terminar até aos 83 anos), porque a API dá `400` mudo, sem dizer qual é o máximo.

**Produtos:** `bankinter:seguro_vida` (-0,20), `bankinter:seguro_multirriscos` (-0,05), `bankinter:domiciliacao_ordenado` (-0,10) — **todos ligados**.

## Banco BPI `bpi`

**Transporte:** browser a conduzir um formulário OutSystems e a **ler texto da página**. Não há API JSON conhecida.

**Endpoint:** só a página `https://www.bancobpi.pt/particulares/credito/credito-habitacao/simulador-credito-habitacao`. Tudo o resto são postbacks internos.

⚠️ **É o banco mais problemático dos dez, e por larga margem:**

- **52 s de um pedido de 52 s.** Os outros nove ficam prontos aos 26 s. Havia 5 s de espera fixa após o banner de cookies, e um poll que dormia 2 s **antes** de cada verificação.
- **Extração instável** (\~1 em 3 corridas falhava): clicar no `input` de rádio não pegava; a correcção foi clicar no `label` associado.
- **Não pede o valor do imóvel** → o LTV não é comparável com os outros bancos.
- Parsing por expressão regular sobre `innerText` inteiro, recortado por `str.find`. É o código mais frágil de todo o v1.

**Limites:** períodos fixos `{3,5,10}`; prazo 11-36 anos; distrito fixo em Lisboa; prazo máximo por idade descoberto **em runtime**, a partir do banner de erro.

**Produtos:** `bpi:vendas_associadas`, **desligado**.

⚠️ **Antes de escrever este banco, gastar meio dia a procurar a API por baixo.** O OutSystems expõe *screen services*; o Banco CTT e o Crédito Agrícola acabaram ambos em HTTP puro abaixo de 2,5 s por essa via. Se existir, os 52 s colapsam para \~2 s **e** a instabilidade desaparece com eles. Se não existir, usar selectores estruturados e esperas por elemento — nunca esperas por tempo nem recorte de texto.

---

# Padrões transversais

**Bom, a manter:**

- Erros do banco usados como sinal estruturado (o `V159` do Novo Banco traz o prazo máximo lá dentro; o `V157` traz o prazo que a fixa exige; o banner do BPI também). Muito melhor do que valores fixos no nosso código. ⚠️ Não é universal: o Montepio dá a mesma frase a três causas diferentes e uma mensagem vazia a uma quarta, e por isso lá impõe-se tudo antes de ir à rede.
- `Produto` como abstracção de bonificação — consistente e reutilizável.
- «Ajusta e anota» em vez de falhar.
- Ler da resposta o que o banco aplicou, em vez de confiar no que pedimos.

**A não repetir:**

- Duas cópias da mesma função de idade e duas de amortização francesa, em ficheiros diferentes, por assinaturas ligeiramente distintas.
- Cada banco a reimplementar o seu «abrir browser / fixar cookies / cunhar token», sem nunca convergirem.
- Parsing por expressão regular sobre texto de página (BPI).
- Limites de idade medidos empiricamente e depois escritos como constantes (83/76/75/74 anos) sem nada que avise quando o banco os mudar.

**Formatos numéricos:** nove bancos devolvem formato português (vírgula decimal, por vezes com espaço não-quebrável); o **Bankinter** devolve formato anglo-saxónico. Isto está certo — os bancos devolvem mesmo formatos diferentes — mas é fácil de trocar sem reparar. Cada banco leva um teste que fixa o seu formato. ⚠️ O **Novo Banco** não devolve texto de todo: os números vêm como números de JSON, e a armadilha aí é outra — lê-los para `float64` em vez de `json.Number` estraga o cêntimo (medido: 356005,55 → 356005,59).

**Uma API aceitar não é o banco vender.** O Banco CTT respondia a identificadores de indexante que não comercializa; a CGD calcula um LTV de 96 % que não pratica; o Novo Banco preça um LTV de 100 %. O que o simulador **mostra** é o que o banco vende — é daí que se lêem as listas, não do que a API tolera.

⚠️ **E o sítio onde isso se lê é o bundle do SPA, não a documentação nem a tolerância da API.** Foi assim que se confirmou, no Novo Banco, que as nove opções de período e os três tenores de Euribor que o v2 declara são exactamente os que o simulador oferece — e foi na mesma passagem que se descobriu um `GET /configuracoes` com os limites todos, depois de eu ter escrito no dossiê que esse endpoint não existia. **Ler o bundle faz parte de escrever um banco**, não é trabalho extra: custou minutos e apanhou uma afirmação falsa que já estava escrita. ⚠️ E há uma variante barata disto: no Montepio o que estava por descobrir não era um endpoint, era **o HTML da própria página de arranque** — o `model.Configs` traz a tabela de prazo por idade que o v1 tinha medido à mão, por busca binária.

**Varrer o LTV ponto a ponto quando se escreve um banco.** Foi assim que se descobriu que os degraus de spread da CGD não são de 5 % (KAN-35). Custa poucos minutos e é a diferença entre saber a forma da função de preço e assumi-la.

⚠️ **E com três bancos varridos sabe-se que a forma não é uma só — são três.** Na CGD o preço **desce** quando o LTV sobe e quebra aos **67 %**, que a banda de 5 % não representa; no Novo Banco **sobe**, e quebra em 50/51, 70/71 e 80/81 — exactamente nas fronteiras do `dominio.BandaLTV`; no Montepio **não muda de todo**, de 50 % a 100 %. Uma grelha de 5 % serve um na perfeição, serve a outro o preço de outro cliente, e ao terceiro dá vinte linhas onde bastava uma. É o material que o **KAN-35** precisava para decidir por medição em vez de por argumento.

**Confrontar a leitura com um leitor independente, e não só com um teste de captura.** O harness de fidelidade lê cada resposta duas vezes, por dois caminhos escritos de propósito para não se parecerem, e compara campo a campo. Na CGD, em quatro corridas apanhou dois defeitos do próprio harness e um do produto — a razão falsa dada a quem via o prazo encolher. No Novo Banco apanhou a **cadeia de ajustes ao prazo**, que é o mesmo defeito outra vez: dois sítios a encolher o prazo, e o segundo ajuste a dizer «Pediu 35 anos» a quem pediu 40. Um teste de captura não apanhava nenhum dos quatro: as capturas são as respostas que já sabíamos ler.

⚠️ **Os cinco bancos implementados têm agora a mesma medição, e é a mesma conclusão:** 3002 ofertas conferidas em cada um, **zero divergências** nas 147 098 comparações somadas. Limite superior do erro de leitura: **0,100 %** a 95 % de confiança, em todos. ⚠️ Não se lê isto como «acertamos 99,9 %»: o observado é zero divergências, e os 0,100 % são o que uma amostra de 3002 permite excluir. Para descer a 0,01 % seriam 30 000 ofertas por banco — no Montepio, catorze horas.

| banco | comparações | duração | pedidos por simulação | tempo por simulação |
|---|---|---|---|---|
| CGD | 36 024 | não registada | não registados | não registado |
| Novo Banco | 33 022 | não registada | 1,24 † | 230 ms † |
| Montepio | 36 024 | 1h27m | 2,03 | 1,721 s |
| Banco CTT | 24 016 | 35m11s | **1,00** | 703 ms |
| Santander | 18 012 | 23m00s | **4,00** | 460 ms |

† O Novo Banco tem as comparações da corrida de 3002 e o custo de uma corrida anterior, de 3392 simulações (4218 pedidos, 7,3 MB). Não são a mesma corrida, e a coluna diz «não registado» onde não há medição em vez de repetir um número parecido — na CGD o custo nunca foi cronometrado.

⚠️ **O número de comparações varia por banco, e não é medida de rigor.** Compara-se o que o banco **afirma**: o Santander publica o preço num plano de troços e nomeia menos campos no topo, logo dá 6 comparações por oferta contra as 12 da CGD. Inflacionar a coluna obrigaria a comparar campos que nós derivamos, o que mede o derivador e não a leitura. A afirmação que vale é a mesma nos cinco: **zero divergências**.

⚠️ **E a última coluna é a que decide o desenho do varrimento**, não a duração: o Banco CTT gasta **um** pedido por simulação e o Santander **quatro** — cada simulação repaga-lhe a configuração, os limites e o catálogo. Sondar o Santander custa quatro vezes mais ao banco pela mesma informação. É o argumento medido para reaproveitar config e catálogo dentro de uma corrida (KAN-16), e a medição veio antes da mudança, não a justificá-la depois.

### Latência do caminho ao vivo, medida a 2026-08-06 às 17h13 (hora de expediente)

⚠️ **Este é o primeiro dos três números de que a Fase 6 depende**, e mede outra coisa que a tabela acima: ali é o tempo por simulação **dentro de uma corrida**, com a configuração e o catálogo já pagos; aqui é o que uma pessoa espera por um pedido **frio**, transportes construídos de raiz, pelo mesmo caminho que o `POST /api/v1/ofertas/{banco}` percorre. 25 simulações, 5 por banco, sequenciais e com 3 s de pausa. Zero falhas.

| banco | min | mediana | max | cauda (max ÷ mediana) |
|---|---|---|---|---|
| Novo Banco | 300 ms | 460 ms | 1,15 s | 2,5× |
| Santander | 500 ms | 920 ms | 1,52 s | 1,7× |
| CGD | 940 ms | 1,04 s | 1,87 s | 1,8× |
| Banco CTT | 1,15 s | 1,22 s | 2,19 s | 1,8× |
| **Montepio** | 1,89 s | 2,01 s | **8,4 s** | **4,2×** |

⚠️ **O frio custa cerca do dobro do quente.** Confrontando a mediana com a coluna «tempo por simulação» da corrida de fidelidade: Banco CTT 1,22 s contra 703 ms, Santander 920 ms contra 460 ms, Montepio 2,01 s contra 1,721 s. Um pedido de cliente paga o que uma corrida amortiza — e é por isso que reaproveitar configuração e catálogo **dentro de um pedido** deixou de ser optimização.

⚠️ **A cauda do Montepio é o número que decide o timeout, e não a mediana dele.** 4,2× a própria mediana, contra 1,7-2,5× em todos os outros. Não é ruído: é o único banco que paga um arranque `GET`→`POST` para fixar cookies em cada simulação, e é o mesmo banco que noutra medição do mesmo dia não respondeu dentro de **10 s** em 4 cenários. Um timeout dimensionado pela mediana cortava-o a meio de uma resposta que ia chegar.

⚠️ **Cinco amostras dão um máximo observado, não um percentil.** O 8,4 s é um **limite inferior** do pior caso do Montepio — a prova é que outra medição do mesmo dia viu passar dos 10 s. Um p95 honesto pede dezenas de amostras por banco, e dezenas × 5 bancos × até 4 pedidos por simulação é carga a sério contra terceiros. O que estas 25 amostras servem para fazer é desmontar um timeout escolhido no ar; não servem para prometer um percentil.

Reproduz-se com `make latencia` (`LATENCIA_AMOSTRAS` regula as amostras). ⚠️ **Corre-se em hora de expediente de propósito** — é a janela em que o número é mau, e um timeout dimensiona-se pelo mau.

⚠️ **A aritmética é um leitor independente, e é grátis.** No Montepio foi ela que decidiu qual de dois números da mesma resposta era o verdadeiro: a fase indexada traz TAN 4,350 e prestação 995,62, mas declara uma taxa de 3,839 — e só a primeira reproduz o `TotalInterest` que o banco publica para essa fase. Sem essa conta, a escolha entre os dois seria uma preferência.
