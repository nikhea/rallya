# Config (`cmd/config`)

Process-wide configuration and shared clients. Every helper reads the environment
(loaded from `.env` by `LoadEnv`); nothing is hardcoded. The package owns no
domain logic — it builds the handles that `cmd/api` wires into domains.

## Files

| File | Owns |
|---|---|
| `env.go` | `LoadEnv()` — loads `.env` via godotenv (missing file is info-level, not fatal) |
| `logger.go` | `InitLogger()` — process-wide JSON `slog` on stdout at Debug |
| `database.go` | Shared Postgres handles for GORM **and** River (see below) |
| `auth.go` | JWT secret/TTLs + public app URL |
| `mail.go` | SMTP settings + enabled check |
| `redis.go` | Optional Redis client (nil-safe disabled mode) |

## Database (`database.go`)

One `*sql.DB` (pgx-stdlib driver) is shared so GORM and River enqueue on the same
pool — and so River jobs can join GORM transactions (see Auth README):

- `DB *gorm.DB` — domain repositories.
- `SQLDB *sql.DB` — River's `riverdatabasesql` driver + migrators.
- `ListenerPool *pgxpool.Pool` — River LISTEN/NOTIFY only (`database/sql` cannot
  LISTEN). Nil when unreachable → River polls (correct, slower pickup).

Pool: 10 idle / 100 open / 1h lifetime. `Ping` at boot — database failure is
**fatal** (unlike Redis). `CloseDatabase()` releases pool + handle; call via defer
in `main`.

## Auth (`auth.go`)

| Helper | Env | Default | Notes |
|---|---|---|---|
| `JWTSecret()` | `JWT_SECRET` | — (fatal) | Must be 32+ random bytes. Missing/known-dev/short values `os.Exit(1)` unless `ALLOW_INSECURE_JWT=true` (local dev only). Generate: `openssl rand -hex 32` |
| `AccessTTL()` | `ACCESS_TTL_MINUTES`, legacy `JWT_TTL_HOURS` | 15m | Access JWT lifetime |
| `RefreshTTL()` | `REFRESH_TTL_DAYS` | 30d | Refresh row + session lifetime |
| `JWTTTL()` | — | = `AccessTTL()` | Backward-compat shim |
| `SuperAdminEmails()` | `SUPERADMIN_EMAILS` | empty (none) | Comma-separated platform superadmin emails, lowercased; empty never wipes existing grants |
| `AppURL()` | `APP_URL` | — (empty) | Base for email links; no hardcoded default, set per environment |

## CORS (`cors.go`)

`AllowedOrigins()` from `CORS_ALLOWED_ORIGINS` (comma-separated, trimmed).
No hardcoded default: empty fails closed (cross-origin denied, same-origin
and server-to-server unaffected; boot logs a warn). A single `*` allows all
origins without credentials; explicit origins echo back with credentials
(`Authorization` header). Allowed methods `GET/POST/PATCH/PUT/DELETE/OPTIONS`,
preflight cache 12h.

## Rate limiting (`ratelimit.go`)

`AuthPerMin()` (`RATE_LIMIT_AUTH_PER_MIN`, default 10) guards `/auth/*`;
`APIPerMin()` (`RATE_LIMIT_DEFAULT_PER_MIN`, default 600) guards the API.
Bad values fall back to defaults. `TrustedProxies()` passes
`TRUSTED_PROXIES` (comma CIDRs/IPs) to Gin; unset = `RemoteAddr` only.

## Mail (`mail.go`)

`MailConfig{Address, Password, Host, Port, FromName}` from `EMAIL_ADDRESS`,
`EMAIL_PASSWORD`, `EMAIL_SERVICE` (`Gmail` → `smtp.gmail.com:587`) or explicit
`EMAIL_HOST`/`EMAIL_PORT`. `FromName` is currently `"Grip"`. `MailEnabled()` is
false when credentials are absent → senders **log instead of sending** (flows and
workers never fail for missing mail).

## Redis (`redis.go`)

`RDB *redis.Client` from `REDIS_URL` when set (e.g. `redis://:password@host:6379/0`,
parsed via `redis.ParseURL`), else the split vars `REDIS_ADDR` (default
`localhost:6379`), `REDIS_PASSWORD`, `REDIS_DB`. `REDIS_PRIVATE_URL` is honored as
a fallback for provider-style deploys. Unreachable at boot is **non-fatal**: warns,
leaves `RDB` nil, app runs without cache. `CloseRedis()` is nil-safe.

## Cloudinary (`cloudinary.go`)

Image-upload credentials for event covers (`CloudinaryConfig`): `CLOUDINARY_URL`
(`cloudinary://key:secret@cloud`, preferred) or the split `CLOUD_NAME` /
`CLOUD_API_KEY` / `CLOUD_API_SECRET`, plus `CLOUDINARY_UPLOAD_PRESET` for
unsigned uploads. `CloudinaryEnabled()` is false unless a full URL or a
complete key triple is present — the API then falls back to local-disk covers
with a loud warn log. Values are never logged.

## Boot order (`cmd/api`)

```
LoadEnv → InitLogger → ConnectDatabase (fatal) → ConnectRedis (optional)
  → River client + workers Start → routes → ListenAndServe
  → on SIGTERM/SIGINT: River Stop + HTTP Shutdown (15s)
```

Schema is **never** migrated here — `make migrate-up` (`cmd/migrate`) is the release
step that must succeed first, in dev and prod alike.

## Privilege split (deploy)

- Migration job: owner-level `DATABASE_URL` (DDL + `river_migration`).
- API runtime: restricted user (`CONNECT`, `SELECT/INSERT/UPDATE/DELETE`, `USAGE`
  on sequences — no DDL). The app only needs row access plus River queue tables.

## Env reference (`.env`, gitignored)

```
APP_PORT=8080
DATABASE_URL=postgresql://admin:adminpassword@localhost:5432/rallya?sslmode=disable
APP_URL=http://localhost:8080
APP_NAME=Rallya
JWT_SECRET=<32+ random bytes hex>
JWT_TTL_HOURS=24            # legacy; prefer ACCESS_TTL_MINUTES
EMAIL_SERVICE=Gmail
EMAIL_ADDRESS=...
EMAIL_PASSWORD=...
REDIS_URL=redis://localhost:6379/0
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0
ALLOW_INSECURE_JWT=true     # local dev only, never prod
SUPERADMIN_EMAILS=boss@example.com,ops@example.com  # platform superadmins (empty = none)
CLOUDINARY_URL=cloudinary://key:secret@cloud  # or split CLOUD_NAME/CLOUD_API_KEY/CLOUD_API_SECRET
CLOUDINARY_UPLOAD_PRESET=homz
```
