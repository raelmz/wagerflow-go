# syntax=docker/dockerfile:1
#
# Build único, multi-stage, para os 4 binários do projeto. Um único
# Dockerfile (em vez de 4 separados) porque todos compartilham o mesmo
# módulo Go e as mesmas dependências — repetir o estágio de build 4x
# só duplicaria cache e tempo de build sem ganho nenhum. O binário
# final de cada container é escolhido pelo ARG BIN, setado por
# `target`/`build.args` em cada serviço do docker-compose.yml.
#
# Mesma versão de Go do go.mod (ver comentário abaixo) para evitar
# qualquer diferença de comportamento entre o que o desenvolvedor
# testou localmente e o que roda em container.

# --- Estágio 1: build ---
# go.mod pede `go 1.27.1`. golang:1.27 ainda não existe como tag oficial
# no Docker Hub no momento deste build (a série 1.27 do Go é recente);
# usamos golang:1.25 (última minor estável publicada) + GOTOOLCHAIN=auto,
# que faz o próprio `go build` baixar e usar o toolchain 1.27.1 exigido
# pelo go.mod automaticamente (mecanismo nativo do Go desde 1.21). Isso
# evita depender de uma tag de imagem que pode não existir ainda.
FROM golang:1.25-bookworm AS build

WORKDIR /src

ENV GOTOOLCHAIN=auto \
    CGO_ENABLED=0

# Copia só o necessário para `go mod download` primeiro, para o cache
# de camada do Docker não invalidar a cada mudança de código-fonte.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Compila os 4 binários numa única camada. Ficam todos disponíveis na
# imagem de build; cada imagem final (estágio 2) copia só o que usa.
RUN go build -o /out/api ./cmd/api && \
    go build -o /out/outbox-publisher ./cmd/outbox-publisher && \
    go build -o /out/wager-consumer ./cmd/wager-consumer && \
    go build -o /out/pending-reference-worker ./cmd/pending-reference-worker

# --- Estágio 2: imagem final ---
# distroless em vez de alpine: menor superfície de ataque (sem shell,
# sem gerenciador de pacotes) e o binário Go é estático (CGO_ENABLED=0),
# então não precisa de libc nenhuma.
FROM gcr.io/distroless/static-debian12:nonroot AS final

# ARG que decide qual binário vai para esta imagem. Cada serviço do
# docker-compose.yml passa um valor diferente via `build.args`.
ARG BIN=api

WORKDIR /app
COPY --from=build /out/${BIN} /app/app

# Não dá para usar ${BIN} dentro de ENTRYPOINT em forma exec direto de
# forma limpa com ARG, então padronizamos o nome do binário copiado
# para /app/app e todo serviço aponta para o mesmo ENTRYPOINT.
ENTRYPOINT ["/app/app"]
