# Análise técnica — KAN-35: a resolução do LTV na grelha

**2026-07-26.** Feita com dois bancos medidos (CGD, KAN-9; Novo Banco, KAN-10) e
com uma medição nova, feita para esta análise, que decide a questão.

⚠️ **Isto é análise, não é decisão executada.** O que ela recomenda implica
alterar a **§4 do `ARQUITETURA.md` primeiro, com justificação** — regra do
`CLAUDE.md` — e só depois tocar em código.

⚠️ **Espelhada no Confluence a 2026-07-27 (KAN-40).** Vivia numa cópia única, no
disco, e a §4 do `ARQUITETURA.md` — que é vinculativa — remete para ela. Uma
referência a um ficheiro que um `git clean -xdf` apaga sem recurso parece
verificável e não é.

---

## 1. O estado actual, levantado no repositório

**O que a §4 diz hoje.** Na tabela «o que é consulta e o que é cálculo», a
primeira linha é `spread, por banda de LTV`. E o `cenario` de `catalogo_taxas` é
descrito como «chave **estruturada** do ponto da grelha: banda de LTV, tipo de
taxa, período fixo e finalidade», com o formato exacto a «fixar-se ao escrever a
CGD».

**O que o código tem.** `internal/dominio/politicas.go`:

```go
func BandaLTV(r Racio) int   // vinte degraus de 5 %: ceil(ltv × 20) × 5
func MesmaBanda(a, b Racio) bool
```

⚠️ **Nenhuma das duas tem um único chamador em produção.** Só aparecem nos seus
próprios testes (`politicas_test.go`, incluindo um `FuzzBandaLTV`) e num
`t.Logf` do E2E da CGD. Confirmado por varrimento do repositório inteiro.

E o `cenario` **nunca chegou a ser fixado**: o `varrimento.Ponto` transporta-o
como `string` opaca, com o comentário a dizer que «o formato exacto fixa-se ao
escrever a CGD» — o que não aconteceu, porque o E2E usa rótulos à mão
(`"variavel/ltv66/30a"`).

**Consequência prática, e é a que muda o custo desta decisão:** o modelo de
bandas de 5 % **ainda não está cimentado em lado nenhum**. Não há migração a
escrever, não há dados a converter, não há chamador a corrigir. Muda-se o
documento, muda-se uma função do domínio, e não há mais nada agarrado.

Nota de arrumação: o `MesmaBanda` foi escrito para a KAN-15 (quantização do
pedido com guarda de degrau), que está **Concluída** e cuja razão de ser — a
cache — a §4 declara extinta («A cache. ⚠️ Não existe»). É código morto por duas
razões independentes.

---

## 2. Os critérios, enunciados antes das opções

1. **Correcção do modelo** — o erro máximo em p.p. que a representação pode
   introduzir, medido e não estimado.
2. **O defeito falha alto?** — quando a representação não chega, isso aparece a
   quem lê, ou serve-se um número errado com ar de certo? É a §7.4: aqui um
   campo mal lido «envenena todas as respostas até ao varrimento seguinte».
3. **Custo de varrimento** — pedidos por banco por corrida. É o que se pede aos
   bancos, e é a única coisa nesta análise que tem um custo externo.
4. **Tamanho da grelha** — linhas por banco em `catalogo_taxas`.
5. **Esforço** — horas de sessão, honestamente.

---

## 3. A medição que decide: há estrutura abaixo de 1 p.p.?

Feita a 2026-07-26 para esta análise. Imóvel fixo em **400 000 €** e montante a
variar de 1 000 € — assim cada passo é exactamente **0,25 p.p.** de LTV, sem
arredondamentos a sujar a medição. Taxa variável, 30 anos, habitação própria.

### CGD, à volta da anomalia dos 67 %

| LTV | 65,00 – 66,50 | 66,75 – 67,75 | 68,00 – 69,00 |
| --- | --- | --- | --- |
| spread | 2,000 | **2,050** | 1,350 |

### CGD, à volta da fronteira que o dossiê deixara por mapear

