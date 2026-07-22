# O portão. Corre-se inteiro — não um subconjunto.
#
# `gerar` (sqlc + oapi-codegen) entra aqui no issue que cria os ficheiros de que
# depende. Um alvo nasce quando se pode ver a funcionar.

.PHONY: verificar lint teste dev parar limpar

verificar: lint teste

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
