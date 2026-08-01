# Ecrãs da app

Especificação da app **React Native** (repositório separado). Este documento existe neste repositório porque define o que a API tem de servir e em que momento — ver `API.md`.

## O problema que isto resolve

A issue #2 do v1, escrita pelo próprio autor:

> «Currently all ui is a single window with form for request and response — visually too much information + disformatting + just crap»

O v1 tinha um formulário de \~25 campos e a tabela de resultados na mesma página, num ficheiro de 874 linhas. **A regra do v2: um ecrã, uma pergunta.** O utilizador nunca vê ao mesmo tempo o que está a pedir e o que já recebeu.

## Mapa

```
  Pedido (3 passos)  ─►  Ofertas  ─►  Detalhe da oferta
        │                   │
        └──► Bancos: o     └──► Comparar duas lado a lado
             que cada um
             precisa
```

⚠️ **Havia aqui um ecrã «A comparar» entre o pedido e as ofertas, e saiu a 2026-07-28** — ver a §2, que fica no lugar dele.

E, à parte, `Mercado` — a série do `rate-catalog`. Fica para depois de a app funcionar (fase 5), mas o desenho já a prevê no separador inferior.

---

## 1. Pedido — três passos

Um passo por ecrã, com barra de progresso. Nada de acordeões nem de secções recolhidas: no telemóvel, um formulário de 25 campos numa página é ilegível.

### 1.1 O imóvel e o empréstimo

```
┌──────────────────────────────┐
│ ●○○            O imóvel      │
├──────────────────────────────┤
│  Valor do imóvel             │
│  ┌────────────────────────┐  │
│  │            250 000 €   │  │
│  └────────────────────────┘  │
│  Quanto quer financiar       │
│  ┌────────────────────────┐  │
│  │            200 000 €   │  │
│  └────────────────────────┘  │
│  LTV 80,0 %                  │
│  ████████████████░░░░░░░░░░  │
│  O preço muda por patamares  │
│  de LTV, e cada banco tem os │
│  seus.                       │
│                              │
│  Prazo                       │
│  [ − ]     30 anos     [ + ] │
│                              │
│  Finalidade                  │
│  [Própria][Secundária][Arr.] │
│                              │
│            [ Seguinte → ]    │
└──────────────────────────────┘
```

⚠️ **Saiu daqui o «✓ dentro dos limites de todos», a 2026-07-29, ao construir o ecrã.** Estava desenhado sob o LTV e **não há como o afirmar**: o `GET /api/v1/bancos` publica `prazo_min`, `prazo_max`, `idade_maxima_fim`, `periodos_fixos`, `euribor_opcoes`, `euribor_imposto`, `produtos` e `notas` — e nenhum campo de limite de LTV. O `RESUME.md` confirma-o do outro lado: «o domínio de LTV a varrer não sai do banco». Era a app dar uma garantia que não tem de onde saber, e vale aqui a regra da casa: quando um documento e o `openapi.yaml` discordam, o documento é que está errado.

⚠️ **E o deslizador do prazo passou a contador.** Um deslizador em React Native é um módulo nativo a mais para manter, o alvo web é o primeiro a publicar-se (`APP.md` §5), e acertar em 30 com o dedo num deslizador de 1 a 50 é pior do que carregar duas vezes. O intervalo é o mais largo dos bancos escolhidos, e quem fica de fora do prazo é nomeado por baixo.

⚠️ **O LTV é calculado e mostrado ao vivo, com a leitura do que significa.** O spread é uma função **em degraus** do LTV; quem está a 68,1 % beneficia de saber que baixar 1 000 € pode mudar o preço. O v1 tinha um campo `LTV` desactivado, sem explicação.

⚠️ **E os degraus não são de 5 %, ao contrário do que esta linha dizia até 2026-07-28.** Medido na KAN-35: na CGD o preço **desce** quando o LTV sobe e quebra aos **67 %**; no Novo Banco **sobe** e quebra em 50/51, 70/71 e 80/81; no Montepio **não muda de todo**. São três formas diferentes, e são as do banco — não as nossas. A app **não desenha degraus próprios**: mostra o LTV e diz que o preço muda por patamares, sem inventar onde eles caem.

### 1.2 Os titulares

