# Uso responsável

⚠️ **Não sou advogado e isto não é aconselhamento jurídico.** O documento separa deliberadamente duas coisas: o que se decide tecnicamente e já está decidido (§2), e o que exige parecer de quem sabe **antes** de publicar (§3). Não misturar as duas é o objectivo do documento.

⚠️ **Trazido do Confluence para o disco a 2026-07-27 (KAN-40).** Vivia só numa morada. Na passagem, as afirmações que a inversão da §1 tornou falsas foram corrigidas e estão marcadas com ⚠️ **corrigido** — não se traz para a morada de trabalho um documento que contradiz o desenho. As referências a issues do GitHub foram convertidas para as do `KAN`.

## 1. O que mudou face ao v1

O v1 era um script pessoal. O `README` dizia «projeto de uso pessoal/educativo».

O v2 é outra coisa: **uma app publicada em lojas** que compara o preçário de dez bancos portugueses, a pedido de pessoas que não conhecemos. A diferença não é de grau.

Três eixos onde isso pesa:

| eixo | script pessoal | app publicada |
| --- | --- | --- |
| Carga nos bancos | umas dezenas de pedidos por dia, do meu portátil | um varrimento nosso por dia, em hora fixa |
| Termos de serviço dos bancos | uso privado | uso comercial ou público, redistribuição de dados |
| Responsabilidade pelos números | só eu os leio | pessoas decidem crédito à habitação com eles |

⚠️ **corrigido — a carga já não é proporcional aos utilizadores.** Dizia aqui «proporcional aos utilizadores, do nosso servidor», e era verdade no desenho antigo, em que cada visita disparava um scrape. Com a inversão da §1 deixou de ser: os bancos são interrogados **uma vez por varrimento**, em hora morta, e a resposta ao cliente sai de cálculo local sobre a grelha. Um milhão de visitas e uma visita custam ao banco exactamente o mesmo. É a melhor coisa que este desenho fez pelo uso responsável, e vale a pena dizê-la em voz alta.

## 2. Decisões técnicas — já tomadas

Estas são nossas, não dependem de parecer nenhum, e são vinculativas.

### Reduzir a carga nos bancos ao mínimo que funciona

⚠️ **corrigido — esta secção descrevia o desenho anterior à inversão da §1.** A cache, a quantização do pedido e o *gate* por banco em Redis existiam todos para o mesmo fim: aparar a carga de um modelo em que cada visita ia à rede. Esse modelo não existe, e os três também não — a `KAN-8` (cache) e a `KAN-15` (quantização) foram fechadas **sem se fazerem**, e o Redis saiu do ambiente na `KAN-39`. O que ficou é mais simples e reduz mais:

* **Os bancos são interrogados só pelo varrimento**, em hora morta e fixa, com guarda de idempotência (`KAN-16`). É o único caminho para eles.
* **A resposta ao cliente não toca em banco nenhum**: é uma consulta a `catalogo_taxas` mais cálculo local (`ARQUITETURA.md` §1, §4).
* **Um varrimento por banco de cada vez**, coordenado por *advisory lock* no PostgreSQL (`internal/infra/travao`). Não há paralelismo contra o mesmo banco.
* **Tecto por IP** (`KAN-14`), que continua a fazer sentido — não para proteger os bancos, que já não recebem os pedidos, mas para proteger o nosso serviço.

### Nunca tocar no que não é simulação

⚠️ **Endpoints de registo de contactos (*lead*) são proibidos** — em código, em testes e em capturas. Um simulador público existe para simular; submeter um contacto ao CRM de um banco é outra coisa completamente diferente, e seria feito em nome de uma pessoa que não o pediu. O v1 mantinha uma lista destes caminhos; ela migra para cá e entra no portão.

Do mesmo modo: **nada de contornar autenticação, nada de áreas de cliente, nada de dados que não estejam abertos a qualquer visitante sem sessão.**

### Ser honesto sobre o que os números são

