# Plano

Seis fases. Cada uma acaba em algo que **funciona e se pode ver a funcionar**. Nenhuma começa antes de a anterior ter o portão verde — a dívida do v1 acumulou-se por se passar à frente com «depois arruma-se».

O desenho está em `docs/ARQUITETURA.md`. O backlog é o projecto `KAN` do JIRA; não se abrem mais issues do GitHub para este repositório.

⚠️ **O trabalho cancelado fica escrito como cancelado, e não apagado.** Um plano onde o trabalho morto desaparece sem rasto é um plano onde alguém volta a propô-lo daqui a um mês.

⚠️ **E isto vale para o tracker, não só para este ficheiro — aprendido a doer a 2026-08-06**, quando a triagem da reversão apagou cinco issues do `KAN` em vez de as cancelar, incluindo uma (`KAN-8`) que estava **viva**. A regra que daí sai: **o `KAN` organiza o trabalho; a prova mora aqui.** Uma issue apagada leva consigo a razão de uma decisão; um documento versionado não, porque tem histórico.

---

## Fase 0 — Fundações ✅

Toolchain, quatro camadas com `depguard` a impor a regra de dependência, `docker-compose` com PostgreSQL, migrações `goose`, `sqlc`, `openapi.yaml` e a geração dos tipos, CI. `KAN-1` a `KAN-4`.

**Portão:** verde, com o `depguard` a reprovar de facto um `import` proibido — verificado por reversão, a nomear a camada.

## Fase 1 — A fatia vertical ✅

Quatro bancos de HTTP puro, escolhidos para exercitarem o máximo de dimensões ao mínimo custo: **CGD** (linha de base, períodos fixos descobertos em runtime), **Novo Banco** (produtos por omissão, erro estruturado como sinal, Euribor escolhível, finalidade muda o preço), **Montepio** (sessão `GET`→`POST`, sem taxa fixa, prova o «ajusta e anota»), **Banco CTT** (Euribor imposta e lida da resposta). `KAN-5`, `KAN-6`, `KAN-9`–`KAN-14`.

**Três coisas morreram aqui com a inversão da §1** (2026-07-25) — e **duas voltaram a nascer com a reversão de 2026-08-06**:

| | 2026-07-25 | 2026-08-06 |
|---|---|---|
| `KAN-7` fan-out do `comparar` | morreu: lia a série e calculava localmente | ⚠️ **VIVO** — o `comparar` volta a falar com bancos, e é onde o `-race` ganha valor |
| `KAN-8` cache e *gate* (hoje **`KAN-58`** — a chave foi apagada na triagem) | morreu: não havia pedido ao banco no caminho do cliente | ⚠️ **VIVO, com outra forma** — cache em Postgres (não Redis), e o *gate* passa de fecho a **tecto de concorrência** por banco |
| `KAN-15` quantização do pedido | morreu: existia para acertos de cache | ⛔ **continua morta, e agora por decisão e não por acaso.** A cache voltou e a quantização não vem com ela: a chave é o pedido exacto. A razão pela qual era perigosa — arredondar cruza um degrau de preço — volta a valer por inteiro, e **sem** a protecção estrutural que a grelha dava |

## Fase 2 — Mercado ⛔ *cancelada a 2026-08-06*

⚠️ **Cancelada pela reversão da §1, e fica escrita como cancelada.** O varrimento morreu (D1), e com ele a série temporal que esta fase existia para produzir.

**O que fica de pé desta fase:** os parsers e as capturas dos bancos (`KAN-17`, `KAN-18`), que passam a ser o activo principal do repositório.

**O que morreu:** a grelha, a escala de LTV por fronteiras medidas, a base da fixa (`KAN-41`), o resíduo em coluna e a TAEG derivada dos encargos. ⚠️ Toda a `KAN-16` para além de «ler um banco» era estrutura para guardar uma reconstrução do preço — ver `docs/DECISAO-AO-VIVO.md` §2.

⚠️ **Dívida por saldar, e é com outro repositório:** o `/api/rate-catalog` fica sem fonte e o `viabilidade-imobiliaria` consome-o em produção. A retirada tem de ser combinada; as saídas estão na `DECISAO-AO-VIVO.md` §4, D1, e **falta escolher qual**.

