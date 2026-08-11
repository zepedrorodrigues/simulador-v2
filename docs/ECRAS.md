# Ecrãs da app

Especificação da app **React Native** (repositório separado). Este documento existe neste repositório porque define o que a API tem de servir e em que momento — ver `API.md`.

## O problema que isto resolve

A issue #2 do v1, escrita pelo próprio autor:

> «Currently all ui is a single window with form for request and response — visually too much information + disformatting + just crap»

O v1 tinha um formulário de \~25 campos e a tabela de resultados na mesma página, num ficheiro de 874 linhas. **A regra do v2: um ecrã, uma pergunta.** O utilizador nunca vê ao mesmo tempo o que está a pedir e o que já recebeu.

## Mapa

⚠️ **Havia aqui um ecrã «A comparar» entre o pedido e as ofertas, e saiu a 2026-07-28** — ver a §2, que fica no lugar dele.

⚠️ **E havia à parte um `Mercado` — a série do `rate-catalog` —, que sai a 2026-08-08.** Não é adiamento: a rota foi apagada do servidor (Fase 6, passo 5) e o varrimento que a alimentava também. Uma série temporal de preços precisa de alguém a varrer em hora morta, e ninguém varre. O separador inferior fica com os ecrãs que existem.

---

## 1. Pedido — três passos

Um passo por ecrã, com barra de progresso. Nada de acordeões nem de secções recolhidas: no telemóvel, um formulário de 25 campos numa página é ilegível.

### 1.1 O imóvel e o empréstimo

⚠️ **Saiu daqui o «✓ dentro dos limites de todos», a 2026-07-29, ao construir o ecrã.** Estava desenhado sob o LTV e **não há como o afirmar**: o `GET /api/v1/bancos` publica `prazo_min`, `prazo_max`, `idade_maxima_fim`, `periodos_fixos`, `euribor_opcoes`, `euribor_imposto`, `produtos` e `notas` — e nenhum campo de limite de LTV. O `RESUME.md` confirma-o do outro lado: «o domínio de LTV a varrer não sai do banco». Era a app dar uma garantia que não tem de onde saber, e vale aqui a regra da casa: quando um documento e o `openapi.yaml` discordam, o documento é que está errado.

⚠️ **E o deslizador do prazo passou a contador.** Um deslizador em React Native é um módulo nativo a mais para manter, o alvo web é o primeiro a publicar-se (`APP.md` §5), e acertar em 30 com o dedo num deslizador de 1 a 50 é pior do que carregar duas vezes. O intervalo é o mais largo dos bancos escolhidos, e quem fica de fora do prazo é nomeado por baixo.

⚠️ **O LTV é calculado e mostrado ao vivo, com a leitura do que significa.** O spread é uma função **em degraus** do LTV; quem está a 68,1 % beneficia de saber que baixar 1 000 € pode mudar o preço. O v1 tinha um campo `LTV` desactivado, sem explicação.

⚠️ **E os degraus não são de 5 %, ao contrário do que esta linha dizia até 2026-07-28.** Medido na KAN-35: na CGD o preço **desce** quando o LTV sobe e quebra aos **67 %**; no Novo Banco **sobe** e quebra em 50/51, 70/71 e 80/81; no Montepio **não muda de todo**. São três formas diferentes, e são as do banco — não as nossas. A app **não desenha degraus próprios**: mostra o LTV e diz que o preço muda por patamares, sem inventar onde eles caem.

### 1.2 Os titulares

⚠️ **A caixa explica porque é que a app pede a data de nascimento.** Pedir dados pessoais sem justificar é o que faz uma app parecer um funil de leads.

⚠️ **As idades saem dos dados, e não estavam a sair — dizia aqui «(75-83)»**, escrito à mão (corrigido a 2026-07-29, ao construir o ecrã). Vêm do `idade_maxima_fim` dos bancos escolhidos e mudam com a escolha. Medido contra o servidor a sério nesse dia: com os cinco bancos, a caixa diz **«70-80 anos»** — 70 da CGD, 80 do Santander. Um número escrito à mão numa frase fica errado no dia em que entra o sexto banco, e ninguém o vai lá corrigir.