* **Valores indicativos, e a app di-lo onde é preciso** — não escondido nas definições. São simulações públicas sem análise de risco; não são propostas.
* **A fonte e a hora aparecem sempre em cada oferta.** Um preço varrido esta madrugada tem de o dizer (`ECRAS.md`, ecrã de detalhe).
* **Os produtos aplicados são visíveis.** Sem isso, um banco com descontos por omissão parece só mais barato — e a comparação induz em erro sem mentir. ⚠️ Isto deixou de ser aspiração e passou a estrutura na `KAN-33`: quem escolhe os produtos é o pedido, e a diferença estava medida a inverter a ordem entre a CGD e o Novo Banco.
* **«Ajusta e anota», nunca «ajusta e cala».** Sempre que o banco simulou coisa diferente da pedida, diz-se no cartão (`CONTRATO-BANCO.md` §6).
* **O que o banco não diz, não se publica.** Na taxa mista nem o Novo Banco nem o Montepio dizem o que se passa depois do período fixo — e nenhum dos dois sai com um plano de fases inventado.
* **Não somos intermediários de crédito** e a app não diz nem sugere que é.

### Dados pessoais

* **O servidor não guarda simulações.** Correm e são devolvidas (`ARQUITETURA.md` §4).
* A série de mercado usa **titular neutro** e cenários fixos — por construção não tem dados de ninguém.
* **As capturas versionadas usam titular fictício**, e o portão procura segredos e cookies antes de deixar submeter (`CONTRATO-BANCO.md` §3).

### Identificar-nos

⚠️ **Decisão a tomar, com consequência real nos dois sentidos.** Um `User-Agent` que nos identifique e um endereço de contacto é a postura honesta e é o que um banco preferiria receber. Mas vários scrapers do v1 dependem de **parecer um browser normal** — o Bankinter está atrás de Cloudflare precisamente a filtrar impressões digitais de automação, e identificarmo-nos parte esse banco.

Não há resposta que sirva os dois casos. Registar a escolha por banco, com a razão, em vez de a deixar acontecer por omissão.

## 3. O que exige parecer antes de publicar

⚠️ **Isto é uma lista de perguntas, não de respostas.** Vive na `KAN-24`, com `prioridade-alta`, e é bloqueante da publicação nas lojas — não da fase 1.

1. **Termos de serviço de cada banco.** Ler os dez. Alguns proíbem explicitamente acesso automatizado ou reutilização de conteúdo; outros não dizem nada. Um `robots.txt` a proibir não é o mesmo que um contrato a proibir, e nenhum dos dois é irrelevante.
2. **Reutilização e redistribuição.** Publicar uma série temporal do preçário de dez bancos, e servi-la a outra aplicação por API, é diferente de a consultar. Que estatuto tem essa base de dados?
3. **Regulação de crédito.** Comparar ofertas de crédito à habitação e apresentá-las a consumidores toca em regras próprias em Portugal — do Banco de Portugal e do regime dos intermediários de crédito. Onde está a fronteira entre «ferramenta de comparação» e actividade regulada?
4. **Requisitos das lojas.** A Apple e a Google têm regras próprias para apps financeiras: declarações obrigatórias, o que se pode afirmar, o que a ficha da loja tem de dizer.
5. **RGPD.** Mesmo sem persistir nada, recolhem-se data de nascimento e rendimento no dispositivo e enviam-se a um servidor. Isso exige fundamento, política de privacidade, e uma resposta clara à pergunta «o que acontece a estes dados» — que hoje é «nada, não são guardados», o que é uma boa resposta, mas tem de estar escrita.
6. **Responsabilidade.** Se um número estiver errado — e vai estar, um dia, porque um banco mudou o formato sem avisar — qual é a exposição, e que limitação de responsabilidade é preciso ter.

## 4. Se um banco pedir para parar

Pára-se. Sem discussão e sem esperar por carta de advogado.

Consequência técnica, e é por isso que isto está num documento de arquitectura e não numa página de rodapé: **desligar um banco tem de ser trivial**. Uma entrada no registo, sem rebuild, sem migração, sem tocar em mais nada — e a app deixa de o listar sozinha, porque a lista vem de `GET /api/v1/bancos`. Isso é um requisito de desenho, e está coberto pela forma como o registo de bancos está feito (`CONTRATO-BANCO.md` §1).