⏳ `KAN-19` — Crédito Agrícola. Continua a fazer sentido: é um banco a mais para perguntar.

## Fase 3 — Bancos de browser ⏳

⚠️ **Começa por uma decisão, não por código.** É a parte de risco por avaliar.

- `KAN-20` (Epic) — `playwright-go` vs `chromedp`, com o Bankinter (Cloudflare) como caso de teste
- `KAN-21` — procurar a API por baixo do OutSystems do BPI **antes** de escrever scraper de browser
- ActivoBank + Millennium BCP, Bankinter, BPI — sub-trabalho do `KAN-20`, e **só se detalham depois da decisão da biblioteca**

**Saída conhecida se correr mal:** correm como serviço à parte, atrás da mesma interface. ⚠️ Com argumento medido: a imagem são **24,4 MB** sem Chromium, contra 4-8 GB no v1. Meter o browser dentro multiplica-a por cem.

## Fase 4 — Produção 🔶

Feito: `Dockerfile` não-root sem Chromium, logs com `X-Request-ID` (`KAN-43`), CORS com lista explícita, `/healthz`, e a forma de produção **corrida em local a 2026-08-01** — migrações como passo próprio, base fechada, proxy com TLS, e o restauro de `pg_dump` executado.

⏳ **Alojamento adiado** (2026-08-01), até isto estar testado a fundo. Nada está preso a fornecedor nenhum. O `fly.toml` fica e não descreve nada executado.

⏳ **Medir o `PROXIES_DE_CONFIANCA`** com um pedido real. ⚠️ Fica vazio até estar medido, e a razão é assimétrica: larga de mais deixa contornar o tecto de vez; vazia, o tecto do site inteiro passa a ser o de um utilizador — o bug de produção do v1. **Um tecto apertado de mais é visível; um contornável não é.**

## Fase 5 — Ecrã de mercado ⛔ *cancelada a 2026-08-06*

`KAN-23` (Epic). ⚠️ **Cancelada com a série que a alimentava.** Sem varrimento não há série temporal, e um ecrã de mercado sobre nada é um ecrã vazio.

⚠️ Fica escrita como cancelada, e não apagada: é a funcionalidade mais óbvia de voltar a propor daqui a um mês, e a resposta é que **exige primeiro decidir se o varrimento volta** — o que contraria a D1.

## Fase 6 — A fatia ao vivo 🆕 *(2026-08-06)*

A fase que a reversão da §1 abre, e a que passa a ser o produto.

⚠️ **Começa por medir, não por escrever.** Eram três números que não tínhamos; **um está medido** e os outros dois não se medem da mesma maneira:

| medir | porquê |
|---|---|
| ~~latência por banco~~ ✅ **medida** (2026-08-06, 17h13) | 25 simulações frias: Novo Banco 460 ms de mediana, Montepio 2,01 s e **cauda de 8,4 s**. Fixa o timeout — os 15 s passam a ser ~1,8× o pior observado, e não um palpite. `make latencia`, tabela no `DOSSIE-BANCOS.md` |
| ~~concorrência que cada banco tolera~~ ✅ **escolhida** (2026-08-06) | **2 vagas por banco**, o apertado que já estava medido. ⚠️ **Não se mediu procurando onde parte** — ver abaixo. Está no `internal/infra/lotacao`, e o que fica por medir é se 2 chega quando houver clientes a sério |
| validade útil da cache | fixa o TTL do §7.6. ⚠️ **Não é uma corrida, é uma vigia**: pede horas ou dias de relógio, não pedidos. Fica **curta** até estar medida — **5 min** desde 2026-08-07, e o número é de partida e não medido |