⚠️ **O rendimento só se pergunta a quem o usa, e isso mudou o ecrã.** Medido nos cinco bancos: **um** declara `rendimento_mensal` com `usa: true` — o Novo Banco — e a nota dele diz que «o simulador exige-o, mas ele não mexe no preço: só entra na recomendação e no rácio de esforço». Quando nenhum dos bancos escolhidos o lê, o campo **não aparece** e a caixa diz porquê; no pedido vai zero, que é o valor inerte de um campo obrigatório no contrato e que ninguém daquela comparação lê. Perguntar o ordenado de uma pessoa para não fazer nada com ele é exactamente o funil que o parágrafo de cima recusa.

⚠️ **Mas a data de nascimento pergunta-se sempre, mesmo à CGD, que não a usa.** `usa: false` quer dizer «o simulador daquele banco não pergunta isto», e não «é irrelevante»: na CGD a idade entra à mesma no prazo máximo, que ela limita a terminar aos 70 anos. O que a app faz com o `usa: false` é mostrar a **nota do banco**, palavra por palavra como vem no contrato, debaixo do campo — que é a resposta exacta à pergunta que a pessoa faz quando lhe pedem a data de nascimento.

⚠️ **E a app não pré-julga nenhum banco por idade.** Seria fácil calcular aqui a idade no fim do crédito e marcar já os que a recusam, e estaria errado: a `KAN-34` está aberta precisamente porque o `idade_maxima_fim` é um número só e na CGD depende da finalidade. Quem decide que um banco não tem oferta é o servidor, que devolve a razão em português (§3). A app explica a regra; não a aplica.

### 1.3 A taxa e os bancos

⚠️ **O tempo por banco saiu deste ecrã a 2026-07-28, e o campo que o alimentava saiu do contrato (KAN-32).** Estava aqui «☑ CGD ~2 s / ☐ Banco BPI ~50 s / Tempo estimado: ~6 s», tirado do campo `custo` de `GET /api/v1/bancos`, e a justificação era boa: «é honesto e faz o utilizador entender porque é que uma comparação demora 6 s e outra 52 s».

⚠️ **Voltou a ser verdade a 2026-08-06.** Isto tinha sido anulado pela inversão da §1 — «nenhum banco é interrogado no caminho do cliente» — e a §1 foi revertida. **Cada banco escolhido é um pedido**, e o tempo volta a ser do cliente: medido a 2026-08-06, o Montepio não respondeu dentro de **10 s** em 4 cenários.

⚠️ **Mas não volta o «~2 s» anunciado no formulário**, e a razão mudou: com um pedido por banco (D2), a lista **enche-se à medida que chegam**. Anunciar um tempo total é anunciar o do banco mais lento a quem já está a ver quatro preços. O `custo` (`Requisitos.Custo`) volta ao caminho do cliente, onde decide o timeout de cada banco.

⚠️ **O formulário adapta-se ao que os bancos escolhidos precisam** — período fixo só na mista, indexante só onde é escolhível, profissão só no grupo BCP. A fonte é sempre `GET /api/v1/bancos`, nunca uma lista escrita na app.

⚠️ **As bonificações entraram neste ecrã a 2026-07-29, e não estavam desenhadas em lado nenhum.** Aparecem recolhidas sob cada banco marcado, com as `por_omissao` já ligadas — que é o que o domínio manda: «quem escolhe é a pessoa; o `PorOmissao` diz à app o que pré-seleccionar, e mais nada». Deixá-las de fora não era neutro: com o `Pedido.Produtos` vazio a comparação sai **sem bonificação nenhuma**, e com elas ligadas de forma desigual sai invertida. Está medido — a 2026-07-26, no mesmo cenário, a CGD respondia com o preçário base e o Novo Banco com as duas já ligadas, e lado a lado isso dizia que o Novo Banco era 0,45 p.p. mais barato quando em pé de igualdade é a CGD a mais barata por 0,25 p.p.

⚠️ **E o indexante que ninguém pode escolher diz-se, em vez de se mostrar uma escolha morta.** Quando todos os bancos escolhidos impõem o seu tenor, o selector não aparece: `euribor_opcoes` vazio, no contrato, não é «não sei» — é «o banco impõe o seu e ignora a escolha». Oferecer três tenores a quem escolheu só CGD e Montepio era deixá-la escolher uma coisa que não chega a lado nenhum. ⚠️ Confirmado a 2026-07-29 contra o servidor a sério: a caixa diz **«Banco CTT, CGD e Santander impõem o seu indexante»**, exactamente os três que o mock previa.

