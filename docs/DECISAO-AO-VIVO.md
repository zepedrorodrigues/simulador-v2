# Proposta: o pedido do cliente volta a ir ao banco

**Estado: PROPOSTA, por avaliar.** Nada aqui está executado. Enquanto esta página
não for aceite, o `ARQUITETURA.md` continua a valer como está.

Reverte a inversão da §1 de **2026-07-25**, que tinha tirado os bancos do caminho
do cliente. O `PLAN.md` regista as três coisas que essa inversão matou —
`KAN-7` (fan-out no `comparar`), `KAN-8` (cache e *gate*) e `KAN-15`
(quantização do pedido). **As três voltam a estar vivas.**

---

## 1. Porque é que se reverte

Não é preferência de desenho. É o custo medido de manter uma **cópia do modelo de
preço de cada banco** do nosso lado.

O modelo actual não guarda respostas: guarda uma *reconstrução* da função de
preço — escala de LTV por fronteiras medidas, base da taxa fixa, encargos
ajustados, descontos por produto. Cada peça dessa reconstrução é uma hipótese
sobre como o banco preça, e **cada hipótese que os bancos contrariam é um número
errado servido com ar de certo**.

Só a 2026-08-06, num único dia de confronto com dados reais:

| o que se assumia | o que os bancos fazem | issue |
|---|---|---|
| a TAEG deriva-se de dois encargos (antecipado + recorrente) | o gap TAEG−TAN **sobe** com o prazo em 4 dos 5 bancos — nenhuma repartição não-negativa produz isso | `KAN-54` |
| a TAEG derivada serve, mesmo onde o banco publicou a dele | o banco publicou 4,500 % e servia-se 4,712 % | `KAN-55` |
| os descontos de produtos **somam-se** | Novo Banco: 0,500 + 0,200 dá **0,600**, e servia-se 0,10 p.p. mais barato do que ele cobra | `KAN-56` |
| um crédito tem um spread | o Santander pratica 0,5 p.p. nos primeiros 36 meses e 0,8 depois | `KAN-57` |

⚠️ **Nenhuma destas foi apanhada por um teste.** Todas apareceram por se
confrontar o servido com o medido, e a última só apareceu porque a `KAN-55`
recusou servir. O portão esteve verde o tempo todo.

E a lista não fecha: qualquer banco que amanhã mude a forma do preço — uma
promoção nova, um desconto que dependa do prazo, um escalão que dependa do
rendimento — parte o modelo outra vez, **em silêncio**. É o argumento que motiva
esta proposta: o modelo de dados não sobrevive a mudanças de taxas e de períodos.

### O argumento que a §7.4 já fazia, e que agora joga do outro lado

> «Ao vivo, um campo mal lido estraga **uma** resposta; aqui envenena **todas**
> até ao varrimento seguinte, e envenena-as com ar de certas.»

Está escrito no `ARQUITETURA.md` como razão para o varrimento medir o seu
resíduo. Lido ao contrário, é o melhor argumento a favor desta reversão: ao vivo,
o raio de um erro é um pedido.

---

## 2. O que desaparece

Isto não é simplificação cosmética. **Deixa de existir modelo de preço nosso.**

| morre | porquê |
|---|---|
| `dominio.Encargos` e o ajuste 2×2 | a TAEG vem do banco, para o pedido exacto |
| `dominio.EscalaDeLTV`, degraus, fronteiras medidas | o banco preça o LTV pedido |
| `base_fixa` e a correcção por degrau | idem |
| descontos por produto e a sua composição | pede-se com os produtos escolhidos |
| a sonda, `simulador sondar`, a tabela `sondagens`, a fiabilidade declarada | não há fotografia a envelhecer |
| `serie_desactualizada` e a viragem do dia | a Euribor do momento é a que o banco aplicou |
| `residuo_prestacao` como travão do modelo | continua útil como coerência da resposta, ver §5 |
| as 7 famílias da grelha, os ~96 pontos por banco | não há grelha a preencher |

Issues que deixam de fazer sentido: **`KAN-54`, `KAN-55`, `KAN-56`, `KAN-57`,
`KAN-41`, `KAN-34`, `KAN-38`** — todas descrevem o modelo, não os bancos.
⚠️ Ficam **canceladas com razão escrita**, não apagadas (`PLAN.md`).

⚠️ **Não foi isso que aconteceu ao tracker.** Verificado a 2026-08-06 à noite: as
`KAN-55`, `KAN-56` e `KAN-57` **foram apagadas** do `KAN`, e as `KAN-34`, `38` e
`41` fechadas como «Itens concluídos». O estado de cada uma está na tabela do
`RESUME.md`, e as chaves ficam citadas aqui na mesma — uma referência a uma
chave apagada diz mais do que o silêncio que sobrava se as removêssemos.

