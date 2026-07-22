# A app

Repositório separado: **`simulador-v2-app`**. Este documento vive aqui, no
backend, porque é aqui que estão as decisões que os dois lados partilham — o
contrato, a versionagem e a geração de tipos.

Os ecrãs estão em [`ECRAS.md`](ECRAS.md). Isto é o *como se constrói*.

## 1. Stack

| decisão | escolha | porquê |
|---|---|---|
| Plataforma | **Expo** (fluxo *managed*), com *dev builds* se aparecer módulo nativo | Um código para iOS, Android e **web**. E o **EAS Build compila para iOS sem Mac** — nesta máquina, que é Windows, isso não é conveniência, é viabilidade. |
| Navegação | **expo-router** | Ficheiros como rotas, e no alvo web dá **URLs a sério**. A versão web substitui o site público do v1, por isso ter `/ofertas` em vez de estado só em memória importa. |
| Estado do servidor | **TanStack Query** | O ciclo desta app é submeter e sondar. `refetchInterval` com paragem por condição é literalmente o caso central da biblioteca; e traz dedup, repetição e invalidação sem os escrevermos. |
| Estado do formulário | **Zustand**, uma loja só | Os três passos do pedido partilham estado e têm de sobreviver a navegar para trás. Limpa-se ao submeter. Não é estado de servidor, por isso não é do Query. |
| Estilos | **StyleSheet + módulo de tokens + `useTema()`** | Sem NativeWind nem biblioteca de componentes. O desenho já tem um sistema de tokens apertado (`ECRAS.md`), o `StyleSheet` comporta-se igual no web via `react-native-web`, e é menos uma peça a manter. |
| Testes | **Jest + React Native Testing Library**; Maestro para fluxos, mais tarde | |
| Distribuição | **EAS Build + EAS Submit**; web como estático | |

**Língua:** só português de Portugal. Sem biblioteca de i18n, mas ⚠️ **nenhuma
frase escrita dentro de um componente** — todas num módulo de textos. Acrescentar
uma língua passa a ser mecânico em vez de ser uma reescrita.

## 2. O contrato: como os tipos atravessam os repositórios

A fonte da verdade é `api/openapi.yaml`, **neste** repositório.

```
simulador-v2/api/openapi.yaml
        │
        ├── oapi-codegen ──────► tipos e handlers Go   (aqui)
        └── openapi-typescript ► api.d.ts              (na app, commitado)
```

Na app: `npm run sincronizar-api` vai buscar o esquema e regenera o `api.d.ts`,
que **fica commitado**. O CI da app corre o mesmo comando e reprova se o
resultado diferir do que está no repositório.

⚠️ **Os tipos gerados ficam commitados de propósito.** Um `.d.ts` que se busca em
tempo de build faz o build da app depender do backend estar de pé, e faz a app
mudar de comportamento sem ninguém mexer nela. Commitado, uma mudança de contrato
aparece como um diff que alguém tem de aprovar.

## 3. ⚠️ A restrição que uma app publicada impõe ao backend

Esta é a decisão da app que entra para trás no servidor, e é a mais importante
deste documento.

**Com uma app nas lojas não se controla quem actualiza.** Vai haver quem fique em
versões antigas durante meses. Portanto:

- **`/api/v1` só muda por acrescento.** Campos novos são opcionais; nunca se
  remove um campo nem se muda o tipo de um. Está em [`API.md`](API.md) §4 e passa
  a ter esta razão concreta por trás.
- **A app tolera campos que não conhece** e não rebenta com eles.
- **Um caminho de «esta versão é demasiado antiga»**, que o servidor possa
  accionar. Custa pouco agora e é impossível de acrescentar depois de haver
  versões antigas no terreno — que é precisamente quando faz falta.
- **Actualizações OTA (`expo-updates`)** para correcções de JavaScript sem passar
  pela loja. ⚠️ Não substituem o ponto anterior: uma actualização OTA só chega a
  quem abre a app.

## 4. Estrutura

```
app/                 rotas (expo-router)
  (pedido)/          os três passos do formulário
  comparar/[id].tsx  o ecrã de espera
  ofertas/           lista e detalhe
  bancos.tsx         o que cada banco precisa
componentes/
design/tokens.ts     as cores, o tipo, o espaçamento de ECRAS.md
design/tema.ts       claro e escuro
api/                 cliente + api.d.ts gerado + hooks do TanStack Query
estado/pedido.ts     a loja Zustand do formulário
textos/              todas as frases
```

## 5. Fases

Arranca **em paralelo com a fase 1 do backend**, assim que o `openapi.yaml`
existir (issue #6). Os tipos vêm do esquema, por isso a app constrói-se contra um
servidor falso antes de o servidor a sério responder.

| # | trabalho |
|---|---|
| A1 | Esqueleto Expo + expo-router + tokens e tema claro/escuro, com um ecrã a provar os dois |
| A2 | Geração e sincronização do `api.d.ts` + verificação no CI |
| A3 | Os três passos do pedido, com a loja Zustand e o formulário adaptativo alimentado por `GET /api/v1/bancos` |
| A4 | Ecrã de espera: sondagem com TanStack Query, resultados progressivos, cancelamento |
| A5 | Ofertas e detalhe, incluindo o gráfico de fases e as notas de ajuste |
| A6 | Estados que não são o caminho feliz: sem rede, servidor em baixo, tecto por IP atingido, todos os bancos falharam |
| A7 | Acessibilidade e alvo web (o que substitui o site do v1) |
| A8 | EAS Build e submissão — ⚠️ **bloqueado pelas perguntas de [`USO-RESPONSAVEL.md`](USO-RESPONSAVEL.md) §3** |

⚠️ **A6 não é polimento.** Uma app que só se testou com quatro bancos a
responderem bem é uma app que ninguém sabe como se comporta quando não
respondem — e eles não respondem com regularidade. Tem de estar coberto por
ecrãs desenhados, não por um alerta genérico.

## 6. O que a app não faz

- **Não guarda dados pessoais.** Vivem no estado do ecrã e desaparecem. Se um dia
  houver «guardar simulação», é armazenamento **local**; o servidor continua sem
  persistir nada (`ARQUITETURA.md` §4).
- **Não tem contas nem login.** Não existem e não vão existir.
- **Não tem analítica de terceiros** sem uma decisão explícita: os inputs desta
  app são data de nascimento e rendimento.
- **Não faz cálculos de crédito próprios.** Os números são dos bancos. A app
  apresenta e ordena; não estima, não extrapola, não preenche buracos.