```
┌──────────────────────────────┐
│ ●●○           Os titulares   │
├──────────────────────────────┤
│  1.º titular                 │
│  Nascimento   [ 12/04/1990 ] │
│                              │
│  [ + Acrescentar 2.º titular]│
│                              │
│  ┌ ⓘ ───────────────────────┐│
│  │ Nenhum dos bancos        ││
│  │ escolhidos usa o         ││
│  │ rendimento, por isso não ││
│  │ o pedimos.               ││
│  └──────────────────────────┘│
│  ┌ ⓘ ───────────────────────┐│
│  │ A idade define o prazo   ││
│  │ máximo: nos bancos       ││
│  │ escolhidos, até aos      ││
│  │ 70-80 anos.              ││
│  └──────────────────────────┘│
│  O que cada banco diz disto  │
│  CGD: o simulador não        │
│  pergunta a idade. Ela só    │
│  entra no prazo máximo…      │
│         [ ← ]  [ Seguinte → ]│
└──────────────────────────────┘
```

⚠️ **A caixa explica porque é que a app pede a data de nascimento.** Pedir dados pessoais sem justificar é o que faz uma app parecer um funil de leads.

⚠️ **As idades saem dos dados, e não estavam a sair — dizia aqui «(75-83)»**, escrito à mão (corrigido a 2026-07-29, ao construir o ecrã). Vêm do `idade_maxima_fim` dos bancos escolhidos e mudam com a escolha. Medido contra o servidor a sério nesse dia: com os cinco bancos, a caixa diz **«70-80 anos»** — 70 da CGD, 80 do Santander. Um número escrito à mão numa frase fica errado no dia em que entra o sexto banco, e ninguém o vai lá corrigir.

⚠️ **O rendimento só se pergunta a quem o usa, e isso mudou o ecrã.** Medido nos cinco bancos: **um** declara `rendimento_mensal` com `usa: true` — o Novo Banco — e a nota dele diz que «o simulador exige-o, mas ele não mexe no preço: só entra na recomendação e no rácio de esforço». Quando nenhum dos bancos escolhidos o lê, o campo **não aparece** e a caixa diz porquê; no pedido vai zero, que é o valor inerte de um campo obrigatório no contrato e que ninguém daquela comparação lê. Perguntar o ordenado de uma pessoa para não fazer nada com ele é exactamente o funil que o parágrafo de cima recusa.

⚠️ **Mas a data de nascimento pergunta-se sempre, mesmo à CGD, que não a usa.** `usa: false` quer dizer «o simulador daquele banco não pergunta isto», e não «é irrelevante»: na CGD a idade entra à mesma no prazo máximo, que ela limita a terminar aos 70 anos. O que a app faz com o `usa: false` é mostrar a **nota do banco**, palavra por palavra como vem no contrato, debaixo do campo — que é a resposta exacta à pergunta que a pessoa faz quando lhe pedem a data de nascimento.

⚠️ **E a app não pré-julga nenhum banco por idade.** Seria fácil calcular aqui a idade no fim do crédito e marcar já os que a recusam, e estaria errado: a `KAN-34` está aberta precisamente porque o `idade_maxima_fim` é um número só e na CGD depende da finalidade. Quem decide que um banco não tem oferta é o servidor, que devolve a razão em português (§3). A app explica a regra; não a aplica.

### 1.3 A taxa e os bancos

```
┌──────────────────────────────┐
│ ●●●        A taxa e os bancos│
├──────────────────────────────┤
│  Tipo de taxa                │
│  [ Variável ][ Mista ][Fixa ]│
│                              │
│  Período fixo      5 anos    │
│  [2][3][4][5][10][15][20]    │
│   ↑ os cinzentos: nem todos  │
│     os bancos os têm         │
│                              │
│  Indexante        [ 6M ▾ ]   │
│  ⓘ CGD, Banco CTT e Santander│
│    impõem o seu — a escolha  │
│    aplica-se aos restantes.  │
│                              │
│  Bancos             5 de 5   │
│  ☑ CGD                       │
│      ☑ Packs                 │
│  ☑ Novo Banco                │
│      ☑ Primeiro Banco        │
│      ☐ Proteção              │
│  ☑ Montepio                  │
│  ☑ Banco CTT                 │
│  ☑ Santander                 │
│  ⓘ As bonificações mudam o   │
│    preço. Comparam-se as que │
│    estiverem marcadas.       │
│                              │
│  [ ← ]      [ Comparar (5) ] │
└──────────────────────────────┘
```

⚠️ **O tempo por banco saiu deste ecrã a 2026-07-28, e o campo que o alimentava saiu do contrato (KAN-32).** Estava aqui «☑ CGD ~2 s / ☐ Banco BPI ~50 s / Tempo estimado: ~6 s», tirado do campo `custo` de `GET /api/v1/bancos`, e a justificação era boa: «é honesto e faz o utilizador entender porque é que uma comparação demora 6 s e outra 52 s».