⚠️ **A concorrência que um banco «tolera» não se mede subindo até ele recusar.** Isso é um teste de carga contra o simulador público de um terceiro, e o resultado que produz — o ponto onde parte — é exactamente o que não se quer usar. O que este repositório já fez, e é o método: mediu **1, 2 e 4 pedidos em paralelo** contra a CGD (2026-07-26, 8 pontos) — 1,086 s por ponto, 603 ms e 367 ms, **zero falhas nos três** — e escolheu **2**, não 4. A razão está escrita no `PorBancoOmissao`, e vale inteira aqui: comprar 40 % de velocidade ao preço de quadruplicar a carga que se põe num sistema alheio não é uma troca que se faça por se poder fazer.

⚠️ **E ao vivo a pergunta é outra**, o que torna a medição menos urgente do que parecia: o travão do varrimento era um **fecho** contra duas corridas nossas; ao vivo, dois clientes a perguntar pelo mesmo banco é o normal, e o que se dimensiona é um tecto de simultâneos. Enquanto não houver clientes, não há número a medir — há um tecto a escolher, e escolhe-se **2 por banco**, que é o que está medido sem falhas.

⚠️ **A validade da cache não se mede com pedidos, mede-se com tempo.** É perguntar o mesmo ao mesmo banco de hora a hora e ver quando a resposta muda — e o que decide não é a média, é a **primeira** mudança, porque servir um preço que mudou é o erro que a cache pode causar. Um preçário muda em dias; uma Euribor diária muda todos os dias úteis de manhã. ⚠️ Até haver vigia, o TTL fica **curto** e não a zero: zero é não ter cache, e a cache existe para não sermos um amplificador.

Depois disso, e por esta ordem:

1. `KAN-7` — o `comparar` volta a falar com bancos, com tecto e timeout por banco. 🔶 **O caminho está de pé** (2026-08-06): `aplicacao/aovivo` pergunta a um banco, e o `POST /api/v1/ofertas/{banco}` serve-o. ⚠️ **O timeout está medido** — 15 s, ~1,8× o pior observado (8,4 s, no Montepio).

   ✅ **O tecto de concorrência por banco está feito** (2026-08-06): `internal/infra/lotacao`, **2 vagas por banco** em advisory locks de Postgres, o pedido a mais com `503 banco_ocupado` e não com uma oferta em falha. O número é o apertado já medido, e não o resultado de procurar onde um banco parte.

   ⏳ **Falta o prazo por banco.** Os cinco diferem **7×** no máximo observado (1,15 s no Novo Banco, 8,4 s no Montepio) e há um número só. ⚠️ **E não se diferencia com o que está medido:** 5 amostras dão um máximo, não um percentil, e cortar um banco lento que ia responder é pior do que esperar 10 s a mais. Quem lhe pegar sobe primeiro o `LATENCIA_AMOSTRAS` — o que custa pedidos aos bancos, e é por isso que não se fez de passagem.
2. **`KAN-58`** ✅ *(2026-08-07)* — cache em Postgres, chave = pedido exacto, com o pedido em claro **fora** do disco. ⚠️ Era a `KAN-8`, **apagada do `KAN` a 2026-08-06** na triagem da reversão: é trabalho por fazer e não trabalho morto, e uma chave nova foi o que restou para o repor.

   `internal/infra/cache`, tabela `respostas_em_cache`, `CACHE_VALIDADE` a **5 minutos**. A chave é o `SHA-256` de (versão, banco, pedido normalizado) — determinística para dois clientes iguais partilharem o acerto, e sem volta para a linha não dizer quem são. **Guarda-se o servido** (a resposta já traduzida) e não o objecto de domínio, para um acerto ser igual a uma resposta fresca por construção; o porquê e o preço estão na §4.

   ⚠️ **O acerto lê-se ANTES do tecto por banco**, e é a ordem que faz a cache valer alguma coisa: um acerto que gastasse vaga deixava dez clientes com o mesmo pedido em fila uns pelos outros — a levar `503` — por uma resposta que já estava em Postgres.

   ⚠️ **A validade continua por medir.** Os 5 min são um valor de partida, e a medição é uma **vigia** e não uma corrida: perguntar o mesmo ao mesmo banco de hora a hora e ver quando muda, sendo que o que decide é a **primeira** mudança e não a média.

   ⚠️ **Limite conhecido e escrito:** um `SHA-256` de um registo de baixa entropia confirma-se por tentativa. O resumo esconde o pedido de quem **lê** a tabela, não de quem o **adivinha** com a base na mão. Um HMAC com segredo fechava-o e traz gestão de chaves que este repositório ainda não tem.
