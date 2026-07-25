# O portão. Corre-se inteiro — não um subconjunto.
#
# `gerar` reúne as três gerações — sqlc, oapi-codegen e openapi-typescript — e o
# alvo `gerado` reprova se qualquer uma ficar para trás. Um alvo nasce quando se
# pode ver a funcionar: o sqlc entrou no KAN-2, os dois do contrato entram agora
# (KAN-3), com o api/openapi.yaml.

.PHONY: verificar gerado lint teste gerar dev parar limpar

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

# PostgreSQL e Redis locais. O `--wait` espera pelos healthchecks do
# `docker-compose.yml`, e esses são literalmente o `pg_isready` e o `redis-cli
# ping` — não um `sleep`, que dá verde antes de o serviço existir.
#
# Idempotente por construção: com a configuração inalterada, o `up -d` não
# recria nada e o `--wait` devolve mal os healthchecks estejam verdes.
dev:
	docker compose up -d --wait --wait-timeout 120

# Pára os serviços e guarda os dados.
parar:
	docker compose down

# ⚠️ Leva o volume do PostgreSQL à frente. É como se volta ao estado de máquina
# limpa — e é o que se corre antes de medir um arranque de raiz.
limpar:
	docker compose down -v