---

## 2. ⚠️ Volta a haver espera — mas não é um ecrã de espera

Havia um — «A comparar», com barra de progresso, cada banco a aparecer assim que chegasse, e um `Cancelar` que propagava pelo `context`. **Estava certo, e o número que o justificava era real:** medido no v1, 7 dos 10 bancos prontos aos 7,4 s, 9 aos 25,9 s, o último aos 52,2 s — uma barra única que só completasse no fim desperdiçava 45 s de informação já disponível.

⚠️ **Reescrito a 2026-08-06, com a §1 revertida.** Uma comparação volta a falar com bancos, e volta a haver espera. **Mas não volta o ecrã de espera do v1**, e a distinção decide o desenho: não há trabalho assíncrono nosso a que se pergunte «já está?» — há N pedidos, um por banco, cada um síncrono.

⚠️ **O que a pessoa vê é a própria lista a encher-se**, e não uma barra de progresso sobre um trabalho invisível. Um banco que ainda não respondeu é uma linha em espera; um que falhou é uma linha com o banco nomeado (D3). Não há `id` de simulação, não há sondagem, e não há nada a cancelar a não ser sair do ecrã.

⚠️ **E voltar a pô-lo seria pior do que nada:** uma barra de progresso sobre uma consulta de milissegundos é teatro, e teatro num sítio onde se comparam créditos ensina a pessoa a desconfiar do resto do ecrã. O submeter do passo 3 vai direito às Ofertas.

**O que sobreviveu dele mudou de sítio:** os estados que não são o caminho feliz — servidor em baixo, sem rede, tecto atingido, banco ocupado — deixaram de ser um ecrã intermédio e são estados **das próprias Ofertas** (§3), que é onde a pessoa está quando acontecem. ⚠️ O «banco sem série» já não é um deles: não há série, e a única coisa que um banco pode fazer é responder, recusar ou não responder a tempo.

⚠️ **O único progresso que a app mostra é o dos três passos do formulário.** É contagem de passos, não espera de trabalho: a pessoa está no segundo de três, e isso é verdade sem depender de nada que esteja a correr.

---

## 3. Ofertas

**Ordenação** por TAEG (omissão), prestação, TAN, spread ou MTIC. `★` marca a melhor de cada métrica.

⚠️ **O aviso de ajuste vive no cartão, não numa gaveta.** Sempre que `aplicado` não vem vazio, a nota aparece ali. É a regra de `API.md`: números diferentes dos pedidos sem o dizer, numa comparação de crédito, são enganadores.

⚠️ **Os bancos que falharam continuam na lista**, com a razão em português. Um banco que desaparece parece um esquecimento; um banco que explica porque não tem oferta é informação útil.

⚠️ **Esta regra esteve por cumprir até 2026-08-01, e a falta era do servidor** (`KAN-45`): ele devolvia ofertas dos bancos com série e não dos bancos pedidos — escolhiam-se cinco, o botão dizia «Comparar (5)», e a lista mostrava dois, sem rasto dos outros.

⚠️ **O código `sem_serie` que a corrigia saiu do contrato a 2026-08-07**, com a série. **A regra que ele servia é que fica**, e passou a ser mais forte: agora é a **app** que monta a lista com os cinco desde o primeiro instante (D2), portanto não há como um banco pedido não aparecer. Uma linha tem três estados — **à espera**, **servida**, ou **não chegou** — e nenhum deles é a ausência.

⚠️ **E «não chegou» não se disfarça de oferta em falha.** Uma oferta com `sucesso: false` traz a razão escrita pelo servidor, em português; um pedido que nem chegou ao servidor não tem essa frase, e fabricar-lhe uma era a app a afirmar o que o banco disse. O que ela sabe di-lo com as palavras dela: não houve rede, o serviço não respondeu, o tecto foi atingido.

⚠️ **Uma oferta em falha tem agora dois donos, e o cartão tem de os separar** (`KAN-30`, 2026-08-11). Até aqui a razão era sempre **do banco**; com o `erro_interno` ela é **nossa** — rebentou uma coisa cá dentro e o banco pode nem ter sido interrogado. **A frase continua a vir do servidor e é essa que se mostra**, mas o rótulo do cartão não pode dizer «Sem oferta»: isso afirma sobre o banco uma coisa que não se apurou. São **três** rótulos, e o código decide qual:

| o que aconteceu | rótulo | de onde vem a frase |
|---|---|---|
| o banco respondeu que não | «Sem oferta» | do servidor, e é a razão **do banco** |
| rebentou do nosso lado (`erro_interno`) | «Falha nossa» | do servidor, e a razão é **nossa** |
| o pedido não chegou ao servidor | «Não foi possível perguntar a este banco» | da **app**, com as palavras dela |

⚠️ **Não é uma quarta linha na lista nem um ecrã à parte:** o defeito é de **um** banco, e os outros quatro continuam a encher-se. É a mesma razão por que o servidor serve isto num `200` e não num `500`.

### Quantas vezes um facto se afirma (A6, 2026-08-11)

⚠️ **Uma falha que aconteceu UMA vez não se mostra cinco.** O fan-out dá uma linha por banco, e uma falha que não é de banco nenhum — não há rede, o serviço não responde, o tecto por IP fechou — aparecia em cada uma delas, com o nome de um banco por cima. É o defeito que o `426` teve, e a regra que dele sai vale para todos os estatutos:

| espécie | de quem é | como se mostra |
|---|---|---|
| `bancoOcupado` (`503`) | **daquele banco** — conta pedidos nossos em voo contra ele | uma linha por banco |
| `semRede`, `servidorEmBaixo`, `tectoExcedido`, `pedidoInvalido`, `versaoDemasiadoAntiga` | de nós ou da ligação | **uma vez**, no ecrã, quando atinge todos |

⚠️ **Só quando atinge TODOS.** Com uma oferta já servida a lista tem valor e fica; e com um banco por responder não há veredicto nenhum, porque «não há ofertas» faz a pessoa sair do ecrã enquanto uma resposta vem a caminho — é a mesma regra da estrela que não se dá a 2 de 5.

⚠️ **E o botão «tentar de novo» é uma decisão, não um adorno.** Não aparece em duas espécies: no `tectoExcedido`, porque o servidor mandou esperar a **janela inteira** e não diz o que falta dela de propósito — «dizer exactamente quando reabre convida a bater à porta ao segundo» —, e na `versaoDemasiadoAntiga`, porque a acção está na loja. Um botão que não pode funcionar promete uma coisa que não acontece.

⚠️ **«Nenhum dos bancos tem oferta para este pedido» afirma sobre o PEDIDO**, e por isso só se escreve quando **todos** responderam e nenhum tem preço. Um banco que não respondeu não sustenta essa conclusão: o que se sabe dele é que não se sabe.

⚠️ **Os produtos aplicados são visíveis em cada cartão.** Sem isso, um banco com descontos por omissão parece simplesmente mais barato. Foi a lição que o v1 demorou a aprender na sua própria série de mercado.

⚠️ **A TAEG deixou de levar marca de derivada, a 2026-08-07, e isto dizia o contrário.** Dizia — desde 2026-07-28 e com razão — que a `taeg` e o `mtic` não vinham cotados pelo banco, que eram calculados dos encargos medidos, e que por isso o cartão levava um `~` antes do número e os `pressupostos` inteiros no detalhe. **Ao vivo são o que o simulador do banco devolveu.** Não há derivação, não há hipóteses nossas a declarar, e o `~` e o `pressupostos` saíram do contrato e do ecrã.

⚠️ **A obrigação da MCD não desaparece — muda de dono.** O Anexo I, Parte II e o Anexo II mandam declarar as hipóteses junto do número que delas depende, e essas hipóteses são agora **do banco** e chegam nas `notas` dele (o Montepio declara lá que projecta a taxa do período fixo para o resto do prazo). **O que a app continua obrigada a dizer** é que uma simulação não é uma proposta — mas isso é a distinção entre simulação e proposta, e não entre estimado e cotado.

⚠️ **E a regra que estava por trás do `~` fica inteira:** um número derivado servido com ar de cotado é a falha que o v1 registou. A forma de a não cometer, agora, é não derivar número nenhum.

⚠️ **E a idade do preço aparece, sempre.** Cada oferta traz o `capturado_em` — **o instante em que o banco cotou**, e não o de um varrimento —, e a resposta traz o `calculado_em` dela própria. ⚠️ **Os dois podem diferir, e é por isso que são dois:** uma resposta pode vir da cache do servidor (`KAN-58`, validade de 5 min), e aí o preço é de há minutos e não de agora. **Um preço sem data não se serve**, e a fronteira recusa-o — há guarda no servidor que desce uma oferta sem `capturado_em` para falha.

