# O portão. Corre-se inteiro — não um subconjunto.
#
# `gerar` reúne as três gerações — sqlc, oapi-codegen e openapi-typescript — e o
# alvo `gerado` reprova se qualquer uma ficar para trás. Um alvo nasce quando se
# pode ver a funcionar: o sqlc entrou no KAN-2, os dois do contrato entram agora
# (KAN-3), com o api/openapi.yaml.

.PHONY: verificar gerado lint teste teste-rede teste-fidelidade medicao gerar dev parar limpar

verificar: gerado lint teste

# Tudo o que é gerado e versionado. O `gerado` compara estes caminhos face ao
# committado; se `gerar` os mexer, o portão apanha-o.
GERADOS := internal/infra/bd api/api.gen.go api/tipos-app.d.ts

# node_modules reposto quando o lock muda — é só o openapi-typescript fixado no
# package-lock.json. `npm ci` instala exactamente o lock, sem o mexer.
node_modules: package-lock.json
	npm ci
	@touch node_modules

# `go tool` e não os binários do PATH: as versões estão fixadas no bloco `tool`
# do go.mod. Cada gerador lê a sua fonte e escreve código gerado, nunca editado
# à mão:
#   sqlc              db/sqlc.yaml         → internal/infra/bd/
#   oapi-codegen      api/openapi.yaml     → api/api.gen.go     (package api)
#   openapi-typescript api/openapi.yaml    → api/tipos-app.d.ts (tipos da app)
#
# A saída do openapi-typescript é `.d.ts` e não `.ts` porque não tem uma única
# linha de runtime — só `interface` e `type`. É a convenção de ficheiro de
# declarações do TypeScript, e é a extensão que a documentação do próprio
# openapi-typescript usa nos exemplos.
gerar: node_modules
	go tool sqlc generate -f db/sqlc.yaml
	go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	npx openapi-typescript api/openapi.yaml -o api/tipos-app.d.ts

# O gerado é versionado e tem de estar em dia: regenera e reprova se sobrar
# qualquer diferença face ao que está committado. É o portão que apanha quem
# altera uma query ou o openapi.yaml e se esquece de gerar — sem ele, a fonte e
# o código gerado divergem em silêncio. `--porcelain` apanha tanto o ficheiro
# alterado (rastreado) como um ficheiro novo por acrescentar (não rastreado).
gerado: gerar
	@test -z "$$(git status --porcelain -- $(GERADOS))" || { \
		echo "código gerado desactualizado — corre 'make gerar' e committa o gerado:"; \
		git status --porcelain -- $(GERADOS); \
		exit 1; }

# `go tool` e não `golangci-lint`: o binário do PATH pode ser um v1, que não lê
# o .golangci.yml v2 deste repositório.
lint:
	go tool golangci-lint run ./...

# O -race não é opcional: o modelo é fan-out concorrente.
teste:
	go test -race ./...

# Os três alvos que tocam a rede dos bancos. NÃO entram no portão: dependem de
# servidores de terceiros estarem de pé, e um portão que amarela por causa disso
# deixa de ser lido.
#
# A tag é a forma escolhida de propósito, e não `testing.Short()`: assim o
# comando do portão não muda uma vírgula — continua a ser `go test -race ./...`
# — e não há caminho por onde estes testes corram sem se pedirem.
#
# ⚠️ **São três e não um porque custam ordens de grandeza diferentes** (KAN-47).
# Até 2026-08-02 os três corriam sob a tag `rede`, e um `make teste-rede` pedido
# para confirmar parsers disparava o cartesiano contra a CGD. O custo em pedidos
# a terceiros está escrito em cada alvo, e é ele que decide qual se corre:
#
#   alvo              tag          pedidos a terceiros    duração
#   teste-rede        rede         dezenas                minutos
#   teste-fidelidade  fidelidade   ~2000 (250 por banco)  até 3h
#   medicao           medicao      >1000, só à CGD        1h+

# Confirmar que um parser ainda corresponde ao que o banco devolve. É o alvo que
# se corre quando se suspeita que um banco mudou por baixo de nós.
#
# Custo: **dezenas de pedidos**, repartidos por cinco bancos — cada teste faz
# um punhado de simulações. Minutos.
teste-rede:
	go test -race -tags rede ./...

# A fidelidade: entre o corpo que chegou pela rede e o dominio.Oferta que sai do
# Simular, perdeu-se ou torceu-se alguma coisa?
#
# ⚠️ Custo: **~2000 pedidos**, e é o alvo mais caro em pedidos que há aqui. São
# 250 amostras por banco (`<BANCO>_AMOSTRAS`), e o custo por simulação está
# medido no DOSSIE-BANCOS.md — 1,00 pedido no Banco CTT, 2,03 no Montepio, 4,00
# no Santander. Logo o Santander sozinho são 1000 pedidos.
#
# O tempo-limite é o do banco mais lento: o Montepio paga o arranque que fixa os
# cookies em cada simulação e o cabeçalho do seu teste prescreve 180m.
#
# ⚠️ Uma afirmação de 99,9 % precisa de alguns milhares de amostras, não de 250.
# Isso corre-se banco a banco com `<BANCO>_AMOSTRAS=3000`, agendado, e não por
# este alvo.
teste-fidelidade:
	go test -race -tags fidelidade -timeout 180m ./...

# As medições de desenho contra a CGD: o cartesiano (KAN-16) e o e2e da grelha
# inteira. NÃO são confirmação de parser — são a pergunta «o desenho da grelha
# aguenta-se?», e respondem-se uma vez, não a cada sessão.
#
# ⚠️ Custo: **mais de mil pedidos ao simulador público da CGD**, ao longo de mais
# de uma hora. É o único teste deste repositório que carrega um sistema de
# terceiros durante mais de uma hora, e o «Reduzir a carga nos bancos ao mínimo
# que funciona» (§2 do docs/USO-RESPONSAVEL.md) aplica-se-lhe inteiro:
# **corre-se em hora morta, e a hora escolhe-se antes de o disparar.**
#
# O tempo-limite vem do alvo e não da memória de quem o corre — era o que
# faltava quando isto vivia na tag `rede` e morria aos 10m por omissão.
medicao:
	go test -race -tags medicao -timeout 90m -v ./internal/aplicacao/varrimento/

# O PostgreSQL local. O `--wait` espera pelo healthcheck do
# `docker-compose.yml`, e esse é literalmente o `pg_isready` — não um `sleep`,
# que dá verde antes de o serviço existir.
#
# Idempotente por construção: com a configuração inalterada, o `up -d` não
# recria nada e o `--wait` devolve mal o healthcheck esteja verde.
dev:
	docker compose up -d --wait --wait-timeout 120

# Pára o serviço e guarda os dados.
parar:
	docker compose down

# ⚠️ Leva o volume do PostgreSQL à frente. É como se volta ao estado de máquina
# limpa — e é o que se corre antes de medir um arranque de raiz.
limpar:
	docker compose down -v
