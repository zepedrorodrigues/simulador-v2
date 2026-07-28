# A imagem do serviço (KAN-22).
#
# ⚠️ **Sem Chromium, e é o maior ganho da reescrita.** O v1 precisava de 4-8 GB
# de imagem, e quase tudo era o browser que quatro scrapers usavam. Aqui não há
# banco de browser nenhum implementado, e o dia em que houver (KAN-20/21) é o dia
# em que se decide se ele entra nesta imagem ou corre como serviço à parte — a
# §5 já diz que a segunda saída está em cima da mesa. Até lá, o que não se
# instala não se mantém, não se actualiza e não tem CVEs.
#
# ⚠️ **Uma imagem, os quatro subcomandos.** O mesmo binário serve, varre, migra e
# reverte (`cmd/simulador`). Não há imagem do varrimento à parte: seriam duas
# coisas para manter em dia, e o que as distingue é um argumento.

# --- compilar -----------------------------------------------------------------------

# ⚠️ A versão vem do go.mod, e não de uma tag escolhida à mão. `GOTOOLCHAIN=local`
# faz o build FALHAR se a imagem e o go.mod discordarem, em vez de descarregar
# outra toolchain em silêncio — é a mesma decisão que o portao.yml já toma.
FROM golang:1.26.5-alpine AS construir
ENV GOTOOLCHAIN=local

WORKDIR /src

# ⚠️ Os manifestos primeiro, e só depois o código. É o que faz a camada das
# dependências ser reaproveitada quando só o código muda — que é quase sempre.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# ⚠️ `CGO_ENABLED=0` porque a imagem final é `static`, sem libc nenhuma. Sem isto
# o binário liga-se dinamicamente à musl da alpine e não arranca lá.
#
# ⚠️ `-trimpath` tira os caminhos da máquina de quem compilou. Não é estética: um
# stack trace em produção não tem por que dizer que o código vive em
# `/c/Users/...`.
ENV CGO_ENABLED=0 GOOS=linux
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags='-s -w' -o /simulador ./cmd/simulador

# --- correr -------------------------------------------------------------------------

# ⚠️ `static` e não `alpine`: sem shell, sem gestor de pacotes, sem busybox. O que
# não está na imagem não pode ser usado por quem lá entrar. O custo é não haver
# `docker exec ... sh` para depurar — e é por isso que o diário de pedidos existe
# (KAN-43): a depuração é por logs, não por sessão dentro do contentor.
#
# ⚠️ `:nonroot` põe o processo a correr como uid 65532. Um processo que não é root
# não escreve no sistema de ficheiros da imagem nem se liga a portas abaixo de
# 1024 — que é a razão de a porta ser 8080 e não 80.
FROM gcr.io/distroless/static-debian12:nonroot

# Os certificados vêm da imagem base, e são precisos: o varrimento fala HTTPS com
# os simuladores dos bancos.
COPY --from=construir /simulador /simulador

# ⚠️ 0.0.0.0 aqui e só aqui. O default do binário é o loopback de propósito
# (`web.EnderecoOmissao`) — um bind em todas as interfaces punha o serviço na
# internet no dia em que alguém o corresse num VPS sem o ter decidido. Dentro de
# um contentor a decisão está tomada: o que escuta no loopback não recebe nada.
ENV ENDERECO_HTTP=0.0.0.0:8080
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/simulador"]
CMD ["servir"]