⚠️ **Havia aqui três parágrafos sobre a `fiabilidade`** (`KAN-49`): o aviso no cartão quando uma sonda contradissesse o preço, o silêncio nos outros dois estados, e a regra de que a dúvida não tirava a estrela. **Saem com a sonda e com a grelha** (2026-08-07) — ao vivo não há uma terceira coisa entre a resposta do banco e o que se serve, logo não há sobre o que ter uma opinião.

⚠️ **A regra que esses parágrafos carregavam fica escrita, porque vale para o que vier:** um estado que só se publica quando as notícias são más ensina quem o lê a tratar a ausência como boa notícia. Se voltar a haver uma verificação de preço, volta com três estados e não com um booleano — e o silêncio faz-se no ecrã, não no contrato.

⚠️ **Construído a 2026-07-29 (A5), e quatro coisas ficaram decididas aqui que não estavam desenhadas:**

⚠️ **O rodapé dá o `capturado_em` MAIS ANTIGO da lista, não o mais recente.** É uma afirmação sobre o conjunto de preços, e a afirmação verdadeira sobre preços de horas diferentes é a do mais velho. Dizer a hora do mais fresco apresentava os outros como sendo dessa hora — que é a mesma falha, à escala da lista, de servir um preço sem data.

⚠️ **Uma oferta ajustada não leva a estrela, e continua ordenada.** É a posição desta app sobre a `KAN-26` («ordenar uma oferta ajustada ao lado das outras compara coisas diferentes sem o dizer»). A estrela não é uma nota: é a afirmação «esta é a melhor», e dá-la a quem foi simulado a 35 anos quando se pediram 40 afirma uma coisa falsa sobre o pedido que a pessoa fez. A nota do ajuste continua no cartão; o que a oferta não leva é a marca. ⚠️ Um empate também não dá estrela a ninguém — marcar a primeira deixava a ordem de chegada decidir.

⚠️ **E uma oferta sozinha não é a melhor de nada** (acrescentado a 2026-07-29, a correr a app contra o servidor a sério). Com série de dois bancos e um deles sem o cenário pedido, a CGD ficava sozinha na lista e o cartão dela apanhava as **cinco** estrelas — melhor TAEG, melhor prestação, melhor TAN, melhor spread, melhor MTIC. Cada uma era verdadeira e o conjunto era falso: cinco estrelas empilhadas lêem-se como uma recomendação forte no sítio exacto onde não havia comparação nenhuma. Uma estrela só diz alguma coisa **contra outra oferta**. ⚠️ Isto não se via com dados fabricados — os testes tinham sempre duas ofertas com preço.

⚠️ **Uma oferta a que falte a métrica de ordenação vai para o fim das que têm preço, nunca para o meio.** `undefined` numa subtracção dá `NaN`, e um `sort` com `NaN` deixa a lista por ordem arbitrária — a pior avaria possível aqui, porque **parece ordenada**. A §4 do `ARQUITETURA.md` permite omitir uma TAEG que não se consegue dar, portanto o caso é legítimo e não hipotético.

⚠️ **Havia aqui a regra de nomear como defeito NOSSO uma `taeg` sem `pressupostos`**, e ela **ia marcar todos os cartões** a partir de 2026-08-07: o servidor ao vivo nunca preenche `pressupostos` — o campo saiu do contrato —, e a app punha uma caixa vermelha a acusar-nos de um defeito que não existe em cada oferta com preço. Foi apanhada **a ler o código** e não por um teste: o teste passava porque a fixture preenchia o campo que o servidor a sério não preenche.

⚠️ **A lição fica, e é sobre fixtures e não sobre pressupostos:** uma regra que dispara sobre a **ausência** de um campo precisa de um caso em que o campo esteja mesmo ausente, e uma fixture que o preenche por conveniência apaga exactamente esse caso.

⚠️ **A estrela espera pelos bancos que faltam.** Enquanto houver uma linha «à espera», nenhuma oferta leva estrela — é a mesma afirmação do parágrafo de cima levada ao fan-out: a melhor de duas de cinco desmente-se quando chega a terceira, e a pessoa lê, decide, e o ecrã muda-lhe a resposta debaixo dos olhos. ⚠️ Um banco que **falhou** não segura as estrelas: já assentou, e não vai mudar a comparação.