| LTV | 32,00 – 33,00 | 33,50 – 36,00 |
| --- | --- | --- |
| spread | 1,950 | 2,000 |

### Novo Banco

| LTV | 49,50 – 50,00 | 50,25 – 51,50 |  | 79,50 – 80,00 | 80,25 – 81,50 |
| --- | --- | --- | --- | --- | --- |
| spread | 0,75 | 0,80 |  | 0,90 | 0,95 |

### O que isto prova

**Três fronteiras da CGD e do Novo Banco caem em intervalos que não contêm
nenhum número inteiro de LTV:**

| fronteira | está em | contém inteiro? |
| --- | --- | --- |
| CGD 2,000 → 2,050 | (66,50 ; 66,75] | **não** |
| CGD 1,950 → 2,000 | (33,00 ; 33,50] | **não** |
| CGD 2,050 → 1,350 | (67,75 ; 68,00] | sim — 68 |
| Novo Banco 0,75 → 0,80 | (50,00 ; 50,25] | não |
| Novo Banco 0,90 → 0,95 | (80,00 ; 80,25] | não |

⚠️ **As duas primeiras são prova de que uma grelha de 1 p.p. tem o mesmo defeito
da de 5 p.p.**, só que mais pequeno. Não é uma questão de afinar o passo: as
fronteiras da CGD não estão alinhadas com passo nenhum que se escolha à cabeça.

⚠️ **As do Novo Banco, ao contrário, são exactamente o que o `BandaLTV` já
faz.** Ele fecha a banda em cima (`ceil`), por isso põe 50,00 na banda 50 e
50,25 na banda 55 — e é aí que o preço muda. O Novo Banco é consistente com a
regra «LTV até 50 %», «até 70 %», «até 80 %». **Os dois bancos discordam sobre a
forma da coisa, e é por isso que um banco não chegava para decidir.**

### E a segunda armadilha: o patamar estreito

O `2,050` da CGD ocupa cerca de **1,25 p.p.** — de ~66,6 % a ~67,9 %. Não é uma
fronteira, é um patamar isolado entre dois mais largos, e o preço **não é
monótono** no LTV (sobe e volta a descer).

Isto elimina uma família inteira de soluções: **qualquer descoberta de fronteiras
por bissecção que assuma monotonia, ou poucos degraus, salta este patamar.** Ele
foi encontrado por amostragem uniforme, e é assim que se encontra outro como ele.

---

## 4. O erro de cada resolução, com os números

A conta que interessa não é intuitiva, e é o coração desta análise.

**Afinar a grelha não reduz o erro. Reduz a exposição.** Numa grelha de passo
fixo, a banda que contém uma fronteira serve um só spread aos dois lados dela. O
erro de quem cai do lado errado é **a altura do degrau**, seja qual for o passo:

| resolução | bandas partidas (CGD) | largura exposta | erro de quem lá cai | linhas por banco |
| --- | --- | --- | --- | --- |
| 5 p.p. (hoje) | banda 70 | 5 p.p. | **0,70 p.p.** | 20 |
| 1 p.p. | bandas 67 e 68 | 2 p.p. | **0,70 p.p.** | ~71 |
| 0,25 p.p. | 2 bandas | 0,5 p.p. | **0,70 p.p.** | ~280 |
| intervalos medidos | nenhuma | 0 | **0** | **~4** |

Os 0,70 p.p. valem cerca de **79 € por mês** num empréstimo de 200 000 € a 30
anos — mais do que a diferença de preço entre bancos que este projecto existe
para comparar.

⚠️ **E a última coluna é a surpresa: a representação exacta é a mais pequena.**
Guardar a função em degraus que o banco pratica são ~4 linhas por banco; guardar
uma amostragem dela são 20, 71 ou 280. A grelha por intervalos é
simultaneamente **exacta e menor** — porque guarda o que o preço é, em vez de
uma fotografia dele em pontos escolhidos por nós.

O custo desloca-se todo para o **varrimento**, que passa a ter de descobrir as
fronteiras em vez de as assumir.

