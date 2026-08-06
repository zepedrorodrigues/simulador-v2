# A app

Repositório separado: `simulador-v2-app`. Este documento vive aqui, no backend, porque é aqui que estão as decisões que os dois lados partilham — o contrato, a versionagem e a geração de tipos.

Os ecrãs estão em `ECRAS.md`. Isto é o *como se constrói*.

## 1. Stack

| decisão | escolha | porquê |
| --- | --- | --- |
| Plataforma | **Expo** (fluxo *managed*), com *dev builds* se aparecer módulo nativo | Um código para iOS, Android e **web**. E o **EAS Build compila para iOS sem Mac** — nesta máquina, que é Windows, isso não é conveniência, é viabilidade. |
| Navegação | **expo-router** | Ficheiros como rotas, e no alvo web dá **URLs a sério**. A versão web substitui o site público do v1, por isso ter `/ofertas` em vez de estado só em memória importa. |
| Estado do servidor | **TanStack Query** | ⚠️ **A justificação mudou a 2026-07-28, e a escolha aguentou.** Dizia aqui «o ciclo desta app é submeter e **sondar**», a sondagem morreu com a inversão da §1, e a §1 foi revertida a 2026-08-06. ⚠️ **A sondagem continua morta** — não há trabalho nosso a sondar —, mas volta o que a biblioteca faz melhor: **N pedidos em paralelo, um por banco**, com dedup, repetição com recuo, estados de erro tipados e resultados a chegar em alturas diferentes. É o caso de uso para que ela foi feita, e a escolha aguentou duas inversões. |
| Estado do formulário | **Zustand**, uma loja só | Os três passos do pedido partilham estado e têm de sobreviver a navegar para trás. Limpa-se ao submeter. Não é estado de servidor, por isso não é do Query. |
| Estilos | **StyleSheet + módulo de tokens + **`useTema()` | Sem NativeWind nem biblioteca de componentes. O desenho já tem um sistema de tokens apertado (`ECRAS.md`), o `StyleSheet` comporta-se igual no web via `react-native-web`, e é menos uma peça a manter. |
| Testes | **Jest + React Native Testing Library**; Maestro para fluxos, mais tarde |  |
| Distribuição | **EAS Build + EAS Submit**; web como estático |  |

**Língua:** só português de Portugal. Sem biblioteca de i18n, mas ⚠️ **nenhuma frase escrita dentro de um componente** — todas num módulo de textos. Acrescentar uma língua passa a ser mecânico em vez de ser uma reescrita.

## 2. O contrato: como os tipos atravessam os repositórios

A fonte da verdade é `api/openapi.yaml`, **neste** repositório.

```
simulador-v2/api/openapi.yaml
        │
        ├── oapi-codegen ──────► tipos e handlers Go   (aqui)
        └── openapi-typescript ► api.d.ts              (na app, commitado)
```

Na app: `npm run sincronizar-api` copia o `openapi.yaml` do repositório irmão para `contrato/openapi.yaml` e regenera dele o `api.d.ts`. **Os dois ficam commitados.** O CI da app regenera a partir da cópia e reprova se o resultado diferir do que está no repositório.

⚠️ **A cópia do esquema é commitada, e não só o `.d.ts` gerado** (decidido a 2026-07-28, ao construir a app). Sem ela, o CI da app não tinha do que gerar — o `openapi.yaml` vive noutro repositório, e um passo de CI que fosse buscá-lo por rede reintroduzia exactamente a dependência que o parágrafo seguinte proíbe. Com a cópia, uma mudança de contrato aparece **em dois diffs**: o do esquema, que se lê, e o dos tipos, que é consequência.

⚠️ **Os tipos gerados ficam commitados de propósito.** Um `.d.ts` que se busca em tempo de build faz o build da app depender do backend estar de pé, e faz a app mudar de comportamento sem ninguém mexer nela. Commitado, uma mudança de contrato aparece como um diff que alguém tem de aprovar.

