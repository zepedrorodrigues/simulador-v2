# Plano

Seis fases. Cada uma acaba em algo que **funciona e se pode ver a funcionar**. Nenhuma começa antes de a anterior ter o portão verde — a dívida do v1 acumulou-se por se passar à frente com «depois arruma-se».

O desenho está em `docs/ARQUITETURA.md`. O backlog é o projecto `KAN` do JIRA; não se abrem mais issues do GitHub para este repositório.

⚠️ **O trabalho cancelado fica escrito como cancelado, e não apagado.** Um plano onde o trabalho morto desaparece sem rasto é um plano onde alguém volta a propô-lo daqui a um mês.

---

## Fase 0 — Fundações ✅

Toolchain, quatro camadas com `depguard` a impor a regra de dependência, `docker-compose` com PostgreSQL, migrações `goose`, `sqlc`, `openapi.yaml` e a geração dos tipos, CI. `KAN-1` a `KAN-4`.

**Portão:** verde, com o `depguard` a reprovar de facto um `import` proibido — verificado por reversão, a nomear a camada.

## Fase 1 — A fatia vertical ✅

Quatro bancos de HTTP puro, escolhidos para exercitarem o máximo de dimensões ao mínimo custo: **CGD** (linha de base, períodos fixos descobertos em runtime), **Novo Banco** (produtos por omissão, erro estruturado como sinal, Euribor escolhível, finalidade muda o preço), **Montepio** (sessão `GET`→`POST`, sem taxa fixa, prova o «ajusta e anota»), **Banco CTT** (Euribor imposta e lida da resposta). `KAN-5`, `KAN-6`, `KAN-9`–`KAN-14`.

**Três coisas morreram aqui com a inversão da §1** (2026-07-25):

| | porquê |
|---|---|
| `KAN-7` fan-out do `comparar` | deixou de falar com bancos: lê a série e calcula localmente. O fan-out passou para o `varrimento`, que é onde o `-race` do portão ganha valor |
| `KAN-8` cache e *gate* em Redis | não há pedido ao banco no caminho do cliente. O *gate* é um advisory lock em Postgres |
| `KAN-15` quantização do pedido | existia para acertos de cache. ⚠️ A guarda de degrau **sobreviveu e ficou melhor**: o LTV é dimensão da grelha, logo o pedido cai no seu intervalo por construção |

## Fase 2 — Mercado ✅ *(menos um banco)*

O que o `viabilidade-imobiliaria` consome, com a fronteira congelada ao byte. `KAN-16`, `KAN-17`, `KAN-18`, `KAN-42` (teste de contrato que três documentos diziam existir e não existia), `KAN-44` (rota no contrato sem handler desde que o contrato existe).

⏳ `KAN-19` — Crédito Agrícola, o último de HTTP puro.

⚠️ A `KAN-16` cresceu muito para além de «grelha + varrimento» e é onde estão as medições que mudaram o desenho: escala de LTV por fronteiras medidas, a **base** da fixa em vez da TAN (`KAN-41`), o resíduo numa coluna, e a TAEG derivada dos encargos medidos.

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

## Fase 5 — Ecrã de mercado

`KAN-23` (Epic). A série do catálogo dentro da app. Fica para o fim de propósito: é a mais fácil de adiar e a mais fácil de fazer mal cedo demais.

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

⚠️ **A A4 não existe, e o buraco é de propósito:** era o ecrã de espera com sondagem, e não há espera nenhuma.

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
