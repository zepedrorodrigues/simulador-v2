# Deploy

A forma de produção: **um VPS**, com Caddy à frente, o serviço e o Postgres na
mesma máquina e na mesma rede interna. `compose.producao.yml`.

⚠️ **Nada aqui está executado.** Escrito a 2026-08-07, quando o alojamento
deixou de estar adiado; o `fly.toml` fica como alternativa e também nunca correu.
Quem correr isto pela primeira vez **actualiza este documento com o que
divergiu** — é a única forma de ele não passar a ficção.

## Porquê um VPS com IP dedicado, e não uma PaaS

Não é preferência: é o que este serviço faz. **Cada comparação são ~10 pedidos
aos simuladores públicos de cinco bancos, e o IP de saída é o que eles vêem.**

- Um **IPv4 dedicado** é uma reputação que é nossa e estável. Uma faixa de saída
  partilhada de uma PaaS é uma reputação de terceiros — e basta outro inquilino
  para a estragar.
- Com o Caddy na mesma máquina, o `PROXIES_DE_CONFIANCA` passa a ser **um
  endereço estável da rede do compose**, medido uma vez. Atrás do proxy de uma
  PaaS é uma faixa que pode mudar sem aviso.
- Não há *cold start*. Numa plataforma que adormece, a espera soma-se à do banco
  — e a cauda medida do Montepio é de **8,4 s** (`DOSSIE-BANCOS.md`).

## O que é preciso antes

1. Um VPS Linux com Docker e o plugin `compose`. O perfil mais pequeno chega: o
   serviço passa o tempo **à espera dos bancos**, não a calcular. ⚠️ Isto **não
   está medido em produção** — se as respostas ficarem lentas, é o primeiro
   número a olhar.
2. Um domínio a apontar para o IP da máquina, **já a resolver**. O Caddy pede o
   certificado no arranque por HTTP-01; se o DNS ainda não vier cá ter, o desafio
   falha e há limite de tentativas por domínio por semana.
3. As portas 80 e 443 abertas. **E mais nenhuma** — em particular a 5432 não, e
   o compose não a publica.

## Pôr de pé

```bash
git clone … && cd simulador-v2
cp .env.producao.example .env.producao
# preencher: DOMINIO, EMAIL_ACME, PG_PASSWORD
docker build -t simulador-v2:local .
docker compose -f compose.producao.yml --env-file .env.producao up -d
```

O compose corre as migrações como **passo próprio** (serviço `migrar`) e só
arranca o `servir` depois de elas saírem bem. ⚠️ Não há auto-migração, e o
binário recusa-se a servir contra uma base por migrar — é a §4 do
`ARQUITETURA.md` a valer em produção.

```bash
curl -fsS https://<dominio>/healthz && echo ok
```

## O `PROXIES_DE_CONFIANCA` — já não se mede, confirma-se

Sem ele, o tecto por IP conta pelo endereço da ligação — que é o Caddy, **igual
para toda a gente**. O tecto do site inteiro passa a ser o de um utilizador, e o
primeiro visitante tranca os restantes. **É literalmente o bug de produção do
v1** (`ARQUITETURA.md` §7.5).

⚠️ **Era isto que bloqueava a `KAN-14`, e deixou de bloquear** — não por se ter
medido, mas por o valor ter passado a ser **conhecido de antemão**: o
`compose.producao.yml` dá ao Caddy um endereço **fixo**, `172.30.0.2`, e o
omissão do `PROXIES_DE_CONFIANCA` é esse. Não há primeira medição a fazer.

**Porque é que não se mede** — as duas razões saíram de correr isto a
2026-08-07:

1. **O diário não regista o endereço do cliente**, só o método, o caminho e o
   estatuto. Não há de onde o ler.
2. **Os endereços atribuídos pelo Docker mudam quando um contentor é recriado.**
   Um valor medido uma vez apodrecia no primeiro `up` — **em silêncio, e do lado
   mau**: o tecto voltava a contar toda a gente como um só.

**O que se faz é confirmar**, e o arranque di-lo:

```
tecto por IP a confiar no X-Forwarded-For de: 172.30.0.2/32
```

```bash
docker compose -f compose.producao.yml logs simulador | grep "tecto por IP"
```

⚠️ **Se aparecer «ligado pelo endereço da ligação», o tecto está a contar toda a
gente como um só.** É o bug do v1 a correr, e não dá erro nenhum.

⚠️ E confirma-se que o endereço é mesmo o do Caddy:

```bash
docker network inspect simulador-v2-producao_interna \
  -f '{{range $k,$v := .Containers}}{{$v.Name}} {{$v.IPv4Address}}{{println}}{{end}}'
```

**Confirmar o comportamento:** um `X-Forwarded-For` forjado de fora **não pode**
mudar a contagem — o Caddy substitui-o antes de chegar cá (medido; ver o
`Caddyfile` e `internal/infra/web/proxy_integracao_test.go`).

## Backups

A base é a única coisa nesta máquina que não se reconstrói: os parsers e as
capturas estão no git, e a cache é descartável por definição.

```bash
docker compose -f compose.producao.yml exec -T postgres \
  pg_dump -U "$PG_UTILIZADOR" "$PG_BASE" | gzip > backup-$(date +%F).sql.gz
```

⚠️ **Um backup que nunca se restaurou não é um backup.** O restauro foi
executado uma vez em local a 2026-08-01; em produção, executa-se **antes** de ser
preciso, contra uma base descartável.

⚠️ E hoje a base guarda **contadores de tecto e respostas em cache**, e mais
nada — nenhum dado pessoal (§4). Perdê-la custa uma janela de tecto e uns
acertos de cache, não um cliente. Isso muda no dia em que passar a guardar outra
coisa, e a §4 é que decide.

## Actualizar

```bash
git pull && docker build -t simulador-v2:local .
docker compose -f compose.producao.yml --env-file .env.producao up -d
```

⚠️ **Uma tag fixa e não `latest`** (`IMAGEM` no `.env.producao`): sem ela não há
como saber o que está a correr nem como voltar atrás — e voltar atrás é o que se
quer poder fazer às duas da manhã.

## O que este documento NÃO resolve

- ⏳ **A ordem, que não é uma opção deste ficheiro.** **Subir a máquina não é
  publicar o serviço**: a ordem certa é ter isto de pé, medido e fechado antes de
  apontar o DNS para quem quer que seja.
- ⏳ **O prazo por banco.** Continua um só (15 s) para cinco bancos que diferem
  **7×** na cauda. Diferenciá-lo pede mais amostras (`LATENCIA_AMOSTRAS`), que
  custam pedidos aos bancos.
- ⏳ **A validade da cache.** Os 5 min são valor de partida. A vigia que a mede
  quer uma máquina sempre ligada — e esta passa a ser uma.