3. `KAN-14` — o tecto por IP dimensionado para **N pedidos por comparação**, e o `PROXIES_DE_CONFIANCA` **medido**. ⚠️ Passa de dívida a bloqueante: é ele que separa um serviço de uma ferramenta de carga contra cinco bancos.
4. ~~`GET`~~ **`POST` `/api/v1/ofertas/{banco}`** no contrato ✅ *(2026-08-06)*, e a app a fazer o fan-out ⏳. ⚠️ O método mudou e não é detalhe: o pedido leva data de nascimento e rendimento, e num `GET` isso viajava na query string — histórico do browser, logs de qualquer proxy, `Referer`.
5. Retirar o que morreu: `sondagens`, a escala, os encargos, a grelha. ⚠️ **Depois** de a fatia ao vivo estar de pé, e não antes — apagar primeiro deixa o repositório sem nada que responda.

---

## A app React Native 🔶

Repositório separado, `simulador-v2-app`. Arrancou a 2026-07-28 com as fases 0-2 do backend fechadas — constrói-se contra um servidor a sério e não contra um falso.

| | |
|---|---|
| A1 esqueleto Expo, tokens, dois temas | ✅ |
| A2 contrato sincronizado + CI a reprovar divergência | ✅ |
| A3 os três passos do pedido | ✅ |
| A5 ofertas e detalhe, com fases, notas e pressupostos | ✅ |
| A6 os estados que não são o caminho feliz | ⏳ |
| A7 acessibilidade e **publicação web** — primeiro alvo a publicar | ⏳ |
| A8 EAS Build e submissão | ⛔ bloqueado pelo `KAN-24` |

⚠️ **A A4 VOLTA (2026-08-06).** Tinha sido apagada com a nota «não há espera nenhuma», e passa a haver: cada banco é um pedido e a lista enche-se à medida que respondem. Não é o ecrã de espera do v1 — não há trabalho assíncrono nosso a que se pergunte «já está?» —, é a lista a preencher-se. ⚠️ E traz consigo o fan-out **do lado da app**, com a responsabilidade de não disparar dez pedidos de uma vez.

⚠️ **A app impõe uma restrição bloqueante ao backend:** com uma app nas lojas não se controla quem actualiza, por isso `/api/v1` **só pode mudar por acrescento**. O caminho de «esta versão é demasiado antiga» custa pouco agora e é impossível de acrescentar quando já houver versões antigas no terreno — que é quando faz falta. **Por fazer.**

---

## Como se sabe que isto não voltou a entulhar

| indicador | v1 | limite no v2 | hoje |
|---|---|---|---|
| erros de lint fora do portão | 368 permanentes | **0**, sempre | 0 |
| ficheiros ad-hoc sem dono | 50+ em `scripts/` | não existe essa pasta | não existe |
| maior ficheiro de interface | 874 linhas num HTML | app noutro repositório | outro repositório |
| tabelas com nome de funcionalidade morta | 1 de 3 | **0** | 0 |
| tamanho da imagem | 4-8 GB | sem Chromium | **24,4 MB** |

⚠️ **Uma funcionalidade que não caiba na §1 do `ARQUITETURA.md` não entra sem essa secção ser alterada primeiro.** Foi por acrescento não decidido que o v1 ganhou autenticação e teve de a apagar em três migrações.

⚠️ **O portão não verifica documentos.** O `make gerado` compara o gerado com o spec — não os documentos com o spec, nem o servido com o spec. Foi assim que o `API.md` prometeu dias a fio uma rota que nunca existiu, que uma rota do contrato ficou sem handler (`KAN-44`), e que os cabeçalhos de defesa da §3 nunca existiram (`KAN-46`). Para o **servido** há agora teste. Para os **documentos** não há, e a regra é: quando um documento e o `api/openapi.yaml` discordarem, **o documento é que está errado**.
