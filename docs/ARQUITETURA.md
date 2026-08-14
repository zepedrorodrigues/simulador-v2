# Arquitectura — simulador-v2

Este documento é a decisão de desenho. É vinculativo: quando o código diverge dele, um dos dois está errado e resolve-se **antes** de continuar.

**Stack:** Go · PostgreSQL · `chi` · `pgx` + `sqlc` · OpenAPI. A interface com pessoas é uma app **React Native**, em repositório separado.

## 1. Âmbito

⚠️ **Reescrita a 2026-08-06.** Reverte a inversão de 2026-07-25 e volta ao que o
v1 fazia: **o pedido do cliente vai ao banco**. O porquê, com os números que o
obrigaram, está em `DECISAO-AO-VIVO.md` — e a razão curta é que guardar uma cópia
do modelo de preço de cada banco obriga a acertar em como eles preçam, e num só
dia de confronto com dados reais falhámos isso quatro vezes seguidas.

O `simulador-v2` faz **uma** coisa e mais nenhuma:

**Perguntar aos simuladores públicos dos bancos o preço do crédito que o cliente
descreveu, e devolver o que eles responderam.** Um pedido do cliente é um pedido
a cada banco escolhido, com os valores que ele introduziu.

⚠️ **Não há modelo de preço nosso.** Não se reconstrói a função de preço de banco
nenhum, não se interpola, não se deriva TAEG, não se compõem descontos. O que se
serve é o que o banco disse, traduzido para o nosso vocabulário e mais nada. É
esta frase que substitui a §4 antiga inteira.

⚠️ **A série temporal de mercado deixou de existir** (D1), e o `/api/rate-catalog`
**saiu com ela** a 2026-08-07. Dizia-se aqui que a retirada era trabalho
coordenado com o `viabilidade-imobiliaria`, «que o consome em produção» — e as
duas metades eram falsas. Ele consome o do **v1**, e não há produção em lado
nenhum. Ver a §6.

O que **sobrevive** desta reversão, e é o activo do repositório: os **parsers**,
as **capturas** e o `DOSSIE-BANCOS.md`. Nada disso é modelo nosso — é
conhecimento sobre o interlocutor, e é o que a fatia ao vivo precisa por inteiro.

Este repositório serve **JSON e mais nada** — sem ficheiros estáticos. A restrição é deliberada: no v1 não havia fronteira entre UI e servidor, e a lógica de apresentação acabou espalhada pelo servidor.

⚠️ **Consequência prática, executada a 2026-07-28 (KAN-22): a app web é um SERVIÇO À PARTE.** O `expo export` produz estáticos, e servi-los deste binário seria a primeira excepção a esta regra — que é como as regras deste género morrem. São dois serviços no mesmo alojamento, e o repositório Go continua a servir só JSON.

## 2. Camadas

