MIGRATE := go run ./cmd/migrate
FMT_DIRS := internal cmd migrations

.PHONY: migrate-up migrate-down migrate-version migrate-force test build run api fmt fmt-check vet lint lint-fix

# Release step: apply pending migrations, then boot the API.
# Dev and prod run the exact same command against their own DATABASE_URL.
migrate-up:
	$(MIGRATE) up

migrate-down:
	$(MIGRATE) down

migrate-version:
	$(MIGRATE) version

# Usage: make migrate-force VERSION=1 (only after manually fixing a dirty DB)
migrate-force:
	$(MIGRATE) force $(VERSION)

test:
	go test ./...

build:
	go build ./...

api:
	go run ./cmd/api

# Prettier equivalent: format in place.
fmt:
	go tool gofumpt -w $(FMT_DIRS)

# CI gate: fail on any formatting drift (generated docs/ excluded by config).
fmt-check:
	@test -z "$$(go tool gofumpt -l $(FMT_DIRS))" || (echo "format drift:"; go tool gofumpt -l $(FMT_DIRS); exit 1)

vet:
	go vet ./...

# ESLint equivalent: config in .golangci.yml.
lint:
	go tool golangci-lint run ./...

lint-fix:
	go tool golangci-lint run --fix ./...