**Deixou de ser verdade.** Nenhum banco é interrogado no caminho do cliente desde a inversão da §1 — a resposta sai de uma consulta e de aritmética local, e custa o mesmo com um banco ou com dez. Anunciar «~2 s» era **avisar de uma espera que não existe**, e escolher bancos por causa dela era escolher pela razão errada. O `custo` continua no domínio (`Requisitos.Custo`), onde decide o prazo de cada banco **no varrimento** — que é onde o tempo ainda se paga.

⚠️ **O formulário adapta-se ao que os bancos escolhidos precisam** — período fixo só na mista, indexante só onde é escolhível, profissão só no grupo BCP. A fonte é sempre `GET /api/v1/bancos`, nunca uma lista escrita na app.

⚠️ **As bonificações entraram neste ecrã a 2026-07-29, e não estavam desenhadas em lado nenhum.** Aparecem recolhidas sob cada banco marcado, com as `por_omissao` já ligadas — que é o que o domínio manda: «quem escolhe é a pessoa; o `PorOmissao` diz à app o que pré-seleccionar, e mais nada». Deixá-las de fora não era neutro: com o `Pedido.Produtos` vazio a comparação sai **sem bonificação nenhuma**, e com elas ligadas de forma desigual sai invertida. Está medido — a 2026-07-26, no mesmo cenário, a CGD respondia com o preçário base e o Novo Banco com as duas já ligadas, e lado a lado isso dizia que o Novo Banco era 0,45 p.p. mais barato quando em pé de igualdade é a CGD a mais barata por 0,25 p.p.

⚠️ **E o indexante que ninguém pode escolher diz-se, em vez de se mostrar uma escolha morta.** Quando todos os bancos escolhidos impõem o seu tenor, o selector não aparece: `euribor_opcoes` vazio, no contrato, não é «não sei» — é «o banco impõe o seu e ignora a escolha». Oferecer três tenores a quem escolheu só CGD e Montepio era deixá-la escolher uma coisa que não chega a lado nenhum. ⚠️ Confirmado a 2026-07-29 contra o servidor a sério: a caixa diz **«Banco CTT, CGD e Santander impõem o seu indexante»**, exactamente os três que o mock previa.

---

## 2. ⚠️ O ecrã que saiu: «A comparar»

**Apagado a 2026-07-28.** O número fica vago de propósito, e o desenho fica aqui escrito, porque um ecrã que desaparece sem explicação volta a ser proposto — e este era bom para o problema que havia.

Era um ecrã próprio, com barra de progresso, cada banco a aparecer assim que chegasse com a TAEG já visível, e um `Cancelar` que propagava o cancelamento pelo `context` até aos bancos ainda a correr:

```
┌──────────────────────────────┐
│         A comparar…          │
│         ███████░░░  3 de 4   │
├──────────────────────────────┤
│  ✓ CGD           3,52 %  1,8s│
│  ✓ Novo Banco    3,25 %  0,9s│
│  ✓ Banco CTT     3,61 %  1,4s│
│  ◍ Montepio      …           │
│         [ Cancelar ]         │
└──────────────────────────────┘
```

**Estava certo, e o número que o justificava era real:** medido no v1, 7 dos 10 bancos prontos aos 7,4 s, 9 aos 25,9 s, o último aos 52,2 s. Uma barra única que só completasse no fim desperdiçava 45 s de informação já disponível.

⚠️ **O que mudou não foi o ecrã: foi o que está por baixo dele.** Com a inversão da §1 do `ARQUITETURA.md` (2026-07-25) uma comparação deixou de falar com bancos — lê a série varrida em hora morta e calcula localmente. Custa **uma consulta a Postgres e aritmética**. Já não há espera, logo não há progresso a mostrar, nem chegada progressiva, nem nada que faça sentido cancelar.

⚠️ **E se voltasse assim mesmo seria pior do que nada:** uma barra de progresso sobre uma consulta de milissegundos é teatro, e teatro num sítio onde se comparam créditos ensina a pessoa a desconfiar do resto do ecrã. O submeter do passo 3 vai direito às **Ofertas**.

**O que sobreviveu deste ecrã, e mudou de sítio:** os estados que não são o caminho feliz — servidor em baixo, sem rede, tecto por IP atingido, um banco sem série recente. Deixaram de ser um ecrã intermédio e passam a ser estados **das próprias Ofertas** (§3), que é onde a pessoa está quando eles acontecem.

