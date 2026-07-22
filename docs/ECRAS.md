# Ecrãs da app

Especificação da app **React Native** (repositório separado). Este documento
existe neste repositório porque define o que a API tem de servir e em que
momento — ver [`API.md`](API.md).

## O problema que isto resolve

A issue #2 do v1, escrita pelo próprio autor:

> «Currently all ui is a single window with form for request and response —
> visually too much information + disformatting + just crap»

O v1 tinha um formulário de ~25 campos e a tabela de resultados na mesma página,
num ficheiro de 874 linhas. **A regra do v2: um ecrã, uma pergunta.** O
utilizador nunca vê ao mesmo tempo o que está a pedir e o que já recebeu.

## Mapa

```
  Pedido (3 passos)  ─►  A comparar  ─►  Ofertas  ─►  Detalhe da oferta
        │                                   │
        └──► Bancos: o que cada um precisa  └──► Comparar duas lado a lado
```

E, à parte, `Mercado` — a série do `rate-catalog`. Fica para depois de a app
funcionar (fase 5), mas o desenho já a prevê no separador inferior.

---

## 1. Pedido — três passos

Um passo por ecrã, com barra de progresso. Nada de acordeões nem de secções
recolhidas: no telemóvel, um formulário de 25 campos numa página é ilegível.

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
│  ──────●───────────────────  │
│  LTV 80 %   ✓ dentro dos     │
│             limites de todos │
│                              │
│  Prazo            30 anos    │
│  ──────────●───────────────  │
│                              │
│  Finalidade                  │
│  [ Habitação própria ▾ ]     │
│                              │
│            [ Seguinte → ]    │
└──────────────────────────────┘
```

⚠️ **O LTV é calculado e mostrado ao vivo, com a leitura do que significa.** O
spread é uma função em degraus de 5 % de LTV; um utilizador a 80,4 % beneficia
de saber que baixar 1 000 € o põe noutra banda. O v1 tinha um campo `LTV`
desactivado, sem explicação.

### 1.2 Os titulares

```
┌──────────────────────────────┐
│ ●●○           Os titulares   │
├──────────────────────────────┤
│  1.º titular                 │
│  Nascimento   [ 12/04/1990 ] │
│  Rendimento   [    2 200 € ] │
│                              │
│  [ + Acrescentar 2.º titular]│
│                              │
│  ┌ ⓘ ───────────────────────┐│
│  │ A idade define o prazo   ││
│  │ máximo: cada banco exige ││
│  │ que o crédito termine    ││
│  │ até certa idade (75-83). ││
│  └──────────────────────────┘│
│         [ ← ]  [ Seguinte → ]│
└──────────────────────────────┘
```

⚠️ **A caixa explica porque é que a app pede a data de nascimento.** Pedir dados
pessoais sem justificar é o que faz uma app parecer um funil de leads.

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
│  Bancos            4 de 10   │
│  ☑ CGD              ~2 s     │
│  ☑ Novo Banco       ~2 s     │
│  ☑ Montepio         ~2 s     │
│  ☑ Banco CTT        ~2 s     │
│  ☐ Banco BPI       ~50 s ⚠️  │
│                              │
│  Tempo estimado: ~6 s        │
│                              │
│  [ ← ]      [ Comparar (4) ] │
└──────────────────────────────┘
```

⚠️ **O custo de cada banco é visível antes de escolher**, do campo `custo` de
`GET /api/v1/bancos`. É honesto e faz o utilizador entender porque é que uma
comparação demora 6 s e outra 52 s. O v1 não dizia nada e a espera parecia avaria.

⚠️ **O formulário adapta-se ao que os bancos escolhidos precisam** — período fixo
só na mista, indexante só onde é escolhível, profissão só no grupo BCP. A fonte é
sempre `GET /api/v1/bancos`, nunca uma lista escrita na app.

---

## 2. A comparar

Ecrã próprio. Não é um *spinner* por cima do formulário.

```
┌──────────────────────────────┐
│         A comparar…          │
│         ███████░░░  3 de 4   │
├──────────────────────────────┤
│  ✓ CGD           3,52 %  1,8s│
│  ✓ Novo Banco    3,25 %  0,9s│
│  ✓ Banco CTT     3,61 %  1,4s│
│  ◍ Montepio      …           │
│                              │
│         [ Cancelar ]         │
└──────────────────────────────┘
```

