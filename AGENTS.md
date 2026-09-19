# AGENTS.md — Rallya (multi-tenant event platform, Go)

Modular monolith: Gin + Postgres + River queue + Casbin. Auth owns identity;
organization owns tenants; IAM enforces; events are org-scoped. Dependency
flows one way: `Events → Organization/IAM → Auth`. Never invert it.

## Layout

- `cmd/api/main.go` — thin wiring only (env → DB → services → routes). No logic.
- `cmd/migrate/main.go` — the ONLY schema path (`go run ./cmd/migrate up`).
  Runs golang-migrate (app) + rivermigrate (queue) before boot, dev and prod.
- `internal/<domain>/{model,repository,service,handler,dto,routes.go}` (+ `token/`, `utils/` where present).
- `migrations/*.up.sql` — single source of truth. Never edit an applied file;
  add a new version. Never `AutoMigrate` outside tests.
- `docs/` — generated Swagger. Never hand-edit; regen with `swag init -g cmd/api/main.go -o docs`.
- `uploads/` — local cover storage (gitignored). `tmp/` — air build output.

## Gates (all must pass before finishing)

```bash
make fmt-check && make lint && make vet && make build && make test
```

`make fmt` (gofumpt), `make lint-fix`, `make migrate-up/down/version` as needed.
Tests: in-memory SQLite per suite. Full suite also passes under `-race`.

## Universal rules

- **Seams, not imports**: domains talk through consumer-declared interfaces
  (`UserReader`, `GroupSyncer`, `PolicySeeder`, `OrgResolver`). No cross-domain
  table joins; no service-to-service imports. Wiring happens in `cmd/api` only.
- **DTOs live in `dto/`**: no wire/response shapes in service/repository/handler
  files. Mapping helpers stay private in service. DTO package names must be
  unique per domain (`dto`, `orgdto`, `eventdto`) — swag fails on duplicates.
- **Errors**: service returns sentinels (`errors.go`); handlers map to HTTP.
  Auth-style envelopes: `{error}` / `{error, code}`; stealth 404s for outsiders.
- **Secrets**: never read, print, or commit `.env` contents (it holds real Gmail
  credentials). Fixtures use placeholders (`Str0ngP@ssw0rd!`). Scan diffs for
  secrets before committing. Never commit `.env`, `tmp/`, `uploads/`.
- **Branch per module** (`feature/<name>`); commit messages match repo style
  (`added <thing>`); commit on request only, never push unless asked.
- **Docs**: per-module `README.md` explains flows; Swagger annotations (with
  examples) on every route; keep both current with behavior changes.

## Known traps (learned the hard way)

- SQLite `:memory:` is per-connection: test DBs need `SetMaxOpenConns(1)`, and
  repo methods called inside a tx must take the tx handle (never `r.db`).
- Casbin: grouping ops for superadmin MUST use `Named` variants with `"g2"`
  (unnamed defaults to `g` and fails). Owner wildcard needs `(p.obj == "*")`
  style matcher arms. Serialize all enforcer access (reads RLock, writes Lock).
- GORM tags must mirror `migrations/` (composite uniques, defaults) — drift
  between them has bitten twice. `ILIKE` is Postgres-only; use `LOWER() LIKE`.
- `river.Job[T]` embeds `*JobRow`: construct test jobs with a non-nil row.
- `pkill -f <pattern>` matches its own command line — use `pkill -x <name>`.

## Per-domain notes live next to the code

Read the nearest `AGENTS.md` before touching a domain: `internal/auth/`,
`internal/organization/`, `internal/iam/`, `internal/event/`,
`internal/notification/`, `internal/media/`, `cmd/`.
