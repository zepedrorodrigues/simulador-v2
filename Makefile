# O portão. Corre-se inteiro — não um subconjunto.
#
# `dev` (docker-compose) e `gerar` (sqlc + oapi-codegen) entram aqui nos issues
# que criam os ficheiros de que dependem. Um alvo nasce quando se pode ver a
# funcionar.

.PHONY: verificar lint teste

verificar: lint teste

# `go tool` e não `golangci-lint`: o binário do PATH pode ser um v1, que não lê
# o .golangci.yml v2 deste repositório.
lint:
	go tool golangci-lint run ./...

# O -race não é opcional: o modelo é fan-out concorrente.
teste:
	go test -race ./...