⚠️ **E é a razão de os números estarem escritos aqui em baixo, e não só na
issue.** As três linhas da tabela do §2 — 4,500 % contra 4,712 %, o
0,500 + 0,200 = 0,600 do Novo Banco, os 0,5/0,8 p.p. do Santander — são a
evidência inteira da reversão da §1. As issues que as continham já não existem;
este documento existe. **Um tracker é onde o trabalho se organiza, não onde a
prova mora.**

⚠️ **O `DOSSIE-BANCOS.md` sobrevive inteiro.** São 481 linhas de factos medidos
banco a banco — limites de idade, o que cada simulador pergunta, o que ignora,
esquisitices de payload. Nada disso é modelo nosso; é conhecimento sobre o
interlocutor, e é exactamente o que a fatia ao vivo precisa.

⚠️ **Os cinco parsers sobrevivem inteiros**, e passam a ser o activo principal do
repositório. A medição de fidelidade — **zero divergências em 147 098
comparações** — deixa de ser uma curiosidade e passa a ser a garantia de que o
que servimos é o que o banco disse.

---

## 3. O que passa a custar

Honestamente, e com os números que há.

### 3.1 Carga nos bancos: melhor ou pior, conforme o tráfego

Medido (`RESUME.md`): **1,00** pedido HTTP por simulação no Banco CTT, **2,03**
no Montepio, **4,00** no Santander. Uma comparação a cinco bancos custa, por
ordem de grandeza, **~10 pedidos**.

| modelo | custo diário |
|---|---|
| varrimento (hoje) | ~480 pedidos × 4 corridas = **~1 920/dia**, independente do tráfego |
| ao vivo | ~10 por comparação |

⚠️ **O ponto de cruzamento é ~190 comparações por dia.** Abaixo disso, ir ao vivo
carrega **menos** os bancos do que varrer. Acima, carrega mais — e cresce sem
tecto com o tráfego.

Isto muda a natureza do problema: a carga deixa de ser uma constante que
escolhemos e passa a ser **proporcional ao uso**, o que a torna um risco de
produto e não de agendamento.

### 3.2 Latência: passa a existir, e é a do banco mais lento

Não há resposta imediata. Uma comparação espera pelo *fan-out*, e o tempo é o do
banco mais lento.

⚠️ **Medido a 2026-08-06:** o Montepio **não respondeu dentro de 10 s** em 4 dos
seus cenários, num varrimento em hora de expediente. Com o modelo actual isso
custou 4 buracos numa grelha; ao vivo custa 10 segundos de espera a um cliente,
ou uma oferta a menos.

A `A4` da app — «o ecrã de espera com sondagem» — foi apagada do plano com a nota
«não há espera nenhuma». **Volta a ser precisa.**

### 3.3 Passamos a ser um amplificador

Um pedido nosso vira ~10 pedidos aos bancos. Sem defesa, qualquer pessoa com
`curl` transforma o nosso endpoint numa ferramenta de carga contra cinco bancos
portugueses, e **a origem aparente é nossa**.

⚠️ O tecto por IP deixa de ser boa educação e passa a ser **estrutural**. E o
`PROXIES_DE_CONFIANCA` por medir (`KAN-14`, Fase 4) passa de dívida a
**bloqueante**: sem ele, ou o tecto é contornável, ou é o tecto do site inteiro.

### 3.4 A cache volta, e com ela a pergunta que a matou

Com pedidos ao vivo, uma cache de curta duração é a única defesa que reduz carga
sem reduzir funcionalidade. É o `KAN-8`, que a inversão de Julho matou.

⚠️ **E ressuscita o `KAN-15`**, a quantização do pedido — arredondar o montante
para aumentar acertos. O `ARQUITETURA.md` guarda a razão pela qual ela era
perigosa: «se o arredondamento cruzar um degrau de LTV, dar a banda errada é pior
do que perder o acerto». Essa razão **volta a valer por inteiro**, e agora sem a
protecção estrutural que a grelha dava.

**Recomendação:** cache sim, quantização **não**. Chave = o pedido exacto.

---

## 4. As decisões, tomadas a 2026-08-06

### D1 — O varrimento **morre por completo**

Não sobrevive nem em versão reduzida. Não há série de mercado.

⚠️ **Consequência que passa a ser trabalho, e não detalhe:** o
`/api/rate-catalog` fica sem fonte de dados. Ele está **congelado ao byte** e é
**consumido pelo `viabilidade-imobiliaria` em produção**, com teste de contrato
(`KAN-42`). Matar o varrimento sem mais parte um consumidor a sério.

Isto deixa de ser uma limpeza interna e passa a ser uma **retirada coordenada
entre dois repositórios**. As saídas, por ordem de preferência:

1. **Congelar a última fotografia.** O `/api/rate-catalog` continua a responder
   com o último varrimento gravado, que deixa de envelhecer. ⚠️ Uma série que não
   avança é uma série morta, e o consumidor tem de o saber — não pode descobri-lo
   por os números pararem.
2. **Retirar com aviso**, depois de o `viabilidade-imobiliaria` deixar de
   depender dela. É a única saída limpa, e o trabalho é do outro lado.