---

## 4. Detalhe da oferta

⚠️ **O gráfico de fases é a informação que o v1 não dava bem.** Numa taxa mista, a TAN dos primeiros anos não é o custo do crédito — é o chamariz. Mostrar as fases lado a lado é a diferença entre comparar e ser induzido em erro.

⚠️ **A proveniência e a hora aparecem sempre, e este parágrafo já esteve errado duas vezes.** Dizia primeiro «um valor de cache com 4 horas tem de o dizer»; passou a dizer que **não havia** cache e que todos os valores vinham de um varrimento anterior. **Nenhuma das duas coisas é verdade hoje:** os valores vêm do banco no momento em que se pergunta, e **há** cache (`KAN-58`, 5 min, em Postgres). A hora não é uma excepção a assinalar — é parte de cada oferta (`capturado_em`), e o rodapé desta página di-la sempre.

⚠️ **Os `pressupostos` deixaram de se mostrar aqui**, porque deixaram de existir (2026-08-07, §3). O que ocupa o lugar deles são as **`notas` do banco** — é onde as hipóteses de cálculo passaram a viver, e são dele e não nossas. ⚠️ **Não vão sob os números:** a app mostra-as numa secção «Notas» própria, depois das fases, dos produtos aplicados e da composição — verificado no `app/ofertas/[banco].tsx` a 2026-08-08. E quando há ajuste, juntam-se ao aviso de ajuste em vez de aparecerem duas vezes.

⚠️ **O `ate_mes` das fases é acumulado desde o início do contrato, não a duração da fase** (2026-07-29). É o contrário do que os bancos devolvem — o domínio do backend tem os dois tipos, `Fase` e `FaseDuracao`, precisamente por causa disso — e lido como duração dá «Anos 1-5» seguido de «Anos 6-11» numa mista de 5 anos a 30. **O erro parece certo:** os números são plausíveis, estão por ordem, e ninguém repara sem somar. Está escrito aqui porque este gráfico é o único sítio da app que lê o campo. ⚠️ Confirmado com dados reais nesse dia: a CGD devolveu `ate_mes` 60 e 360, e o detalhe mostra **«Anos 1-5»** e **«Anos 6-30»**.

⚠️ **E a largura de cada fase é proporcional à duração.** Dois blocos do mesmo tamanho, numa mista de 5 anos a 30, dizem que metade do crédito é à taxa do chamariz — que é exactamente o que este gráfico existe para não deixar acontecer.

---

## 5. Bancos: o que cada um precisa

Ecrã de consulta, alimentado por `GET /api/v1/bancos`. Responde às perguntas que no v1 viviam num painel apertado ao fundo da página: que campos é que este banco usa mesmo, que períodos fixos aceita, que indexante impõe, que produtos tem, até que idade financia.

⚠️ **Parte deste ecrã já vive noutro sítio, e é de propósito** (2026-07-29). O campo `nota` de cada `BancoInput` — «o simulador da CGD não pergunta a idade; ela só entra no prazo máximo, que a CGD limita a terminar aos 70 anos» — aparece **debaixo do campo a que diz respeito**, no passo 2, e não só aqui. A pergunta «porque é que me estão a pedir isto?» faz-se no momento de preencher, não num ecrã de consulta a que é preciso ir de propósito. Este ecrã continua a fazer falta para a leitura completa, banco a banco.

---

## Regras transversais

- **Português de Portugal em toda a interface.** Sem gerúndio brasileiro.
- **Nunca um número sem unidade nem contexto.** `3,61 %` é TAEG ou TAN? Rotular.
- **Modo claro e escuro** desde o início.
- **Acessibilidade**: alvos de toque ≥ 44 pt, contraste AA, rótulos para leitor de ecrã em todos os campos. Uma app financeira lida com pessoas de todas as idades.
- ⚠️ **A app não guarda dados pessoais.** Os inputs vivem no estado do ecrã e desaparecem. Se um dia houver «guardar simulação», é armazenamento **local** no dispositivo — o servidor continua sem guardar nada (`ARQUITETURA.md` §4).
- ⚠️ **Isto não é aconselhamento financeiro, e a app diz isso onde é preciso** — não escondido nas definições. Os valores são indicativos, de simuladores públicos, sem análise de risco personalizada.
