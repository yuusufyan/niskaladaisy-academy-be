FROM golang:1.25-alpine AS base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# Local development: source is bind-mounted and air rebuilds on change.
FROM base AS dev
RUN go install github.com/air-verse/air@v1.61.7
CMD ["air", "-c", ".air.toml"]

FROM base AS build
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.20 AS prod
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
ENTRYPOINT ["api"]