```
cmd/
  simulador/        o binário: servidor HTTP e subcomandos
internal/
  dominio/          tipos e regras. Zero I/O, zero dependências do projecto.
  bancos/           um pacote por banco + as estratégias de transporte.
  aplicacao/        casos de uso: comparar, limitar. Sem HTTP, sem SQL.
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

`internal/aplicacao/` — `comparar` (perguntar a um banco o preço do crédito pedido, com tecto e resíduo) e `limites` (tecto por IP). Recebe os bancos por injecção; nunca importa o registo directamente — é o que torna um caso de uso testável com bancos falsos.

⚠️ **Reescrito a 2026-08-06.** Dizia aqui que «o `comparar` não fala com bancos e o `varrimento` não responde a clientes». O `varrimento` deixou de existir e o `comparar` **fala com bancos** — é o que a §1 passou a mandar.

⚠️ **O que se perdeu com isso, e é preciso ter presente:** o `comparar` era testável sem rede nem relógio, e deixa de o ser de graça. A propriedade recupera-se pela injecção — os bancos entram como interface e o caso de uso testa-se com bancos falsos —, mas passa a ser **disciplina** em vez de ser estrutura. É a fronteira que mais barato se perde nesta reversão.

⚠️ E quem tem fan-out concorrente passa a ser o caminho do cliente. É lá que o `-race` do portão (§8) ganha agora o seu valor — no código que serve pessoas, e não num subcomando nocturno.

`internal/infra/` — `http` (routers `chi`, handlers, tradução de erros de domínio para códigos HTTP), `bd` (código gerado pelo `sqlc` + migrações), `travao` (o *advisory lock* por banco), `config`, `relogio`.

⚠️ **Os tipos que saem em JSON são distintos dos tipos de `dominio`, e são gerados.** Vivem em `api/` (`package api`), gerados do `openapi.yaml` pelo `oapi-codegen`; os handlers em `internal/infra/http` implementam a interface gerada e traduzem `dominio` ↔ contrato. Parece duplicação face ao domínio e não é: são a fronteira publicada e versionada, que não pode mudar só porque o domínio mudou. No v1 o modelo de domínio *era* o contrato HTTP, e por isso qualquer refactor arriscava partir o `viabilidade-imobiliaria`.

⚠️ **`api/` está fora de `internal/` de propósito, e é uma folha.** É a superfície pública do módulo — o contrato — e não importa nenhum pacote interno: o `depguard` reprova quem lá meta um `import` de `dominio`, `bancos`, `aplicacao` ou `infra`. O contrato descreve-se a si mesmo; não conhece a máquina que o serve. É `internal/infra/http` que o consome, nunca o contrário. O standard internacional (`/api` para os ficheiros de contrato) fixa o **spec**; co-locar aqui o Go e o TypeScript gerados é decisão nossa, para o contrato viver todo num sítio.

## 4. Modelo de dados

⚠️ **Reescrita a 2026-08-06.** A secção antiga tinha ~320 linhas a descrever a
`catalogo_taxas`, a escala de LTV, a base da fixa, o resíduo em coluna, os
encargos e a `sondagens`. **Nada disso existe.** Não é perda: era tudo estrutura
para guardar uma reconstrução do preço dos bancos, e o preço passou a vir dos
bancos.

**A regra que sobrou é a que já cá estava, e agora é quase tudo:**

> **Guarda-se o parâmetro, não o resultado.** Simulações de utilizador **não** se
> guardam.

⚠️ Ao vivo isto ganha um segundo sentido, de privacidade e não de desenho: um
pedido traz o montante, o valor do imóvel, a data de nascimento e o rendimento de
alguém. **Nada disso toca no disco.** Atravessa o processo, vai ao banco no
formato que ele exige, e a resposta sai para o cliente.

### As tabelas

| tabela | para quê |
|---|---|
| `limites` | tecto de pedidos por IP (§7.5). ⚠️ Passou de higiene a estrutural |
| `respostas_em_cache` | a cache do §7.6, com chave = pedido exacto e validade curta |
| `catalogos_de_banco` | o que um banco **publica** e nós temos de saber para lhe montar o pedido |

**E mais nenhuma.** ⚠️ Uma tabela que não caiba nesta lista não entra sem esta
secção mudar primeiro — foi por acrescento não decidido que o v1 ganhou
autenticação e a teve de apagar em três migrações.

### `catalogos_de_banco` — porque é que entrou (2026-08-14, KAN-36)

**É a regra desta secção aplicada à letra: guarda-se o parâmetro, não o
resultado.** Um catálogo é um parâmetro — a lista de opções que o banco publica e
que é preciso conhecer **antes** de lhe montar o pedido. Não é a resposta a
pedido nenhum, e não é de ninguém.

⚠️ **O que a obrigou a existir, medido a 2026-08-14:** a CGD publica os períodos
de taxa fixa dentro do HTML da página inicial, e o `internal/bancos/cgd` lia-os
**dentro do `Simular`**. Por pedido de cliente com fase fixa ou mista: **3
pedidos à CGD e 82 333 bytes, dos quais 75 291 (91,4 %) são a página** — para
extrair catorze pares `ano → código`. Em taxa variável a página não é pedida, e o
custo são 2 pedidos e 7 042 bytes.

⚠️ **Foi a inversão da §1 que mudou o tamanho disto.** Enquanto havia varrimento,
a página lia-se uma vez por corrida e diluía-se por dezenas de pontos; a `KAN-36`
está escrita nesse mundo e chama-lhe «um parâmetro da corrida». **Corrida já não
há.** Ao vivo o custo é por pessoa que pergunta, e o `CLAUDE.md` diz que
reaproveitar catálogo dentro de um pedido «deixou de ser optimização e passou a
ser defesa».

**Porque não em nenhum dos sítios mais baratos:**

- **em memória do processo** — a §7.5 proíbe, e com razão: com mais de um worker
  cada um teria o seu, e o que se está a limitar é carga contra um terceiro;
- **na `respostas_em_cache`** — o contrato dela está escrito e é outro: chave =
  pedido exacto, valor = a resposta do banco. Um catálogo não é nem uma coisa nem
  outra, e enfiá-lo lá era o acrescento não decidido que esta secção existe para
  travar;
- **no construtor do banco** — o `CONTRATO-BANCO.md` §2 evita I/O em
  construtores, e não resolvia nada: o processo reconstrói bancos.

⚠️ **Não tem dados pessoais, e é o que a separa da outra tabela.** O que lá vive é
o que o banco publica a quem visitar o site — catorze inteiros, no caso da CGD.
Nenhuma das guardas de privacidade do §7.6 se aplica, e por isso **a chave é
legível**: `(banco_id, nome)`, sem resumo criptográfico. Um resumo aqui não
protegia nada e tirava a capacidade de olhar para a tabela e perceber o que está
lá.

⚠️ **A validade é de partida e não está medida.** Ficam **24 h**, pela mesma
honestidade com que o §7.6 declara os 5 min: ninguém mediu de quanto em quanto
tempo a CGD mexe nos períodos que pratica. O que a mede é uma vigia, e é trabalho
próprio. **O que não se faz é servir um catálogo velho em silêncio** — quando não
há entrada válida, vai-se à página; quando a ida falha, cai-se na lista conhecida
**e a oferta di-lo**, que é o comportamento que já existia.

### ⚠️ O que a cache guarda, e o que nunca guarda

A chave é o **pedido exacto**, e não uma versão arredondada dele (§7.6). O valor
é a **resposta do banco**, que é preço e não pessoa.

⚠️ **O pedido tem dados pessoais e a chave não os pode ter em claro.** Montante e
prazo são parâmetros; data de nascimento e rendimento são de alguém. A chave é um
**resumo criptográfico** do pedido normalizado, e o pedido em claro não se grava.
Assim dois clientes iguais partilham o acerto sem que a linha diga quem são.

**A validade é curta e mede-se, não se escolhe.** É a distância entre «cortar
carga» e «servir um preço que já mudou», e os bancos fixam preço em ritmos
diferentes — a Euribor é diária, os spreads não. ⚠️ Fica **curta** até estar
medido, pela mesma assimetria de sempre: uma cache curta de mais custa pedidos, e
uma longa de mais custa um preço errado.

**Parte de omissão: 5 minutos** (`CACHE_VALIDADE`), decidido a 2026-08-07. Cobre
o padrão que existe hoje — a mesma pessoa a comparar, a mexer num campo e a
voltar — e é curto ao ponto de uma Euribor que fixa de manhã nunca ficar cinco
minutos errada em cima de quem pergunta. ⚠️ **Não é a validade medida**; é o
valor com que se serve enquanto a vigia do §7.6 não existir.

#### ⚠️ Guarda-se o **servido**, e não o objecto de domínio

O valor da linha é a resposta **já traduzida para o contrato** — o mesmo JSON que
sairia de uma resposta fresca. Decidido a 2026-08-07, e a razão é de
verificabilidade: assim um acerto é igual a uma resposta fresca **por
construção**, e não por cuidado de quem escreveu o código.

⚠️ A alternativa — guardar o `dominio.Oferta` — obrigava a pôr um formato de
persistência dentro do domínio puro, porque os `ajustes` e as `notas` não são
exportados e o `Ajuste` fecha os seus campos **de propósito**, para que um ajuste
sem nota não seja construível a partir de fora do pacote. Ressuscitar um por JSON abria essa porta, e criava uma segunda descrição
da forma da `Oferta` a divergir da primeira em silêncio — que é exactamente o que
aconteceu à interface `Banco` copiada para o `CONTRATO-BANCO.md`.

**O preço desta escolha, escrito:** a cache vive na fronteira HTTP e não no caso
de uso, portanto um `comparar` que voltasse a fazer fan-out **no servidor** não a
herdava. Hoje quem faz fan-out é a app (§7.4), logo o único chamador é o
handler — e no dia em que deixar de ser, isto muda de sítio.

#### ⚠️ Uma falha nunca se guarda

Só respostas com `sucesso` entram na cache. Guardar um `banco_indisponivel`
transformava um soluço de segundos numa indisponibilidade de **toda a validade**,
e multiplicava por todos os clientes com o mesmo pedido. A cache existe para
cortar carga sobre os bancos, não para memorizar que um deles tropeçou.

⚠️ E é assimétrico de propósito: uma falha não guardada custa um pedido a mais ao
banco; uma falha guardada custa uma oferta que não se serve a quem a podia ter.

### `catalogo_taxas` e `sondagens` — ⛔ apagadas a 2026-08-07

Caíram na migração `00008_o_varrimento_morre`, com o `/api/rate-catalog` que a
primeira alimentava. **A migração fica escrita como o fim de um desenho, e não
como limpeza** — é o que esta secção prometia.

⚠️ **A D1 desbloqueou-se por os factos não serem os que aqui estavam escritos.**
Dizia-se que a `catalogo_taxas` tinha de ficar porque o `/api/rate-catalog`
serviria a última fotografia ao `viabilidade-imobiliaria` «em produção». Ambas as
metades eram falsas, e verificaram-se a 2026-08-07: aquele repositório consome o
`/api/rate-catalog` do **v1** (o `chmonitor`), não o daqui; e **não há produção**
— nem o v2 nem o v1 estão alojados. A rota daqui nunca teve consumidor.

⚠️ **O Down da 00008 recria a forma, não os dados.** Uma migração de remoção não
guarda o que apaga; quem precisar da série repõe-na de um `pg_dump`. Há teste a
afirmar que o Down corre sem partir, e a afirmar que depois de migrar tudo as
duas tabelas **não existem**.

⚠️ **Uma tabela com nome de funcionalidade morta é o indicador que o `PLAN.md`
conta**, e o v1 tinha 1 em 3. Este repositório volta a **0**, e agora há um teste
a vigiá-lo.

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

⚠️ **Um banco avariado nunca derruba a resposta.** No v1 isso obrigava a um `except Exception` dentro de cada scraper. Em Go a política é a mesma mas fica **num sítio só**: quem chama um banco corre-o com `recover` e converte pânico ou erro numa oferta de falha nomeada. Dentro de um banco, os erros devolvem-se; não se engolem.

⚠️ **E o pânico recuperado sai com código próprio — `erro_interno`** (KAN-30, 2026-08-11), e não como `banco_indisponivel`. Recuperar o pânico é isolamento; atribuí-lo ao banco é contabilidade errada, e a segunda não vem de graça com a primeira: enquanto vinha, um defeito nosso dava ao banco fama de instável e não aparecia em métrica nenhuma nossa.

⚠️ **Reescrito a 2026-08-06, e o sentido inverteu-se.** Dizia aqui que um banco em falha «já não estraga a resposta — atrasa-a», porque se servia o varrimento anterior. Com a reversão da §1 **não há varrimento anterior**: um banco que não responde sai como `banco_indisponivel`, nomeado (D3). O que era um número mais velho declarado volta a ser uma falha visível — e é a troca que esta reversão faz de propósito: prefere-se um buraco honesto a um número de outro momento no meio de quatro frescos.

## 6. Fronteiras HTTP

**Uma.** Contrato completo em `API.md`, esquema executável em `api/openapi.yaml`.

`/api/v1/*` — a app React Native. Nossa, versionada, evolui connosco. Os tipos Go do servidor e os tipos TypeScript da app são **gerados** a partir do `openapi.yaml` (`oapi-codegen` e `openapi-typescript`). O contrato é código dos dois lados, não documentação.

⚠️ **Eram duas.** A `/api/rate-catalog` — inglês, snake\_case, autenticada por `X-API-Key`, compatível ao byte com o v1 — **saiu a 2026-08-07** (Fase 6, passo 5). A série que ela publicava vinha do varrimento, e o varrimento morreu. Com ela saiu a única superfície autenticada deste serviço: hoje **não há credencial nenhuma**, e é decisão — o `/api/v1` é público porque é uma app sem contas a falar com ele, e quem o protege é o tecto por IP (§7.5).

⚠️ **A D1 desbloqueou-se por verificação, não por decisão, e é a lição de §6.** Este documento e mais quatro afirmavam que o `viabilidade-imobiliaria` consumia aquela rota **em produção**, e por isso a retirada estava a ser tratada como trabalho a combinar com outro repositório. As duas metades eram falsas: ele consome o `/api/rate-catalog` do **v1** (o `chmonitor`, que mantém os seus próprios scrapers), e **não há produção** — nem o v2 nem o v1 estão alojados. A rota daqui nunca teve consumidor. **Quando um documento afirma um facto sobre o mundo, verifica-se o facto** — não se desempata com a regra de que o código manda.

⚠️ **Um dia o v2 responde às perguntas do `viabilidade`, e não será por aquela rota:** ela publica uma série temporal, e sem varrimento não há como a produzir. O caminho é o ao vivo — perguntar um cenário de referência quando alguém precisar dele —, e é decisão nova.

⚠️ **O ciclo das simulações muda outra vez com a reversão da §1 (2026-08-06), e é a terceira forma.** Foi `POST` → `202` + sondagem (v1), passou a `POST /api/v1/comparacoes` → `200` com tudo dentro (`KAN-32`), e passa agora a **um pedido por banco**: `POST /api/v1/ofertas/{banco}`, que a app dispara por banco escolhido e mostra à medida que chegam (D2).

⚠️ **Não volta o `202` com sondagem, e a distinção importa:** não há trabalho assíncrono do nosso lado a que se voltasse a perguntar «já está?». Cada pedido é síncrono, fala com um banco e devolve o que ele disse ou uma falha nomeada. O que era progresso de um trabalho nosso passa a ser, do lado da app, a lista a encher-se.

⚠️ **O `POST /api/v1/comparacoes` SAIU** (2026-08-07). Dizia-se aqui que ficava «e a razão é a app nas lojas» — `/api/v1` só pode mudar por acrescento, e uma versão antiga no terreno continuaria a chamá-lo. **A razão não caiu; caiu a premissa:** não há versão nenhuma no terreno, e esta era a única janela em que partir o `/api/v1` custava zero. Ela fecha no dia em que houver uma app publicada, e a regra do acrescento passa a valer sem excepção.

⚠️ **A rota chamava-se `/api/v1/comparacoes` e nunca se chamou outra coisa.** O `API.md` prometeu `/api/v1/simulacoes` até 2026-07-28 — este parágrafo repetia-lhe o nome ao descrever o ciclo antigo, e assim **dois documentos concordavam um com o outro e ambos com o código nenhum**. O `make gerado` não apanha isto: compara o gerado com o spec, não os documentos com o spec. **Quando um documento e o `api/openapi.yaml` discordarem, o documento é que está errado** — excepto quando o documento afirma um facto sobre o mundo, e aí verifica-se o facto (ver a D1, acima).

⚠️ **Uma rota declarada no spec e sem handler atravessa o portão inteiro em silêncio**, e o `/api/rate-catalog/snapshots` esteve assim desde que o contrato existe (corrigido a 2026-07-28, KAN-44). A razão é estrutural: o `make gerado` compara o **gerado** com o spec, não o **servido**. Há um teste que percorre os caminhos do `openapi.yaml` e falha a nomear o que ficar sem handler — fecha a classe, não o caso.

⚠️ **E o defeito inverso passou aqui logo a seguir, sem que nada reparasse.** O passo 5 apagou as duas rotas e deixou **oito schemas** no contrato sem caminho nenhum a alcançá-los — `Comparacao`, `ComparacaoPedido`, `SerieIndisponivel`, `RateCatalog`, `Scenario`, `Point`, `SnapshotsResposta`, `Snapshot`. Cada um gerava um tipo Go exportado e uma entrada no `.d.ts` que a app consome: o contrato continuava a oferecer-lhe a forma de respostas que o servidor já não sabia dar, e a app estava mesmo a compilar contra a `Comparacao` enquanto chamava uma rota que dava 404. Há agora `TestTodoOSchemaDoSpecEAlcancavel`, e a regra que ele impõe é simples: **quando uma rota sai, sai o que só ela usava.**

### ⚠️ CORS: quem pode chamar de um browser, e o que fica de fora

**Decidido a 2026-07-28 (KAN-22), ao pôr a app web de pé.** Até aqui não havia CORS **nenhum** — nem no código nem no contrato —, o que na prática queria dizer que a app web não conseguia falar com esta API a partir de outra origem. Não era uma escolha: era um buraco.

**A regra:** lista **explícita** de origens em `ORIGENS_PERMITIDAS`, com **default vazio**. Vazio não é «tudo»: é «sem cabeçalhos de CORS», ou seja, só a mesma origem. Nunca `*`.

**E `AllowCredentials` é falso, sempre.** Não há cookies, não há sessões e não vai haver (§4): a app não tem nada para autenticar. Com credenciais a falso, `*` seria tecnicamente aceitável para o browser — e continua a não se usar, porque uma lista de origens é também a lista de **quem sabemos que existe**, e essa informação vale por si no dia em que aparecer tráfego de uma origem que ninguém reconhece.

⚠️ **O CORS aplica-se a `/api/v1/*` e ao `/healthz`, e hoje isso é tudo o que há.** Aplicava-se-lhes **por oposição** ao `/api/rate-catalog`, que ficava de fora: o catálogo autenticava-se por `X-API-Key`, e **uma chave dentro de um bundle de browser é uma chave pública** — qualquer pessoa a lê nas ferramentas de programador.

⚠️ **A rota saiu (2026-08-07) e o GRUPO de rotas que a separava fica.** Não se dissolve no router, e a razão é que ele é o que mantém a política aplicada por **decisão** e não por omissão — dissolvê-lo poupava uma indentação e transformava a política numa coincidência. É lá que entra a próxima superfície que não seja para browsers, e quem a puser repõe com ela a distinção inteira: `*` na app abriria o que estiver de fora; uma chave dentro da app publica-a.

**O que o browser não recebe, e é de propósito:** a app **não** manda `X-API-Key` nenhuma. Os endpoints `/api/v1` são públicos e o que os protege é o tecto por IP (§7), não uma credencial. Uma credencial que viaja para o cliente não é uma credencial.

⚠️ **E ligar o CORS partia o tecto por IP ao meio, o que só apareceu por se ir medir.** Um browser manda um `OPTIONS` de sondagem antes de cada `POST /api/v1/ofertas/{banco}` — o `Content-Type: application/json` obriga-o —, portanto cada pedido feito da app custava **dois** do tecto e o mesmo feito por `curl` custava **um**: o tecto passava a medir o cliente em vez do uso. Os preflights não contam. E não abre buraco, porque só é reconhecido como preflight o `OPTIONS` que traga `Access-Control-Request-Method` — senão bastava escolher o método para escapar ao tecto.

⚠️ **Com o fan-out na app isto multiplicou-se por cinco e a conclusão não mudou.** Uma comparação são cinco `POST` e cinco preflights: a contá-los, custava **dez** do tecto de 60/min em vez de cinco, e o número de comparações por minuto caía para metade sem que nada o dissesse.

## 7. A fatia ao vivo

⚠️ **Reescrita a 2026-08-06.** Substitui «O varrimento, e o que substitui a
cache». O varrimento morreu (D1) e a cache voltou.

1. **Um pedido do cliente é um pedido a cada banco escolhido.** É o caminho
   normal, e não a excepção. Não há fotografia, não há grelha, não há preço
   guardado a que se recorra.

2. **⚠️ O travão por banco muda de natureza: passa de fecho a TECTO.** O
   `pg_try_advisory_lock` existia para impedir **dois varrimentos** do mesmo
   banco em simultâneo, e ao vivo isso está errado — dois clientes diferentes a
   perguntar pelo mesmo banco ao mesmo tempo é o funcionamento normal, e um
   mutex punha-os em fila. O que é preciso é um **limite de concorrência por
   banco**: N pedidos em voo, o N+1 espera ou desiste.

   ⚠️ **N não se escolhe à cabeça — mede-se, banco a banco.** É a mesma regra do
   `PROXIES_DE_CONFIANCA`: fica **apertado** até estar medido, porque um tecto
   apertado de mais é visível (clientes esperam) e um largo de mais não é (o
   banco bloqueia-nos, e só se sabe depois).

   ✅ **Feito a 2026-08-06** (`internal/infra/lotacao`, `VAGAS_POR_BANCO`): **2
   vagas por banco**, em advisory locks de Postgres — N chaves distintas por
   banco, e não uma, que é o que separa isto do fecho do varrimento. O 2 é o
   apertado que já estava medido (1, 2 e 4 em paralelo contra a CGD a
   2026-07-26, zero falhas nos três), e não o resultado de procurar onde o banco
   parte.

   **O N+1 desiste, não faz fila** — a §7.2 deixava as duas em aberto. Quem
   ficasse em fila continuava a segurar uma ligação à base e a gastar o prazo do
   cliente para no fim ouvir «o banco não respondeu», quando esperou por nós.
   Sai `503 banco_ocupado` com `Retry-After`, e **não** uma oferta em falha: essa
   dizia que o banco está em baixo quando quem não tem lugar somos nós.

   ⚠️ **Custa uma ligação ao Postgres por pedido em voo** — o lock é de sessão.
   Com cinco bancos a duas vagas são dez, e é por isso que o pool passou a ser
   dimensionado a partir daqui: o omissão do `pgxpool` (o maior de 4 e o número
   de CPUs) deixava o tecto a esfomear as consultas que ele existe para
   proteger, e o sintoma aparecia como lentidão em toda a API.

   ⚠️ **E falha FECHADO**, ao contrário do tecto por IP: se a base não responde,
   não se serve. A assimetria é de quem paga — o tecto por IP protege-nos a nós,
   este protege o simulador de um terceiro que não tem voz nenhuma nisto.

3. **⚠️ Timeout por banco, e a falha é por banco.** Um banco lento não pode
   segurar a comparação inteira. Quem não responder dentro do prazo sai como
   `banco_indisponivel`, nomeado — nunca em silêncio e nunca substituído por
   preço antigo (D3).

   ⚠️ **Medido a 2026-08-06:** o Montepio não respondeu dentro de **10 s** em 4
   cenários, num varrimento em hora de expediente. Ao vivo isso é um cliente à
   espera, e é a razão de o prazo ser por banco e não global.

4. **A resposta vai por banco, à medida que chega** (D2). A app pede um banco de
   cada vez — `POST /api/v1/ofertas/{banco}` — e mostra o que já tem.

   ⚠️ **`POST` e não `GET`:** o pedido leva data de nascimento e rendimento, e num `GET` isso ia na query string — histórico, logs de proxies e `Referer`. Ver `API.md`.

   ⚠️ **O fan-out passa a viver na app**, e com ele a responsabilidade de não
   disparar tudo de uma vez. Foi escolha contra SSE: o `fetch` do React Native
   não o suporta nativamente e ligações longas atravessam mal proxies. É o ponto
   mais discutível deste desenho e está assinalado como tal na
   `DECISAO-AO-VIVO.md`.

5. **⚠️ Somos um amplificador, e isso é o novo risco central.** Um pedido nosso
   vira ~10 pedidos aos bancos — medido: **1,00** por simulação no Banco CTT,
   **2,03** no Montepio, **4,00** no Santander. Com origem aparente nossa.

   O tecto por IP deixa de ser boa educação e passa a ser **estrutural**. E
   passa a contar **N pedidos por comparação** em vez de um.

   ✅ **Contado a 2026-08-07, e o receio não se confirmou.** Dizia aqui que
   «dimensionado como está, tranca um utilizador normal a meio da primeira
   comparação» — **é falso, e fica corrigido**: uma comparação são 5 pedidos (um
   por banco) contra um tecto de 60 por minuto, ou seja **12 comparações por
   minuto** e 12× de folga sobre a primeira. O tecto **fica nos 60**, agora com a
   conta escrita ao lado dele em vez de uma frase que ninguém tinha somado.

   ⚠️ E o que sustenta os 60 é não ser este o travão que protege os bancos —
   esse é o tecto de concorrência **por banco** (§7.2), que não depende de quantos
   IPs apareçam. Confundir os dois leva a apertar o que se vê e a deixar aberto o
   que conta.

   ⚠️ **O `PROXIES_DE_CONFIANCA` sobe de dívida a BLOQUEANTE.** Sem ele medido,
   ou o tecto é contornável, ou é o tecto do site inteiro — o bug de produção do
   v1. Antes dava para adiar; agora é ele que separa um serviço de uma
   ferramenta de carga contra cinco bancos.

   🔶 **Medido a 2026-08-07 contra um proxy a sério** (Caddy em contentor,
   `internal/infra/web/proxy_integracao_test.go`): os dois critérios da `KAN-14`
   passam a correr no portão — um cliente que forja o cabeçalho atrás de um proxy
   de confiança **não escapa** ao tecto, e duas máquinas atrás do mesmo proxy têm
   tectos separados. ⚠️ **O que continua por medir é o VALOR**, e depende do
   alojamento, que está adiado: a lista fica vazia até haver um proxy real à
   frente cujo endereço se possa ler.

   ⚠️ **E há um facto medido que muda como se configura isto:** por omissão o
   **Caddy 2 substitui** o `X-Forwarded-For` pelo endereço de quem lhe falou, em
   vez de acrescentar — com o cliente a mandar `203.0.113.10`, o serviço recebeu
   `172.17.0.1` e mais nada. Um nginx com `$proxy_add_x_forwarded_for`
   **acrescenta**. **A cadeia que chega cá depende do proxy que se puser à
   frente**, e o `PROXIES_DE_CONFIANCA` mede-se contra o que ele faz de facto e
   não contra o que se supõe que faça.

6. **A cache volta** (`KAN-8`, que a inversão de Julho tinha matado). É a única
   defesa que corta carga sem cortar funcionalidade.

   ⚠️ **E a quantização do pedido NÃO volta** (`KAN-15`). Ela existia para
   aumentar acertos arredondando o montante, e a razão pela qual era perigosa
   está escrita desde o v1: se o arredondamento cruzar um degrau de preço, dá-se
   a banda errada. Essa razão **volta a valer por inteiro** — e agora sem a
   protecção estrutural que a grelha dava, porque não há grelha.

   **A chave é o pedido exacto.** Um acerto a mais não paga um preço errado.

   ✅ **Feito a 2026-08-07** (`internal/infra/cache`, `respostas_em_cache`,
   `CACHE_VALIDADE`): chave = `SHA-256` do par (banco, pedido normalizado), valor
   = a resposta já traduzida para o contrato, validade de **5 minutos**.

   ⚠️ **«Normalizado» não é «quantizado».** O que a normalização faz é fixar uma
   ordem e uma grafia para os mesmos valores — é serialização determinística, e
   nenhum número muda. A quantização mudava-os, e é a `KAN-15`, que continua
   morta. Nada se arredonda, nada se ordena: dois pedidos que difiram num cêntimo
   são chaves diferentes, e a ordem dos produtos escolhidos entra na chave tal
   como veio, porque tratá-la como conjunto já era uma decisão sobre o que o
   banco considera igual.

   ⚠️ **A versão do formato entra no que se resume.** Sem ela, mudar a
   serialização fazia linhas antigas responderem a pedidos novos — um acerto
   errado e silencioso, que é a única coisa que esta cache pode causar de grave.

7. **⚠️ Nada de estado em-processo**, e a regra fica **mais** necessária, não
   menos. O tecto por banco, o tecto por IP e a cache vivem em Postgres — que já
   existe. O v1 tinha-os no processo e avisava no arranque que com mais do que um
   worker o mesmo banco levava N pedidos em paralelo, que é precisamente o que o
   tecto existe para evitar.

8. **O resíduo sobrevive, com outro papel.** Deixa de ser o travão contra uma
   grelha envenenada — não há grelha — e passa a ser **coerência interna da
   resposta do banco**: a prestação que ele devolveu contra a amortização
   francesa sobre o plano que ele próprio descreveu. Custa uma multiplicação,
   apanha um campo mal lido, e agora estraga **uma** resposta em vez de todas.

   ⚠️ É o argumento da §7.4 antiga lido ao contrário, e é a favor desta reversão.

### O que morreu com o varrimento

Não é uma limpeza: é a maior parte do que este repositório tinha de próprio.

| morreu | porque existia |
|---|---|
| `dominio.Encargos` e o ajuste 2×2 | derivar a TAEG. O banco dá-a para o pedido exacto |
| `dominio.EscalaDeLTV`, degraus, fronteiras medidas | dar o spread do LTV pedido. O banco preça-o |
| `base_fixa` | corrigir a TAN da fixa para outro LTV |
| descontos por produto e a sua composição | preçar produtos escolhidos. Pedem-se ao banco |
| a sonda, `simulador sondar`, `sondagens`, a fiabilidade | detectar uma fotografia a envelhecer |
| `serie_desactualizada`, a viragem do dia | não servir através da fixação da Euribor |
| as 7 famílias da grelha, ~96 pontos por banco | preencher a grelha |

⚠️ **E morreu bem: cada uma destas peças era uma hipótese sobre como um banco
preça.** As quatro que os dados contrariaram num só dia — `KAN-54` a `KAN-57` —
eram todas desta lista. O que sobra não tem hipóteses para contrariar.

✅ **Esta tabela passou a ser verdade sobre o código também** (2026-08-08). O
`dominio.Encargos` (424 l.), o `dominio.TAEGDeFluxos` (207 l.), a
`dominio.EscalaDeLTV` (190 l.) e o `dominio.mercado` (78 l.), mais 971 linhas de
testes, **saíram do repositório** — 1 892 linhas. Tinham saído do contrato a
2026-08-07, com a `taeg` e o `mtic` a passarem a ser o que o banco cotou; o
código ficou mais um dia, e durante esse dia esta tabela e o pacote `dominio`
discordavam.

⚠️ **A prova de que não tinham chamador é o compilador, e não uma busca.** Foram
apagados de uma vez e o `go build ./...` passou à primeira: em Go, uma referência
que ficasse para trás é `undefined` e não compila. O que o portão **não** teria
apanhado sozinho é que existiam — código sem chamadores compila e passa nos
testes para sempre, que é como sobreviveram um dia inteiro depois de mortos.

⚠️ **Não se voltam a ligar.** Cada um era uma hipótese sobre como um banco preça,
e quatro caíram num só dia (`KAN-54` a `KAN-57`). Quem precisar de um número
destes pergunta ao banco.

### O que sobreviveu, e é o que resta

- Os **cinco parsers** e as **capturas**. Passam a ser o activo principal.
- ⚠️ A medição de fidelidade — **zero divergências em 147 098 comparações** —
  deixa de ser curiosidade e passa a ser a garantia central: é ela que diz que o
  que servimos é o que o banco disse.
- O `DOSSIE-BANCOS.md`, inteiro.
- A regra de dependência, o portão, o `-race`, o pt-PT, a verificação por
  reversão.

**⚠️ IP atrás de proxy — bug de produção do v1, e agora crítico.** No v1 o
servidor não aceitava o `X-Forwarded-For` de um proxy noutro contentor e o IP do
cliente passava a ser o do proxy, igual para toda a gente: o tecto por IP virava
o tecto do site inteiro. A correcção **não** é confiar em toda a gente — isso
deixa qualquer cliente escolher o seu IP num cabeçalho. É uma lista explícita de
proxies de confiança, com default vazio, e um teste que confirma que um
`X-Forwarded-For` enviado directamente à app é ignorado. Ver o ponto 5.

## 8. Verificação

Regra da casa: um critério de pronto vale pelo que se mediu. Reverte-se o defeito e confirma-se que o teste **falha**, e que falha a nomear a coisa certa.

Cinco portões, todos automáticos:

1. `depguard` — a regra de dependência de §3.
2. **Teste do contrato contra o servido** — toda a rota declarada no `openapi.yaml` tem handler (KAN-44), e todo o schema declarado é alcançável a partir de um caminho. ⚠️ Substitui o teste de contrato do `/api/rate-catalog`, que saiu com a rota; e o segundo dos dois nasceu de essa saída ter deixado **oito** schemas órfãos com o portão verde.
3. **Testes de captura por banco** — cada `resposta.go` contra as capturas versionadas, offline e sem rede.
4. `golangci-lint run ./...` limpo, sobre o repositório inteiro. ⚠️ Não sobre um subconjunto. O v1 tinha 368 erros permanentes fora de `src/`, o que cegou o portão por completo: ninguém lê uma saída de 368 linhas para descobrir a 369.ª. Aqui não há pasta de scripts onde eles se acumulem, e o portão é o repositório todo ou não é portão.
5. `go test -race ./...` — o detector de corridas é obrigatório no portão. ⚠️ **E ganhou valor com a reversão da §1:** o fan-out concorrente deixou de estar num subcomando nocturno e passou a estar no caminho que serve pessoas. Uma corrida de dados aqui produz números errados, não um crash.

⚠️ **O resíduo não é um sexto portão, e não pode ser.** Mede-se contra os bancos reais, portanto depende de servidores de terceiros estarem de pé — e um portão que amarela por isso deixa de ser lido. Passou a viver no caminho do cliente (§7.8) e é registado; o que o portão verifica é que o **cálculo** do resíduo está certo, contra respostas gravadas nas capturas.

⚠️ **Nem o `-race` é detector de fugas de goroutines.** Medido a 2026-07-25: um teste que deixa uma goroutine bloqueada para sempre passa `go test -race` a verde, sem uma palavra. Onde isso importar — e passou a importar no caminho do cliente, onde uma fuga por pedido acumula — a afirmação é separada, com uma sonda que espera a contagem voltar ao valor de partida (medida: 0 falsos positivos em 300 corridas, 50 fugas apanhadas em 50).

⚠️ **E há um sexto portão que não é automático: a corrida de fidelidade por banco.** Cada banco implementado tem um `TestFidelidade…` atrás de `//go:build rede` que lê cada resposta duas vezes — por dois caminhos escritos de propósito para não se parecerem — e compara campo a campo. Não entra no `make verificar` pela mesma razão do resíduo. Corrido nos **cinco** bancos implementados: 3002 ofertas em cada um, **zero divergências** em 147 098 comparações somadas — 36 024 na CGD, 33 022 no Novo Banco, 36 024 no Montepio, 24 016 no Banco CTT e 18 012 no Santander; erro de leitura ≤ 0,100 % a 95 % de confiança, pela regra dos três. ⚠️ Não se lê isso como «acertamos 99,9 %» — o observado é zero, e os 0,100 % são o que a amostra permite excluir. ⚠️ **Nem se lê a coluna das comparações como medida de rigor**: compara-se o que cada banco afirma, e o Santander nomeia metade dos campos da CGD porque publica o preço num plano de troços. Inflacioná-la obrigaria a comparar campos que somos nós a derivar — mediria o derivador, não a leitura.

⚠️ **E a corrida mede uma segunda coisa, que decide desenho: o que custa sondar cada banco.** Pedidos HTTP por simulação, medidos: **1,00** no Banco CTT, 2,03 no Montepio, **4,00** no Santander — que repaga em cada simulação a descoberta da configuração, os limites e o catálogo. Quatro vezes mais carga no banco pela mesma informação. ⚠️ **Com a reversão da §1 este número deixa de ser um argumento de eficiência e passa a ser o custo de cada cliente:** uma comparação a cinco bancos custa ~10 pedidos a terceiros, e é ele que fixa o ponto a partir do qual esta arquitectura carrega os bancos mais do que o varrimento carregava (~190 comparações/dia). Reaproveitar config e catálogo dentro de um pedido passa de optimização a defesa. Está no `DOSSIE-BANCOS.md` com a tabela completa.