⚠️ **O único progresso que a app mostra é o dos três passos do formulário** (2026-07-29). É contagem de passos e não espera de trabalho: a pessoa está no segundo de três, e isso é verdade sem depender de nada que esteja a correr.

---

## 3. Ofertas

```
┌──────────────────────────────┐
│  Ofertas          [ ⇅ TAEG ] │
│  250 000 € · 200 000 € · 30a │
│  mista 5 anos · Euribor 6M   │
├──────────────────────────────┤
│ ┌──────────────────────────┐ │
│ │ Novo Banco        ★ TAEG │ │
│ │ ~3,61 %                  │ │
│ │ 870,42 €/mês             │ │
│ │ TAN 3,25 · spread 0,90   │ │
│ │ 🏷 Primeiro Banco,       │ │
│ │    Proteção              │ │
│ └──────────────────────────┘ │
│ ┌──────────────────────────┐ │
│ │ Banco Montepio           │ │
│ │ ~3,74 %                  │ │
│ │ 891,10 €/mês             │ │
│ │ ⚠️ Simulado como mista — │ │
│ │    não tem taxa fixa     │ │
│ └──────────────────────────┘ │
│ ┌──────────────────────────┐ │
│ │ Banco CTT     ✕ sem      │ │
│ │                  oferta  │ │
│ │ O crédito terminaria aos │ │
│ │ 78 anos; exige até 75.   │ │
│ └──────────────────────────┘ │
│                              │
│  Preços de hoje, 05:00       │
│ [ Comparar seleccionadas ]   │
└──────────────────────────────┘
```

**Ordenação** por TAEG (omissão), prestação, TAN, spread ou MTIC. `★` marca a melhor de cada métrica.

⚠️ **O aviso de ajuste vive no cartão, não numa gaveta.** Sempre que `aplicado` não vem vazio, a nota aparece ali. É a regra de `API.md`: números diferentes dos pedidos sem o dizer, numa comparação de crédito, são enganadores.

⚠️ **Os bancos que falharam continuam na lista**, com a razão em português. Um banco que desaparece parece um esquecimento; um banco que explica porque não tem oferta é informação útil.

⚠️ **E hoje não continuam todos — é a `KAN-45`** (medido a 2026-07-29). O servidor devolve ofertas dos bancos que têm série, não dos que foram pedidos: com série só de dois, escolheram-se cinco no passo 3, o botão dizia «Comparar (5)», e a lista mostrou dois. Os outros três não deixaram rasto. A app mostra o que lhe derem e mostra bem o `sucesso: false` que já recebe; **a correcção é do servidor**, e enquanto não chegar esta regra está por cumprir.

⚠️ **Os produtos aplicados são visíveis em cada cartão.** Sem isso, um banco com descontos por omissão parece simplesmente mais barato. Foi a lição que o v1 demorou a aprender na sua própria série de mercado.

⚠️ **A TAEG leva marca de derivada, e não é opcional** (acrescentado a 2026-07-28). A `taeg` e o `mtic` não vêm cotados pelo banco: são calculados a partir dos encargos medidos, e cada oferta traz os `pressupostos` sob os quais o foram. No cartão basta a marca — um `~` antes do número, ou «TAEG estimada» — com os pressupostos inteiros no detalhe (§4). É a condição em que a §4 do `ARQUITETURA.md` permite servir o número, e a MCD exige-a junto do valor que dela depende (Anexo I, Parte II; Anexo II). **Um número derivado servido com ar de cotado é exactamente a falha que o v1 registou** e que este projecto herdou como regra: preferir falhar com clareza a servir um número inventado com ar de oficial.

⚠️ **E a idade do preço aparece, sempre.** Cada oferta traz o `capturado_em` do varrimento de que saiu, e a resposta traz o `calculado_em` dela própria. O rodapé da lista diz de quando são os preços — «preços de hoje, 05:00» — porque **um preço sem data não se serve**. Um preço de ontem continua a ser informação; um preço de ontem apresentado como o de agora, não.

⚠️ **Construído a 2026-07-29 (A5), e quatro coisas ficaram decididas aqui que não estavam desenhadas:**

⚠️ **O rodapé dá o `capturado_em` MAIS ANTIGO da lista, não o mais recente.** É uma afirmação sobre o conjunto de preços, e a afirmação verdadeira sobre preços de horas diferentes é a do mais velho. Dizer a hora do mais fresco apresentava os outros como sendo dessa hora — que é a mesma falha, à escala da lista, de servir um preço sem data.