⚠️ **Cada banco aparece assim que chega, com a TAEG já visível.** Medido no v1: 7
dos 10 bancos prontos aos 7,4 s, 9 aos 25,9 s, o último aos 52,2 s. Uma barra
única que só completa no fim desperdiça 45 s de informação já disponível.

⚠️ **`Cancelar` cancela mesmo** — propaga o cancelamento pelo `context` até aos
bancos ainda a correr. Um utilizador que sai do ecrã não deve deixar quatro
scrapes a correr contra os bancos a partir do nosso IP.

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
│ │ 3,61 %                   │ │
│ │ 870,42 €/mês             │ │
│ │ TAN 3,25 · spread 0,90   │ │
│ │ 🏷 Primeiro Banco,       │ │
│ │    Proteção              │ │
│ └──────────────────────────┘ │
│ ┌──────────────────────────┐ │
│ │ Banco Montepio           │ │
│ │ 3,74 %                   │ │
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
│ [ Comparar seleccionadas ]   │
└──────────────────────────────┘
```

**Ordenação** por TAEG (omissão), prestação, TAN, spread ou MTIC. `★` marca a
melhor de cada métrica.

⚠️ **O aviso de ajuste vive no cartão, não numa gaveta.** Sempre que `aplicado`
não vem vazio, a nota aparece ali. É a regra de `API.md`: números diferentes dos
pedidos sem o dizer, numa comparação de crédito, são enganadores.

⚠️ **Os bancos que falharam continuam na lista**, com a razão em português. Um
banco que desaparece parece um esquecimento; um banco que explica porque não tem
oferta é informação útil.

⚠️ **Os produtos aplicados são visíveis em cada cartão.** Sem isso, um banco com
descontos por omissão parece simplesmente mais barato. Foi a lição que o v1
demorou a aprender na sua própria série de mercado.

---

## 4. Detalhe da oferta

```
┌──────────────────────────────┐
│ ←        Novo Banco          │
├──────────────────────────────┤
│  TAEG              3,61 %    │
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
│  Notas                       │
│  • Arrendamento agrava o     │
│    spread em 0,50 p.p.       │
│                              │
│  Valor indicativo. Não é uma │
│  proposta. Fonte: simulador  │
│  público, 22/07 09:14.       │
└──────────────────────────────┘
```

⚠️ **O gráfico de fases é a informação que o v1 não dava bem.** Numa taxa mista, a
TAN dos primeiros anos não é o custo do crédito — é o chamariz. Mostrar as fases
lado a lado é a diferença entre comparar e ser induzido em erro.

⚠️ **A proveniência e a hora aparecem sempre.** Um valor de cache com 4 horas tem
de o dizer.

---

## 5. Bancos: o que cada um precisa

Ecrã de consulta, alimentado por `GET /api/v1/bancos`. Responde às perguntas que
no v1 viviam num painel apertado ao fundo da página: que campos é que este banco
usa mesmo, que períodos fixos aceita, que indexante impõe, que produtos tem, até
que idade financia.

---

## Regras transversais

- **Português de Portugal em toda a interface.** Sem gerúndio brasileiro.
- **Nunca um número sem unidade nem contexto.** `3,61 %` é TAEG ou TAN? Rotular.
- **Modo claro e escuro** desde o início.
- **Acessibilidade**: alvos de toque ≥ 44 pt, contraste AA, rótulos para leitor
  de ecrã em todos os campos. Uma app financeira lida com pessoas de todas as
  idades.
- ⚠️ **A app não guarda dados pessoais.** Os inputs vivem no estado do ecrã e
  desaparecem. Se um dia houver «guardar simulação», é armazenamento **local** no
  dispositivo — o servidor continua sem guardar nada (`ARQUITETURA.md` §4).
- ⚠️ **Isto não é aconselhamento financeiro, e a app diz isso onde é preciso** —
  não escondido nas definições. Os valores são indicativos, de simuladores
  públicos, sem análise de risco personalizada.
