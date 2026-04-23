# Dockerfile — shared multi-binary image for csw-server / csw-worker / csw.
#
# The Go monorepo ships three binaries: csw-server (HTTP API),
# csw-worker (asynq worker), and csw (CLI: migrate, openapi-dump).
# They share a dependency graph, so we build all three in one
# builder stage and copy them into a single runtime image. docker-
# compose picks which entrypoint runs via the `command:` field —
# the same image serves every Go process in the stack.
#
# Build args let the image pin a specific Go toolchain without
# editing the Dockerfile; CI tags builds with the commit SHA.

# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.25.0

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

# Leverage the module cache: copy go.mod/go.sum first, resolve deps,
# then copy source. Subsequent builds only re-fetch modules when
# go.sum changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/csw-server ./cmd/csw-server && \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/csw-worker ./cmd/csw-worker && \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/csw ./cmd/csw

# Runtime stage is as small as we can get while retaining a shell
# for the migrate-then-run entrypoint dance the compose stack uses.
# alpine:3.20 pulls in libs (ca-certificates, tini) that scratch
# wouldn't have, which matters for TLS-verified outbound calls.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tini \
    && addgroup -S csw && adduser -S csw -G csw

WORKDIR /app

COPY --from=builder /out/csw-server /usr/local/bin/csw-server
COPY --from=builder /out/csw-worker /usr/local/bin/csw-worker
COPY --from=builder /out/csw /usr/local/bin/csw

USER csw

ENTRYPOINT ["/sbin/tini", "--"]
# Default command: server. Compose overrides with `command:` for
# the worker / migrate invocations.
CMD ["csw-server"]
