# syntax=docker/dockerfile:1
# BuildKit syntax pin: required for the --mount=type=cache flags below.

# ---------------------------------------------------------------------------
# air (dev tooling only, never shipped to production)
# Built from a clean base so changing go.mod/go.sum doesn't force a reinstall;
# the cache mount also keeps go.mod from affecting air's own module downloads.
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS air
RUN --mount=type=cache,target=/go/pkg/mod \
    go install github.com/air-verse/air@v1.61.7

# ---------------------------------------------------------------------------
# dev (local hot-reload only, never shipped to production)
# Source is bind-mounted at runtime (see docker-compose.dev.yml), so this
# stage only needs go.mod/go.sum ahead of time to warm the module cache
# before the container's first build.
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS dev
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY --from=air /go/bin/air /usr/local/bin/air
CMD ["air", "-c", ".air.toml"]

# ---------------------------------------------------------------------------
# Production image: classic 2-stage build (compile, then copy the binary
# into a minimal runtime image so the toolchain/source never ship).
# ---------------------------------------------------------------------------

# Stage 1/2 - build: compile a static binary.
FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
# CGO_ENABLED=0: static binary, runs on scratch-like alpine without glibc.
# -s -w: strip debug symbols to shrink the binary.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/api ./cmd/api

# Stage 2/2 - prod: only the compiled binary + runtime essentials ship.
FROM alpine:3.20 AS prod
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
ENTRYPOINT ["api"]
