# syntax=docker/dockerfile:1

# Rallya API — multi-stage image.
# Migrations are embedded in the binary (migrations.FS), so the runtime
# image needs no source checkout: /out/migrate owns schema, /out/api serves.

FROM golang:1.27-alpine AS builder

WORKDIR /src

# Module deps first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

COPY . ./

# Static binaries; CGO off (no cgo deps: pgx stdlib, lib/pq pure Go paths).
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate

FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S app \
 && adduser -S app -G app \
 && mkdir -p /app/uploads \
 && chown app:app /app/uploads

WORKDIR /app

COPY --from=builder /out/api /out/migrate /out/

USER app

EXPOSE 8080

# Overridden per service in compose.yaml (migrate: up, api: serve).
CMD ["/out/api"]