---

## 5. As opções

### A — não fazer nada, ficar nos 5 p.p.

Defensável só se a resposta ao cliente nunca vier a depender do spread por LTV.
Não é o caso: a §4 diz que **toda** a resposta sai da grelha.

**Errada quando:** um cliente com LTV 66 % receber o preço de um de 68 %. Que é
hoje, para qualquer LTV entre 65 % e 70 % na CGD.

### B — afinar para 1 p.p.

Reduz a exposição de 5 p.p. para 2 p.p., **e não reduz o erro** (continua em
0,70 p.p.). Multiplica a grelha por 3,5 e as fronteiras medidas da CGD continuam
por representar. Compra pouco e paga em tabela.

**Errada quando:** se acredita que comprou correcção — comprou probabilidade.

### C — descobrir as fronteiras por bissecção e guardar intervalos

Exacta e com a grelha mais pequena. **Mas** a bissecção pura salta o patamar de
1,25 p.p. da CGD, que é precisamente o achado que abriu esta issue.

**Errada quando:** o preço não é monótono — que é o caso medido.

### D — amostragem uniforme a 1 p.p. **mais** refinamento onde há desacordo (recomendada)

Duas fases no varrimento:

1. **varrer** o domínio de LTV a 1 p.p. — apanha patamares até 1 p.p. de largura,
   incluindo o das 2,050;
2. **refinar por bissecção só entre pontos vizinhos que discordem**, até o
   intervalo ficar abaixo de uma tolerância (0,05 p.p. chega — são 4 níveis).

Guarda-se o resultado como **intervalos** `[ltv_min, ltv_max] → spread`, com as
fronteiras medidas, e não bandas de passo fixo.

⚠️ **E o remate que torna o defeito visível em vez de silencioso:** quando o
refinamento pára com o intervalo ainda por resolver (rede em baixo, banco a
mudar de preçário a meio), esse intervalo fica marcado como **não resolvido**, e
a reconstrução não escolhe um dos lados em silêncio.

⚠️ **O que se faz nesse caso ficou decidido na §9, e não é recusar:** serve-se o
**spread mais alto do intervalo**, com nota obrigatória a dizer que é um limite
superior. É o que o Anexo I da MCD prescreve para «vários valores possíveis», e é
mais útil do que recusar sem deixar de ser honesto.

**Errada quando:** existir um patamar mais estreito do que 1 p.p. que ninguém
procurou. Mitiga-se: o varrimento seguinte, com a grelha já em intervalos, pode
amostrar dentro dos intervalos largos a custo baixo e detectar se algum se parte.

---

## 6. Custo do varrimento, medido

Com os tempos por simulação já medidos — CGD **603 ms** com dois trabalhadores,
Novo Banco **230 ms**:

| | pedidos por banco | CGD | Novo Banco |
| --- | --- | --- | --- |
| 5 p.p. (hoje) | 20 | 12 s | 5 s |
| **opção D** (71 + ~25 de refinamento) | ~96 | **~58 s** | **~22 s** |

⚠️ Isto é a dimensão do LTV **e mais nada**, e não multiplica com as outras:
está medido, nos dois bancos, que **o spread não depende do tipo de taxa** — na
CGD dá 1,350 na variável, na mista e na fixa a LTV 80 %; no Novo Banco dá 0,900
nas três. A dimensão do LTV soma-se às outras em vez de as multiplicar, que é o
que mantém a grelha nas «dezenas de pontos por banco» que a §4 exige.

Um minuto por banco, uma vez por varrimento, em hora morta. Não é caro.

⚠️ **Nota de 2026-07-27, com o terceiro banco escrito:** o Montepio custa
**1,721 s** por simulação — sete vezes o Novo Banco, porque cada simulação paga o
arranque que fixa os cookies. Pelas mesmas contas, a opção D custa-lhe **~165 s**.
Continua a não ser caro em hora morta, mas é a ordem de grandeza a ter em conta
na KAN-16. E o Montepio traz um terceiro comportamento de LTV: **o preço não muda
de todo**, de 50 % a 100 % — logo, um degrau só. Os três bancos dão três formas
diferentes, o que reforça a conclusão da §7.1: a resolução é medida por banco, e
não é uma constante do domínio.

