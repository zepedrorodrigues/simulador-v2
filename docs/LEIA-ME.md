# Documentação

Este repositório publica o **código** do `simulador-v2` e o que é preciso para o
pôr a correr localmente. O desenho — arquitectura, modelo de dados, contrato dos
bancos, plano de fases — **não é versionado aqui**. Vive num espaço Confluence
privado.

Não é acidente nem esquecimento: é uma decisão. O código é aberto de propósito;
o planeamento não.

## O que existe, e o que cada documento decide

| documento | o que decide |
|---|---|
| `ARQUITETURA` | as quatro camadas, o modelo de dados, a política de cache e os portões. **Vinculativo** — quando o código diverge dele, um dos dois está errado |
| `CONTRATO-BANCO` | como se acrescenta um banco, e a disciplina de captura antes de código |
| `DOSSIE-BANCOS` | o que se apurou sobre cada um dos dez bancos |
| `API` | as duas fronteiras HTTP e os seus estatutos diferentes |
| `ECRAS` | os ecrãs da app React Native |
| `APP` | a stack da app e o que ela impõe ao servidor |
| `USO-RESPONSAVEL` | carga nos bancos, dados pessoais, e o que exige parecer jurídico |
| `PLAN` | as seis fases e a ordem entre elas |
| `RESUME` | estado actual e próximos passos |

## Pedir acesso

Abre uma [issue](../../issues) a dizer quem és e para que precisas, ou fala
directamente com o dono do repositório
([@zepedrorodrigues](https://github.com/zepedrorodrigues)). O acesso ao
Confluence é dado caso a caso.

Se a tua dúvida é sobre **como correr isto localmente**, não precisas de acesso
nenhum — está tudo no [`README.md`](../README.md).

Se a tua dúvida é **porque é que o portão te reprovou**, também não: as
mensagens do `depguard` enunciam a regra que foi violada, sem remeter para
documento nenhum.

## Uso responsável — o resumo

Esta parte fica pública de propósito, porque uma postura escondida não é postura.
O documento completo desenvolve-a; o essencial é isto:

- **Só simuladores públicos.** O servidor interroga os simuladores de crédito
  que os bancos publicam nos seus sites. **Nunca** endpoints de registo de
  contactos, de marcação ou de envio de propostas — não se geram *leads* nem se
  entrega o que quer que seja a um banco em nome de ninguém.
- **Frequência baixa, e cache a sério.** Cada comparação custa dezenas de
  segundos de trabalho aos servidores dos bancos, e parte do nosso IP. Por isso
  há cache com validade ajustada ao custo de cada banco, um *gate* que impede o
  mesmo banco de ser interrogado em paralelo, e um tecto de pedidos por IP.
  Não é só protecção nossa — é não ser um peso para quem não pediu nada.
- **Sem dados pessoais.** As simulações de utilizador **não são guardadas**: são
  corridas e devolvidas. A única coisa persistida a longo prazo é a série de
  mercado, varrida sobre cenários fixos com um titular neutro e fictício. Não há
  contas, sessões, emails nem histórico por pessoa, e não vai haver.
- **Os valores são indicativos.** São o que o simulador público de cada banco
  devolveu, na data registada. **Não são propostas, não vinculam o banco e não
  são aconselhamento financeiro.** Uma proposta a sério vem do banco, por
  escrito, depois de avaliar quem a pede.
- **Há perguntas em aberto, e estão assumidas como tal.** Nomeadamente as que
  exigem parecer jurídico antes de isto ser publicado numa loja de aplicações.
  Estão listadas no documento completo e numa issue própria — não estão
  resolvidas por omissão.