⚠️ **E os tipos não substituem confrontar a app com uma resposta real** (2026-07-29). Os tipos garantem a **forma**; não garantem que a app leia bem o que lá está. O `ate_mes` é o exemplo: tipado como `integer`, e ser acumulado ou duração não se vê no tipo nenhum. Ver a nota da A5 na §5.

⚠️ **E os dois ficheiros exigem fins de linha estáveis** (2026-07-29). O repositório da app não tinha `.gitattributes`, e em Windows o checkout escrevia a cópia do esquema em CRLF enquanto a origem é LF: 29 371 bytes contra 28 531, exactamente uma a mais por linha. O `--verificar` comparava os dois e dizia «o contrato do backend mudou» sobre ficheiros idênticos. ⚠️ **O CI não o apanhava**, porque corre em Ubuntu — falhava só em Windows, e sempre.

## 3. ⚠️ A restrição que uma app publicada impõe ao backend

Esta é a decisão da app que entra para trás no servidor, e é a mais importante deste documento.

**Com uma app nas lojas não se controla quem actualiza.** Vai haver quem fique em versões antigas durante meses. Portanto:

- `/api/v1` só muda por acrescento. Campos novos são opcionais; nunca se remove um campo nem se muda o tipo de um. Está em `API.md` §4 e passa a ter esta razão concreta por trás.
- **A app tolera campos que não conhece** e não rebenta com eles.
- **Um caminho de «esta versão é demasiado antiga»**, que o servidor possa accionar. Custa pouco agora e é impossível de acrescentar depois de haver versões antigas no terreno — que é precisamente quando faz falta.
- **Actualizações OTA (**`expo-updates`) para correcções de JavaScript sem passar pela loja. ⚠️ Não substituem o ponto anterior: uma actualização OTA só chega a quem abre a app.

## 4. Estrutura

```
app/                 rotas (expo-router)
  (pedido)/          os três passos do formulário
  ofertas/           lista e detalhe
  bancos.tsx         o que cada banco precisa
componentes/         basicos, controlos, Passos, Estados, EcraDePasso,
                     CartaoDeOferta, GraficoDeFases
contrato/openapi.yaml  cópia sincronizada do esquema (ver §2)
design/tokens.ts     as cores, o tipo, o espaçamento de ECRAS.md
design/tema.ts       claro e escuro
dominio/             regras puras: formulário, ofertas, fases, formatação pt-PT
api/                 cliente + api.d.ts gerado + hooks do TanStack Query
estado/pedido.ts     a loja Zustand do formulário
estado/selecao.ts    junta a resposta da API com o que a pessoa escolheu
estado/ordenacao.ts  por que métrica a lista está ordenada
estado/comparacao.ts monta o pedido e serve a resposta à lista e ao detalhe
testes/              jest + a fábrica de bancos de teste
```

⚠️ **A comparação é servida por `useQuery` e não por `useMutation`** (2026-07-29). É um POST, mas não muda nada do outro lado — o contrato di-lo por palavras: nada do pedido é persistido, não há recurso a que voltar, e é por isso que nem existe `GET /api/v1/comparacoes/{id}`. O que isto é, na verdade, é uma **leitura cara com o pedido no corpo**. Servida como consulta, ganha a cache e o dedup que justificam a biblioteca (§1), e abrir uma oferta e voltar atrás não custa um pedido ao servidor.

⚠️ **E a ordenação da lista tem loja própria, e não um `useState` no ecrã.** No alvo web o «para trás» do browser devolve a pessoa do detalhe à lista: com estado do ecrã, ela tinha ordenado por prestação, foi ver uma oferta, e encontrava a lista por TAEG outra vez. Fica fora da loja do pedido porque não é o pedido — isto não viaja para o servidor, e misturá-lo com o que viaja era arriscar mandá-lo.