---

## 7. Recomendação

**Opção D.** E, em concreto:

1. **A §4 muda primeiro.** A linha `spread, por banda de LTV` passa a
   `spread, por intervalo de LTV com fronteiras medidas`, com o parágrafo de
   justificação a dizer o que se mediu: que a CGD tem fronteiras em LTV não
   inteiro, que tem um patamar não monótono de 1,25 p.p., e que o Novo Banco cai
   certinho nos múltiplos de 5 — logo, **a resolução não pode ser uma constante
   do domínio, tem de ser medida por banco**.
2. **`BandaLTV` e `MesmaBanda` saem do domínio.** Não se «corrigem» para 1 p.p.:
   o que está errado não é a constante, é a ideia de que existe uma banda fixa.
   Entra no lugar um tipo que representa a função em degraus.
3. **A chave `cenario` deixa de ter banda de LTV.** Passa a identificar o
   intervalo pelo seu limite inferior medido, ou — melhor — o LTV deixa de ser
   parte da chave e passa a ser duas colunas (`ltv_min`, `ltv_max`), o que é mais
   honesto e indexável. ⚠️ Isto é uma alteração à tabela de §4 e tem de ser
   decidida lá, não aqui.
4. **Dentro de intervalo não resolvido serve-se o lado mais caro, com nota
   obrigatória** (§9, Decisão 1) — é o que a torna diferente de uma aproximação
   com outro nome, e é o que o Anexo I da MCD manda.

### Porque não a B, que era a resposta óbvia

Porque a medição diz que não funciona. Duas das cinco fronteiras encontradas
estão provadamente entre inteiros, e o erro de quem cai numa banda partida é
0,70 p.p. tanto a 5 p.p. como a 1 p.p. A opção B compra a sensação de ter
resolvido.

---

## 8. Plano de execução para a sessão seguinte

Ordem, e cada passo com o seu portão.

**Passo 1 — o documento, antes do código.** Editar a §4 do `ARQUITETURA.md`:
a linha da tabela de consulta/cálculo, o parágrafo do `cenario`, e a decisão
sobre `ltv_min`/`ltv_max` como colunas. Justificação com os números desta
análise. ⚠️ Alterar também no Confluence — não há sincronização.

**Passo 2 — o domínio.** Substituir `BandaLTV`/`MesmaBanda` por um tipo de
função em degraus. Esboço a discutir, não a copiar:

```go
// DegrauLTV é um intervalo de LTV com um spread medido e as fronteiras onde
// ele muda. Resolvido == false quer dizer que se sabe que o preço muda aqui
// dentro, mas não onde — e aí não se responde.
type DegrauLTV struct {
    De, Ate   Racio
    Spread    Taxa
    Resolvido bool
}

// EscalaDeLTV é a função em degraus de um banco, ordenada e sem buracos.
type EscalaDeLTV []DegrauLTV

func (e EscalaDeLTV) SpreadEm(ltv Racio) (Taxa, error)
```

Testes, cada um visto a falhar primeiro:

* os três pontos da CGD (66,50 / 66,75 / 68,00) recebem **três** spreads
  diferentes — é o critério de pronto que a KAN-35 pede;
* um LTV dentro de um degrau não resolvido devolve erro, e não um spread;
* a escala recusa-se a construir com buracos ou sobreposições (à imagem do
  `ValidarFases`, que já faz exactamente isto para o plano de prestações).

**Passo 3 — o teste de reversão.** Repor o `BandaLTV` a 5 p.p. por trás do novo
tipo e confirmar que o teste dos três pontos **falha**, e falha a nomear os
0,70 p.p.

**Passo 4 — parar.** A descoberta das fronteiras no varrimento é **KAN-16**, e é
outro PR. Esta issue entrega a decisão, o documento e o tipo de domínio que a
KAN-16 vai preencher.

