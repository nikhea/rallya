# Rallya

Multi-tenant event platform API. Organizations own tenants, members manage org-scoped events, tickets, orders, and door check-in — with Casbin enforcement, background email via River, and Stripe checkout.

Modular monolith in Go: Gin + Postgres + River queue + Casbin. Auth owns identity; organization owns tenants; IAM enforces; events and everything downstream are org-scoped.

```
Auth (identity) → Organization (tenants) → IAM (permissions) → Events → Ticketing → Orders → Attendees → Check-in
```

## Features

- **Auth** (`internal/auth`): register, email verify (link + OTP), login, refresh rotation, logout, password reset, `GET /auth/me` with org context. JWT access (15m) bound to revocable server-side sessions.
- **Organizations** (`internal/organization`): org CRUD, `OWNER > ADMIN > MEMBER` memberships, email invites (7d TTL), custom grant-only roles, slug-or-UUID routing, stealth 404s for outsiders.
- **IAM** (`internal/iam`): Casbin enforcer over Postgres, per-org policy seeding, superadmin bootstrap from env.
- **Events** (`internal/event`): draft → published lifecycle (cancel is terminal), public discovery, per-org slugs, cover uploads (Cloudinary or local disk), opt-in publish announcements.
- **Ticketing** (`internal/ticketing`): ticket types, pricing, and rules per event.
- **Orders + Payments** (`internal/order`, `internal/payment`): inventory claims, hold sweeper (River periodic job), Stripe Checkout + webhooks. Degrades to 503 when Stripe is unconfigured.
- **Attendees + Check-in** (`internal/attendee`, `internal/checkin`): door records minted from confirmations, QR-signed tickets, scan logging.
- **Cross-cutting**: audit trail (`internal/audit`), admin reads (`internal/admin`), rate limiting (`internal/ratelimit`, Redis-shared or in-memory), email jobs (`internal/notification`, River), caching (`internal/cache`, Redis-backed, nil-safe).

## Tech stack

| Layer | Choice |
|---|---|
| HTTP | Gin, `gin-swagger` docs at `/swagger/*any` |
| DB | Postgres (GORM), versioned SQL in `migrations/` via golang-migrate |
| Queue | River (in-process workers, `rivermigrate` tables, transactional enqueues) |
| AuthZ | Casbin + gorm-adapter |
| AuthN | HS256 JWT + opaque SHA-256-hashed tokens, bcrypt passwords |
| Payments | Stripe Checkout |
| Covers | Cloudinary when configured, else local `./uploads` |
| Rate limit / cache | Redis when `REDIS_URL` set, otherwise memory / disabled |
| Toolchain | Go 1.27, gofumpt, golangci-lint, air (`.air.toml`), Docker multi-stage |

## Quickstart

### Option A — Docker Compose (Postgres + Redis + migrate + API)

```bash
cp .env.example .env   # fill secrets; .env is gitignored, never committed
docker compose up --build
curl localhost:8080/health
```

Boot order is the release order: `db + redis → migrate → api`. The API never migrates on boot.

```bash
docker compose down -v   # full reset (drops pgdata + redisdata)
```

### Option B — Local Go (needs Postgres + optional Redis)

```bash
cp .env.example .env
# point DATABASE_URL at your Postgres, REDIS_URL at Redis (or leave unset)
go run ./cmd/migrate up   # or: make migrate-up
go run ./cmd/api          # or: make api / air for live reload
```

Health check: `GET /health`. API base path: `/api/v1`. Swagger UI: `http://localhost:8080/swagger/index.html`.

## Configuration

All config is env-driven (`cmd/config`, see `.env.example`). Key variables:

| Var | Purpose | Default |
|---|---|---|
| `DATABASE_URL` | Postgres DSN (fatal if unreachable) | compose network URL in `compose.yaml` |
| `APP_PORT` / `APP_URL` | listen port / public URL (covers, Stripe redirects) | `8080` |
| `JWT_SECRET` | HS256 secret, fail-closes unless 32+ bytes | — (required) |
| `QR_SIGNING_SECRET` | ticket QR HMAC secret, fail-closes | — (required) |
| `SUPERADMIN_EMAILS` | comma-separated bootstrap superadmins | empty |
| `CORS_ALLOWED_ORIGINS` | unset = deny cross-origin (fail closed); `*` = no credentials | unset |
| `TRUSTED_PROXIES` | explicit proxies for real client IPs | unset (RemoteAddr) |
| `RATE_LIMIT_AUTH_PER_MIN` / `RATE_LIMIT_DEFAULT_PER_MIN` | auth (strict) vs default tiers | `10` / `600` |
| `REDIS_URL` | shared rate limiter + cache; unset = memory/disabled | unset |
| `EMAIL_*` | SMTP; unset = logged only, requests still succeed | unset |
| `STRIPE_SECRET_KEY` / `STRIPE_WEBHOOKS_SIGNING_SECRET` | unset = checkout 503s, boot unaffected | unset |
| `CLOUDINARY_URL` (or `CLOUD_*`) | unset = local disk under `./uploads` | unset |
| `ALLOW_INSECURE_JWT` | local-dev escape hatch only, never in prod | `false` |

Never commit `.env`, `tmp/`, or `uploads/`. Fixtures use placeholder secrets.

## Migrations

`migrations/*.up.sql` is the single source of truth. `cmd/migrate` applies golang-migrate (app tables) then `rivermigrate` (queue tables) in one command.

```bash
make migrate-up        # apply pending (release step, dev and prod)
make migrate-down      # roll back one
make migrate-version   # report both engines
make migrate-force VERSION=1   # only after manually fixing a dirty DB
```

Rules: never edit an applied file — add a new version. Never `AutoMigrate` outside tests.

## API docs

- Swagger annotations on every route; regenerate after behavior changes:
  `swag init -g cmd/api/main.go -o docs` (never hand-edit `docs/`).
- Per-module `internal/<domain>/README.md` explains flows and tables — read the nearest one before touching a domain.

## Development

Gates (all must pass before finishing):

```bash
make fmt-check && make lint && make vet && make build && make test
```

Helpers: `make fmt`, `make lint-fix`. Tests use in-memory SQLite per suite (`SetMaxOpenConns(1)`) and also pass under `-race`.

Conventions:

- **Seams, not imports**: domains talk through consumer-declared interfaces (`UserReader`, `GroupSyncer`, `PolicySeeder`, `OrgResolver`). Wiring happens in `cmd/api/main.go` only. No cross-domain table joins.
- **DTOs live in `dto/`** with unique package names per domain (`dto`, `orgdto`, `eventdto`, …).
- **Errors**: services return sentinels (`errors.go`); handlers map to HTTP with `{error}` / `{error, code}` envelopes and stealth 404s for outsiders.
- Branch per module (`feature/<name>`); commit on request only, never push unless asked.

## Layout

```
cmd/api/main.go        thin wiring: env → DB → services → routes → serve
cmd/migrate/main.go    the ONLY schema path (app + River tables)
cmd/config/            env-driven handles (DB, Redis, JWT, Stripe, mail, covers)
internal/<domain>/     model, repository, service, handler, dto, routes.go
migrations/            versioned SQL, single source of truth
docs/                  generated Swagger (do not hand-edit)
compose.yaml           local stack: db + redis + migrate + api
Dockerfile             multi-stage: /out/migrate owns schema, /out/api serves
uploads/               local cover storage (gitignored)
tmp/                   air build output (gitignored)
```

## License

Proprietary — all rights reserved. (Add a `LICENSE` file if this changes.)