3. **Manter um varrimento mínimo só para ela.** Contraria a D1 e fica registada
   por ser a alternativa óbvia que foi recusada.

**Por decidir qual**, e é a primeira coisa a fechar — bloqueia a §4 e a §6.

⚠️ E morre com o varrimento tudo o que dependia dele: o ecrã de mercado
(`KAN-23`, Fase 5) e a medição passiva do *market-diff*.

### D2 — A resposta vai **por banco, à medida que chega**

O cliente não espera pelo banco mais lento. O primeiro preço aparece assim que o
primeiro banco responder.

**Mecanismo proposto: um pedido por banco a partir da app**, e não SSE.

| | |
|---|---|
| SSE de `/api/v1/comparacoes` | um pedido do cliente, servidor mantém a ligação aberta. ⚠️ O `fetch` do React Native **não** suporta SSE nativamente, e ligações longas atravessam mal proxies |
| **um pedido por banco** | `POST /api/v1/ofertas/{banco}`. HTTP simples, cada resposta pequena, retry por banco, a app controla a concorrência e mostra o que já tem |

⚠️ **O custo desta escolha é que o *fan-out* passa para a app**, e com ele a
responsabilidade de não disparar dez pedidos de uma vez. E o tecto por IP passa a
contar N pedidos por comparação em vez de um — tem de ser dimensionado para isso,
ou tranca um utilizador normal.

**Isto é escolha minha dentro da D2, e é o ponto mais discutível desta página.**

### D3 — Um banco que não responde **sai em falta, nomeado**

`banco_indisponivel`, que já existe. Não se recorre a preço antigo: servir um
preço velho ao lado de quatro frescos, na mesma lista, é o modo de falha que este
projecto passou o dia a corrigir. ⚠️ E com a D1 nem sequer haveria preço velho
que servisse.

### ⛔ D4 — retirada a 2026-08-11

Era «Legal, por decidir»: um pedido por cliente, com os dados que ele
introduziu, é uma relação diferente com os simuladores dos bancos do que uma
recolha periódica. **Saiu por decisão do dono do projecto**, com a issue que a
acompanhava apagada e as referências retiradas dos documentos.

⚠️ **Fica escrita como retirada e não apagada**, que é a regra deste ficheiro
para o resto: uma decisão que desaparece sem rasto é uma decisão que alguém
volta a propor daqui a um mês sem saber que já foi tomada.

---

## 5. O que muda em cada documento

Depois de D1–D4 decididas.

| documento | mudança |
|---|---|
| `ARQUITETURA.md` §1 | reescrita: as «duas coisas» passam a ser *responder ao vivo* e *publicar a série de mercado* |
| `ARQUITETURA.md` §4 | `catalogo_taxas` deixa de ser a fonte da resposta e passa a ser só a série. Caem `ltv_min`/`ltv_max`/`spread_minimo`/`base_fixa`; cai a tabela `sondagens` |
| `ARQUITETURA.md` §7 | reescrita completa: deixa de ser «o varrimento e o que substitui a cache» e passa a ser **a fatia ao vivo** — travão por banco, timeouts, cache, tecto |
| `ARQUITETURA.md` §5 | o `Banco` deixa de alimentar uma grelha e passa a servir um pedido; a interface quase não muda, o que muda é quem a chama |
| `API.md` | `/api/v1/comparacoes` ganha semântica de espera e falha parcial |
| `CLAUDE.md` | os alvos de rede mudam de sentido: `teste-rede` passa a ser o caminho do cliente |
| `PLAN.md` | fases 2, 3 e 5 mudam de âmbito; `KAN-7`/`8`/`15` deixam de estar cancelados |
| `ECRAS.md` / `APP.md` | volta a `A4` — o ecrã de espera |
| `DOSSIE-BANCOS.md` | **intacto** |
| `CONTRATO-BANCO.md` | quase intacto: capturar → parser → payload → rede continua igual |

---

## 6. O que **não** muda, e é bom que não mude

- Os cinco parsers e as capturas.
- A regra de dependência (`dominio ← bancos ← aplicacao ← infra ← cmd`).
- O portão, a verificação por reversão, o pt-PT, o `-race`.
- Nada de estado em-processo: o travão por banco continua em Postgres, e ao vivo
  passa a ser **mais** necessário, não menos.
- A app, o contrato sincronizado, e a regra de `/api/v1` só mudar por acrescento.

---

## 7. O risco desta proposta, dito em voz alta

O v1 **era** ao vivo, e foi reescrito. ⚠️ **Convém não reescrever o v1 outra
vez.** O que correu mal no v1 — estado em-processo, autenticação acrescentada sem
decisão, 50+ ficheiros ad-hoc, 338 erros de lint permanentes, 4-8 GB de imagem —
**não vinha de ser ao vivo**. Vinha de não haver portão nem fronteiras, e essas
partes do v2 ficam de pé.

O que esta proposta admite é mais simples: **a parte do v1 que estava certa era
perguntar ao banco.** A parte que estava errada era tudo o resto.