✅ **Escrito a 2026-07-27, no `internal/aplicacao/grelha`** (KAN-16, primeira
fatia). O `grelha.Descobrir` é a opção D: fase 1 uniforme ao passo, fase 2 por
bissecção entre vizinhos que discordem, e intervalo por resolver marcado em vez
de decidido em silêncio. Contra a forma medida da CGD: **86 amostras, 4
degraus**, dentro do orçamento de ~96 desta análise.

⚠️ **E uma medição nova que corrige em parte a §5.** O refinamento escrito
recorre aos **dois** lados de um ponto médio que discorde de ambos os extremos —
e não a um só, como uma bissecção clássica. Com isso, e **só com os dois
extremos amostrados na fase 1** (nem um ponto uniforme), a forma da CGD sai
inteira à mesma: 4 degraus, os três pontos de 66,50 / 66,75 / 68,00 certos, em
**29 amostras**. Ou seja: nesta forma, quem apanha o patamar de 1,25 p.p. é a
recursão pelos dois lados, e não a amostragem uniforme.

Isso **não** revoga o passo de 1 p.p., e a diferença entre as duas afirmações
importa: com a bissecção pelos dois lados, encontrar o patamar depende de algum
ponto médio cair lá dentro — o que aqui aconteceu e noutra forma pode não
acontecer. A amostragem uniforme é o seguro contra isso, e agora sabe-se o que o
seguro custa: **86 contra 29 pedidos**, cerca de 35 s a mais por corrida na CGD.
Fica medido e escrito; baixar o passo é uma decisão a tomar com o custo à vista,
e não por não se ter reparado.

⚠️ Medido também que a bissecção **clássica** — descer só pelo lado que ainda
contém a fronteira — perde mesmo o patamar quando ele cai entre dois pontos da
fase 1: com passo de 5 p.p., serve 2,050 a quem tem 69 % de LTV e paga 1,350. É
o teste `TestORefinamentoEncontraUmPatamarQueCaiuEntreDoisPontosDaFase1`, e foi
visto a falhar assim.

**Esforço estimado:** uma sessão. O passo 1 é o que leva mais tempo a escrever e
menos a executar; o passo 2 é pequeno porque não há chamadores a migrar.

✅ **Executado.** A KAN-35 foi fechada com o `dominio.EscalaDeLTV` no lugar do
`BandaLTV`, e a §4 alterada primeiro, como o passo 1 manda.

---

## 9. As decisões, e a regra internacional que as fixa

**Decidido a 2026-07-26: seguir as guidelines internacionais e, onde elas deixam
margem, a hipótese mais segura.** Não é uma preferência de estilo — é a regra que
o legislador já escreveu para exactamente este problema, e alinha com a nota que
o `dominio/oferta.go` já tem sobre a Directiva 2006/114/CE.

### O que a Directiva 2014/17/UE (MCD) prescreve

O Anexo I, Parte II, resolve o caso «há vários valores possíveis» sempre da mesma
maneira — **pelo mais caro**:

- **alínea (b)** — havendo formas de utilização com encargos ou taxas
  diferentes, presume-se a taxa e o encargo **mais altos** da forma mais comum;
- **alínea (d)** — havendo taxas ou encargos diferentes por período ou montante
  limitado, presume-se **a taxa e os encargos mais altos** para toda a duração do
  contrato.

E resolve o caso «não se sabe o que vem a seguir» com um **piso**: num contrato
com taxa fixa inicial, presume-se que no fim desse período a taxa passa a ser a
do indexante no momento do cálculo, **nunca inferior à taxa fixa**.

O Anexo II (a FINE/ESIS) fecha o par: quando um custo **não entra** na TAEG por
não ser conhecido da instituição, **isso tem de ser assinalado** ao consumidor.

**A regra que daí sai, e que este projecto adopta:** perante incerteza, assume-se
o valor **menos favorável ao consumidor** e **declara-se a assunção**. Nunca uma
sem a outra — assumir sem declarar é enganar, declarar sem assumir é não
responder.

