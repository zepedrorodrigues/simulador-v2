# Uso responsável

⚠️ **Não sou advogado e isto não é aconselhamento jurídico.** O documento separa
deliberadamente duas coisas: o que se decide tecnicamente e já está decidido
(§2), e o que exige parecer de quem sabe **antes** de publicar (§3). Não misturar
as duas é o objectivo do documento.

## 1. O que mudou face ao v1

O v1 era um script pessoal. O `README` dizia «projeto de uso pessoal/educativo».

O v2 é outra coisa: **uma app publicada em lojas** que corre os simuladores
públicos de dez bancos portugueses, ao vivo, a partir de um endereço IP nosso, a
pedido de pessoas que não conhecemos. A diferença não é de grau.

Três eixos onde isso pesa:

| eixo | script pessoal | app publicada |
|---|---|---|
| Carga nos bancos | umas dezenas de pedidos por dia, do meu portátil | proporcional aos utilizadores, do nosso servidor |
| Termos de serviço dos bancos | uso privado | uso comercial ou público, redistribuição de dados |
| Responsabilidade pelos números | só eu os leio | pessoas decidem crédito à habitação com eles |

## 2. Decisões técnicas — já tomadas

Estas são nossas, não dependem de parecer nenhum, e são vinculativas.

### Reduzir a carga nos bancos ao mínimo que funciona

- **Cache antes de tudo.** É a razão principal por que a cache existe, mais do que
  a latência: cada acerto de cache é um pedido que o banco não recebe.
  (`ARQUITETURA.md` §7)
- **Quantizar o pedido.** Dois visitantes com 240 000 € e 240 500 € passam a
  partilhar um scrape em vez de gerarem dois. (issue #18)
- **Tecto por IP**, para um cliente sozinho não pôr o nosso endereço a martelar
  os simuladores. (issue #17)
- **Um scrape por banco de cada vez**, coordenado em Redis. Não há paralelismo
  contra o mesmo banco. (issue #11)
- **Varrimento a uma hora morta e fixa**, com guarda de idempotência. (issue #19)

### Nunca tocar no que não é simulação

⚠️ **Endpoints de registo de contactos (*lead*) são proibidos** — em código, em
testes e em capturas. Um simulador público existe para simular; submeter um
contacto ao CRM de um banco é outra coisa completamente diferente, e seria feito
em nome de uma pessoa que não o pediu. O v1 mantinha uma lista destes caminhos;
ela migra para cá e entra no portão.

Do mesmo modo: **nada de contornar autenticação, nada de áreas de cliente, nada
de dados que não estejam abertos a qualquer visitante sem sessão.**

### Ser honesto sobre o que os números são

- **Valores indicativos, e a app di-lo onde é preciso** — não escondido nas
  definições. São simulações públicas sem análise de risco; não são propostas.
- **A fonte e a hora aparecem sempre em cada oferta.** Um valor de cache com
  quatro horas tem de o dizer. (`ECRAS.md`, ecrã de detalhe)
- **Os produtos aplicados são visíveis.** Sem isso, um banco com descontos por
  omissão parece só mais barato — e a comparação induz em erro sem mentir.
- **«Ajusta e anota», nunca «ajusta e cala».** Sempre que o banco simulou coisa
  diferente da pedida, diz-se no cartão. (`CONTRATO-BANCO.md` §6)
- **Não somos intermediários de crédito** e a app não diz nem sugere que é.

### Dados pessoais

- **O servidor não guarda simulações.** Correm e são devolvidas.
  (`ARQUITETURA.md` §4)
- A série de mercado usa **titular neutro** e cenários fixos — por construção não
  tem dados de ninguém.
- **A cache não leva dados pessoais**: a idade entra como escalão, não como data
  de nascimento. (issue #11)
- **As capturas versionadas usam titular fictício**, e o portão procura segredos
  e cookies antes de deixar submeter. (`CONTRATO-BANCO.md` §3)

### Identificar-nos

⚠️ **Decisão a tomar, com consequência real nos dois sentidos.** Um `User-Agent`
que nos identifique e um endereço de contacto é a postura honesta e é o que um
banco preferiria receber. Mas vários scrapers do v1 dependem de **parecer um
browser normal** — o Bankinter está atrás de Cloudflare precisamente a filtrar
impressões digitais de automação, e identificarmo-nos parte esse banco.

Não há resposta que sirva os dois casos. Registar a escolha por banco, com a
razão, em vez de a deixar acontecer por omissão.

## 3. O que exige parecer antes de publicar

⚠️ **Isto é uma lista de perguntas, não de respostas.** Fica na issue própria, com
`prioridade-alta`, e é bloqueante da publicação nas lojas — não da fase 1.

1. **Termos de serviço de cada banco.** Ler os dez. Alguns proíbem
   explicitamente acesso automatizado ou reutilização de conteúdo; outros não
   dizem nada. Um `robots.txt` a proibir não é o mesmo que um contrato a proibir,
   e nenhum dos dois é irrelevante.
2. **Reutilização e redistribuição.** Publicar uma série temporal do preçário de
   dez bancos, e servi-la a outra aplicação por API, é diferente de a consultar.
   Que estatuto tem essa base de dados?
3. **Regulação de crédito.** Comparar ofertas de crédito à habitação e
   apresentá-las a consumidores toca em regras próprias em Portugal — do Banco de
   Portugal e do regime dos intermediários de crédito. Onde está a fronteira
   entre «ferramenta de comparação» e actividade regulada?
4. **Requisitos das lojas.** A Apple e a Google têm regras próprias para apps
   financeiras: declarações obrigatórias, o que se pode afirmar, o que a ficha da
   loja tem de dizer.
5. **RGPD.** Mesmo sem persistir nada, recolhem-se data de nascimento e
   rendimento no dispositivo e enviam-se a um servidor. Isso exige fundamento,
   política de privacidade, e uma resposta clara à pergunta «o que acontece a
   estes dados» — que hoje é «nada, não são guardados», o que é uma boa resposta,
   mas tem de estar escrita.
6. **Responsabilidade.** Se um número estiver errado — e vai estar, um dia, porque
   um banco mudou o formato sem avisar — qual é a exposição, e que limitação de
   responsabilidade é preciso ter.

## 4. Se um banco pedir para parar

Pára-se. Sem discussão e sem esperar por carta de advogado.

Consequência técnica, e é por isso que isto está num documento de arquitectura e
não numa página de rodapé: **desligar um banco tem de ser trivial**. Uma entrada
no registo, sem rebuild, sem migração, sem tocar em mais nada — e a app deixa de
o listar sozinha, porque a lista vem de `GET /api/v1/bancos`. Isso é um requisito
de desenho, e está coberto pela forma como o registo de bancos está feito
(`CONTRATO-BANCO.md` §1).