⚠️ **O `dominio/` entrou a 2026-07-29, com a A3, e não estava previsto aqui.** É onde vivem as regras puras do formulário — que períodos mostrar, que indexante é escolhível, que campos sequer perguntar, o LTV, a leitura de números e datas à portuguesa — e, com a A5, as das ofertas e das fases. Não importa React, nem o cliente HTTP, nem a loja, e é isso que permite testá-las sem montar um ecrã. O nome é o mesmo do backend pela mesma razão: é a parte que se parte em silêncio quando um banco muda o que aceita. ⚠️ **Não calcula crédito** — não há aqui prestação, TAEG nem spread; isso é do servidor (§6).

⚠️ **Havia aqui um `comparar/[id].tsx`, «o ecrã de espera», e saiu a 2026-07-28** — com a A4 e com o ecrã §2 do `ECRAS.md`. O `[id]` daquela rota era o identificador da simulação a sondar, e **nem esse identificador existe**: a resposta não tem `id`, porque não há trabalho em segundo plano a que voltar. Uma rota dinâmica sobre uma chave inexistente teria falhado no primeiro `expo-router build`.

## 5. Fases

| # | trabalho |
| --- | --- |
| A1 | ✅ Esqueleto Expo + expo-router + tokens e tema claro/escuro |
| A2 | ✅ Geração e sincronização do `api.d.ts` + verificação no CI |
| A3 | ✅ Os três passos do pedido, com a loja Zustand e o formulário adaptativo |
| A5 | ✅ Ofertas e detalhe, com o gráfico de fases, as notas de ajuste e os pressupostos — **confirmada contra o servidor a sério** |
| A6 | Estados que não são o caminho feliz: sem rede, servidor em baixo, tecto atingido, todos os bancos falharam |
| A7 | Acessibilidade e alvo web |
| A8 | EAS Build e submissão — ⚠️ **bloqueado pelas perguntas jurídicas da** `KAN-24` (`prioridade-alta`): termos de serviço dos bancos, redistribuição da série, regulação de crédito, requisitos das lojas, RGPD e responsabilidade |

⚠️ **A A4 VOLTA a 2026-08-06, com a §1 revertida — mas não é a A4 antiga.** Era «ecrã de espera: sondagem, resultados progressivos, cancelamento». Volta só o **meio**: resultados progressivos.

⚠️ **Não volta a sondagem nem o `comparar/[id].tsx`.** Não há `id` de simulação porque não há trabalho em segundo plano do nosso lado: são N pedidos, `GET /api/v1/ofertas/{banco}`, um por banco escolhido. A rota dinâmica sobre uma chave inexistente continuaria a falhar no primeiro build — e continua a não se escrever.

⚠️ **E traz trabalho novo que a A4 antiga não tinha: o fan-out passa a viver na app** (D2), e com ele a responsabilidade de não disparar dez pedidos de uma vez contra cinco bancos.

⚠️ **O alvo web não é polimento — é o primeiro alvo a publicar.** É o único que se aloja sem passar por uma loja, e as lojas estão bloqueadas pela `KAN-24`. A ordem é A1, A2, A3, A5, A6, A7; a A8 fica onde está.

⚠️ **A A6 também não é polimento.** Uma app que só se testou com bancos a responderem bem é uma app que ninguém sabe como se comporta quando não respondem — e eles não respondem com regularidade. Tem de ter ecrãs desenhados, não um alerta genérico.

## 6. O que a app não faz

- **Não guarda dados pessoais.** Vivem no estado do ecrã e desaparecem. Se um dia houver «guardar simulação», é armazenamento **local**; o servidor continua sem persistir nada (`ARQUITETURA.md` §4).
- **Não tem contas nem login.** Não existem e não vão existir.
- **Não tem analítica de terceiros** sem uma decisão explícita: os inputs desta app são data de nascimento e rendimento.
- **Não faz cálculos de crédito próprios.** Os números são dos bancos. A app apresenta e ordena; não estima, não extrapola, não preenche buracos.