### Decisão 1 — intervalo por resolver: serve-se o lado mais caro, com nota

Substitui a recusa que a §5 (opção D) tinha deixado em aberto. Dentro de um
degrau não resolvido, a oferta sai com o **spread mais alto do intervalo** e uma
**nota obrigatória** a dizer que é um limite superior e porquê.

É o que a alínea (d) manda, é o que a §4 já faz com o TAEG que não se consegue
dar, e é mais útil do que recusar — o cliente fica a saber que não pagará **mais**
do que aquilo.

⚠️ **A nota não é opcional e não é decorativa.** Sem ela isto vira o defeito que
a §7.4 descreve: um número errado com ar de certo. O mecanismo já existe —
`Oferta.Anotar` — e o `Ajuste` do domínio já torna «valor diferente do pedido sem
nota» não representável. O degrau não resolvido deve seguir o mesmo desenho.

### Decisão 2 — `ltv_min` e `ltv_max` como colunas

Duas colunas `numeric`, não uma chave de texto a empacotá-las. Uma chave
composta parte-se em silêncio quando o formato muda — é literalmente a armadilha
que o `DOSSIE-BANCOS.md` regista no `ConditionCode` do Montepio, onde partir a
string pelo separador errado dá a finalidade errada. Duas colunas tipadas não
têm esse modo de falha, e indexam-se.

Custo: uma migração `goose` e uma alteração à tabela da §4. É o único custo de
migração desta análise, e paga-se uma vez.

### Decisão 3 — tolerância do refinamento: 0,05 p.p.

Mantém-se o proposto. ⚠️ **É escolhida, não medida** — uma ordem de grandeza
abaixo do menor degrau observado (0,05 p.p., entre 1,950 e 2,000). Fica escrito
que é uma escolha, para que a primeira medição que a contrarie a possa mudar sem
discussão.

### Continua fora de âmbito

**Mapear os degraus da CGD abaixo dos 32 %.** Sabe-se agora que existe pelo menos
uma fronteira em (33,00 ; 33,50]; o resto abaixo disso não foi varrido.

---

## 10. Uma consequência que sai daqui e não é desta issue

A mesma regra do Anexo I aplica-se a uma decisão já tomada na **KAN-10**, e
contraria-a em parte.

Na taxa **mista** do Novo Banco, o banco não diz que taxa aplica depois do
período fixo, e o v2 devolve por isso a oferta **sem fases**, com nota. Isso
respeita «não inventar», mas a MCD tem uma resposta melhor do que o silêncio: a
presunção do fim do período fixo — indexante actual, **nunca abaixo da taxa
fixa** — permite publicar a segunda fase como **assunção declarada** em vez de a
omitir.

Não se muda aqui: está aberto em **KAN-38**, com a base legal, para se decidir com
o mesmo critério e não à pressa.

⚠️ **Nota de 2026-07-27:** o Montepio deu à KAN-38 um caso mais forte, porque
**ele próprio declara a assunção**. Na mista, projecta a fase indexada à taxa do
período fixo — a fase indexada vem com a TAN e a prestação repetidas da fixa — e é
dessa projecção que saem o MTIC e a TAEG dele. Ou seja: um banco português já faz
o que a MCD prescreve, e é isso que a KAN-38 tem de decidir se copiamos.

---

## 11. Defeitos encontrados por esta análise

Nenhum novo que precise de issue própria. Dois apontamentos que vão para dentro
da KAN-35 por serem a mesma decisão:

* `BandaLTV` e `MesmaBanda` são **código morto** — sem chamadores em produção. O
  `MesmaBanda` foi escrito para a KAN-15, que está fechada e cuja razão de ser (a
  cache) a §4 declara extinta.
* O comentário do `MesmaBanda` promete ser «a guarda que a quantização do pedido
  vai precisar». Essa guarda, tal como está escrita, é **insuficiente pela mesma
  razão** que tudo o resto nesta análise: guardaria degraus de 5 % que não são os
  degraus do banco.