⚠️ **Uma oferta ajustada não leva a estrela, e continua ordenada.** É a posição desta app sobre a `KAN-26` («ordenar uma oferta ajustada ao lado das outras compara coisas diferentes sem o dizer»). A estrela não é uma nota: é a afirmação «esta é a melhor», e dá-la a quem foi simulado a 35 anos quando se pediram 40 afirma uma coisa falsa sobre o pedido que a pessoa fez. A nota do ajuste continua no cartão; o que a oferta não leva é a marca. ⚠️ Um empate também não dá estrela a ninguém — marcar a primeira deixava a ordem de chegada decidir.

⚠️ **E uma oferta sozinha não é a melhor de nada** (acrescentado a 2026-07-29, a correr a app contra o servidor a sério). Com série de dois bancos e um deles sem o cenário pedido, a CGD ficava sozinha na lista e o cartão dela apanhava as **cinco** estrelas — melhor TAEG, melhor prestação, melhor TAN, melhor spread, melhor MTIC. Cada uma era verdadeira e o conjunto era falso: cinco estrelas empilhadas lêem-se como uma recomendação forte no sítio exacto onde não havia comparação nenhuma. Uma estrela só diz alguma coisa **contra outra oferta**. ⚠️ Isto não se via com dados fabricados — os testes tinham sempre duas ofertas com preço.

⚠️ **Uma oferta a que falte a métrica de ordenação vai para o fim das que têm preço, nunca para o meio.** `undefined` numa subtracção dá `NaN`, e um `sort` com `NaN` deixa a lista por ordem arbitrária — a pior avaria possível aqui, porque **parece ordenada**. A §4 do `ARQUITETURA.md` permite omitir uma TAEG que não se consegue dar, portanto o caso é legítimo e não hipotético.

⚠️ **E o que o servidor deixar por declarar diz-se.** Quando uma oferta traz `taeg` ou `mtic` sem `pressupostos`, o cartão nomeia-o como defeito **nosso** e não do banco — é o que o contrato manda («vazio com um deles preenchido é defeito nosso, e não um caso legítimo»).

---

## 4. Detalhe da oferta

```
┌──────────────────────────────┐
│ ←        Novo Banco          │
├──────────────────────────────┤
│  TAEG             ~3,61 %    │
│  Prestação      870,42 €     │
│  MTIC        313 351,20 €    │
│                              │
│  Como evolui                 │
│  ┌──────────────────────────┐│
│  │ ▁▁▁▁▁▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃  ││
│  │ 5 anos fixos │ variável  ││
│  └──────────────────────────┘│
│  Anos 1-5    3,25 %  870,42 €│
│  Anos 6-30   Eur+0,90  …     │
│                              │
│  Composição                  │
│  TAN 3,25 = Eur 6M 2,351     │
│             + spread 0,90    │
│                              │
│  Produtos aplicados          │
│  ☑ Primeiro Banco    −0,50   │
│  ☑ Proteção          −0,20   │
│                              │
│  Pressupostos da TAEG        │
│  • Derivada dos encargos     │
│    medidos, não cotada.      │
│  • O encargo recorrente      │
│    incide sobre o capital    │
│    em dívida.                │
│                              │
│  Notas                       │
│  • Arrendamento agrava o     │
│    spread em 0,50 p.p.       │
│                              │
│  Valor indicativo. Não é uma │
│  proposta. Fonte: simulador  │
│  público, 28/07 05:00.       │
└──────────────────────────────┘
```

⚠️ **O gráfico de fases é a informação que o v1 não dava bem.** Numa taxa mista, a TAN dos primeiros anos não é o custo do crédito — é o chamariz. Mostrar as fases lado a lado é a diferença entre comparar e ser induzido em erro.

⚠️ **A proveniência e a hora aparecem sempre.** Dizia aqui «um valor de cache com 4 horas tem de o dizer», e **não há cache** — morreu com a inversão da §1. O que há é melhor e mais exigente: **todos** os valores vêm de um varrimento anterior, não só alguns. A hora não é uma excepção a assinalar, é parte de cada oferta (`capturado_em`), e o rodapé desta página di-la sempre.

⚠️ **E os `pressupostos` mostram-se aqui por inteiro**, sob a TAEG e o MTIC. É o sítio onde a lista cabe, e é a condição em que a §4 do `ARQUITETURA.md` permite servir números derivados — «assumir e declarar», nunca uma sem a outra.

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
