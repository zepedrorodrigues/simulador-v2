# A resolução do LTV: o que se mediu e o que daí se decidiu (KAN-35)

A §4 do `ARQUITETURA.md` remete para aqui. Este documento guarda **as medições** que fizeram a grelha passar a guardar intervalos com fronteiras medidas em vez de bandas de passo fixo, e **as decisões** que daí saíram — não a deliberação que as produziu.

Medido a **2026-07-26**. Imóvel fixo em 400 000 € e montante a variar de 1 000 €, para cada passo ser exactamente 0,25 p.p. de LTV sem arredondamentos a sujar a medição. Taxa variável, 30 anos, habitação própria.

## O que se mediu

**CGD, à volta dos 67 %** — e o preço **não é monótono**:

| LTV | 65,00–66,50 | 66,75–67,75 | 68,00–69,00 |
|---|---|---|---|
| spread | 2,000 | **2,050** | 1,350 |

**CGD, à volta dos 33 %:** 1,950 em 32,00–33,00 e 2,000 em 33,50–36,00.

**Novo Banco:** 0,75 em 49,50–50,00 e 0,80 em 50,25–51,50; 0,90 em 79,50–80,00 e 0,95 em 80,25–81,50.

### Três fronteiras não contêm nenhum LTV inteiro

| fronteira | está em | contém inteiro? |
|---|---|---|
| CGD 2,000 → 2,050 | (66,50 ; 66,75] | **não** |
| CGD 1,950 → 2,000 | (33,00 ; 33,50] | **não** |
| CGD 2,050 → 1,350 | (67,75 ; 68,00] | sim — 68 |
| Novo Banco 0,75 → 0,80 | (50,00 ; 50,25] | não |
| Novo Banco 0,90 → 0,95 | (80,00 ; 80,25] | não |

**As duas primeiras provam que uma grelha de 1 p.p. tem o mesmo defeito da de 5 p.p.**, só mais pequeno: as fronteiras da CGD não estão alinhadas com passo nenhum que se escolha à cabeça.

**As do Novo Banco são o contrário** — caem nos múltiplos de 5, consistentes com «LTV até 50 %», «até 80 %». Os dois bancos discordam sobre a forma da coisa, e **é por isso que um banco não chegava para decidir**.

### O patamar estreito, e o que ele elimina

O 2,050 da CGD ocupa ~1,25 p.p., de ~66,6 % a ~67,9 %. Não é uma fronteira: é um patamar isolado entre dois mais largos.

**Elimina uma família inteira de soluções.** Qualquer descoberta por bissecção que assuma monotonia, ou poucos degraus, salta este patamar. Foi encontrado por amostragem uniforme — é a razão de a fase 1 da descoberta ser uniforme e não recursiva.

## Porque é que afinar o passo não resolvia

A conta que não é intuitiva, e é o coração disto: **afinar a grelha não reduz o erro — reduz a exposição.** Numa grelha de passo fixo, a banda que contém uma fronteira serve um só spread aos dois lados dela, e o erro de quem cai do lado errado é a altura do degrau, seja qual for o passo:

| resolução | bandas partidas (CGD) | largura exposta | erro de quem lá cai | linhas por banco |
|---|---|---|---|---|
| 5 p.p. | banda 70 | 5 p.p. | **0,70 p.p.** | 20 |
| 1 p.p. | bandas 67 e 68 | 2 p.p. | **0,70 p.p.** | ~71 |
| 0,25 p.p. | 2 bandas | 0,5 p.p. | **0,70 p.p.** | ~280 |
| **intervalos medidos** | nenhuma | 0 | **0** | **~4** |

Os 0,70 p.p. valem ~**79 €/mês** num empréstimo de 200 000 € a 30 anos — mais do que a diferença entre bancos que este projecto existe para comparar.

**E a representação exacta é também a mais pequena:** ~4 linhas por banco contra 20, 71 ou 280. Guarda o que o preço **é**, em vez de uma fotografia dele em pontos escolhidos por nós. O custo desloca-se todo para o varrimento, que passa a descobrir as fronteiras — ~96 pedidos por banco, ~58 s na CGD.

## As decisões

### A regra da casa perante incerteza

**Decidido a 2026-07-26**, e vale muito para além desta issue: **perante incerteza, assume-se o valor menos favorável ao consumidor e declara-se a assunção.** Nunca uma sem a outra — assumir sem declarar é enganar, declarar sem assumir é não responder.

Não é preferência de estilo: é o que a **Directiva 2014/17/UE (MCD)** prescreve para exactamente este problema, no Anexo I, Parte II:

- **alínea (b)** — havendo formas de utilização com encargos ou taxas diferentes, presume-se a taxa e o encargo **mais altos** da forma mais comum;
- **alínea (d)** — havendo taxas ou encargos diferentes por período ou montante limitado, presume-se **os mais altos** para toda a duração do contrato.

E o Anexo II (a FINE/ESIS) fecha o par: um custo que **não entra** na TAEG por não ser conhecido da instituição **tem de ser assinalado**.

### 1 — Num degrau por resolver serve-se o lado mais caro, com nota obrigatória

É o que a alínea (d) manda, e é mais útil do que recusar: o cliente fica a saber que não pagará **mais** do que aquilo.

⚠️ **A nota não é opcional.** Sem ela isto vira um número errado com ar de certo. É por isso que o `DegrauLTV` guarda **os dois lados** (`Spread` e `SpreadMinimo`) em vez de uma flag: a frase que a pessoa lê nomeia os dois números, e sem eles diria apenas «é aproximado», que não é informação.

### 2 — `ltv_min` e `ltv_max` como colunas, não uma chave de texto

Uma chave composta parte-se em silêncio quando o formato muda. É a armadilha que o `DOSSIE-BANCOS.md` regista no `ConditionCode` do Montepio, onde partir a string pelo separador errado dá a finalidade errada. Duas colunas tipadas não têm esse modo de falha, e indexam-se.

### 3 — Tolerância do refinamento: 0,05 p.p.

⚠️ **É escolhida, não medida** — uma ordem de grandeza abaixo do menor degrau observado (os 0,05 p.p. entre 1,950 e 2,000). Fica escrito que é escolha para que a primeira medição que a contrarie a mude sem discussão.

## O que isto abriu, e está por decidir na KAN-38

A mesma regra do Anexo I contraria em parte uma decisão da `KAN-10`. Na **mista do Novo Banco** o banco não diz que taxa aplica depois do período fixo, e o v2 devolve a oferta **sem fases**, com nota. Isso respeita «não inventar», mas a MCD tem resposta melhor do que o silêncio: a presunção do fim do período fixo — indexante actual, **nunca abaixo da taxa fixa** — permite publicar a segunda fase como **assunção declarada** em vez de a omitir.

⚠️ **E o Montepio deu-lhe um caso mais forte** (2026-07-27): ele próprio declara a assunção — projecta a fase indexada à taxa do período fixo, e é dessa projecção que saem o MTIC e a TAEG dele. **Um banco português já faz o que a MCD prescreve**, e a `KAN-38` tem de decidir se copiamos.

## Por fazer

**Mapear os degraus da CGD abaixo dos 32 %.** Sabe-se que há pelo menos uma fronteira em (33,00 ; 33,50]; abaixo disso não foi varrido.
