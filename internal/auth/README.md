# Auth Domain

Owner of **user identity** for the MVP multi-tenant event platform: authentication,
sessions, refresh rotation, email verification (link + OTP), password reset, and
welcome onboarding. OAuth (`oauth_accounts`) and MFA (`mfa_factors`) tables exist
in the schema but have **no endpoints yet**.

Org membership and permissions live in `organization` / `iam`. Those domains must
consume identity only through `UserReader` (`reader.go`), never auth internals —
auth is shaped so it can later be extracted as its own service.

```
Auth -> (User identity) -> Organization -> IAM -> Events
```

## Architecture

Layered, dependency-inward. Handlers are thin Gin adapters; all rules live in the service.

```
routes.go ── wires /api/v1/auth group
handler/
  auth_handler.go  ── bind JSON → service → status codes (no business logic)
  middleware.go    ── RequireAuth: Bearer access JWT → session + user checks → ctx IDs
dto/               ── request/response shapes + binding tags + swagger examples
service/
  auth_service.go  ── flows (register, verify, login, refresh, logout, reset)
  errors.go        ── sentinel errors → HTTP mapping lives in handler
token/
  jwt.go           ── HS256 access tokens: sub=userID, sid=sessionID, iat/exp
  opaque.go        ── crypto/rand raw tokens + SHA-256 hashing for storage
repository/
  auth_repository.go ── GORM CRUD only (no business logic); implements UserReader
model/             ── 10 tables (users, profiles, credentials, sessions,
                      refresh_tokens, email_verifications, email_otp_codes,
                      password_resets, oauth_accounts, mfa_factors, login_attempts)
```

Cross-domain contract (`reader.go`):

```go
type UserReader interface {
    GetUserByID(id uuid.UUID) (*model.User, error)
}
```

## Tables

| Table | Purpose | Key rule |
|---|---|---|
| `users` | Core identity | `status ∈ ACTIVE/INACTIVE/SUSPENDED/DELETED`, soft delete |
| `user_profiles` | Display data (name, avatar, tz, locale) | 1:1 with users |
| `credentials` | Password hashes only | bcrypt cost 12, never raw |
| `sessions` | Logged-in devices (IP/UA/device, revoke) | soft revoke, preserves audit |
| `refresh_tokens` | `hash(token)` only | single-use rotation |
| `email_verifications` | 24h link tokens | single-use (`verified_at`) |
| `email_otp_codes` | 6-digit codes, 10-min TTL | hashed, max 5 attempts |
| `password_resets` | 1h tokens | single-use; success revokes **all** sessions |
| `login_attempts` | Append-only telemetry | brute-force / rate-limit source |
| `oauth_accounts`, `mfa_factors` | Schema-ready | no flows yet |

Canonical DDL is `migrations/00000*_*.up.sql`. IDs are app-generated (`BeforeCreate`);
GORM `AutoMigrate` is **never** used outside tests.

## Endpoints (`/api/v1/auth`)

Swagger UI: `GET /swagger/index.html`. Every route has request/response examples there.

### `POST /register` → 201

```json
{ "email": "john@test.com", "password": "Str0ngP@ssw0rd!", "firstName": "John", "lastName": "Doe" }
```

One GORM transaction creates user + profile + credential + 24h link token + 10-min
OTP, and transactionally enqueues `send_verification_email` (River). Duplicate
email (case-insensitive) → `409`. Responds `{ "message": "Verification email sent" }`
— never tokens (login is blocked until verified).

### `POST /verify-email` (also `GET ?token=`) → 200

Consumes the 24h link token (`verified_at` set), flips `email_verified`, enqueues
`send_welcome_email`. Reuse → `409`, unknown/expired → `400`.

### `POST /verify-code` → 200

```json
{ "email": "john@test.com", "code": "482910" }
```

Hash-compares the 6-digit code against the newest pending row: 10-min expiry,
5 wrong guesses lock the code (`429` on the 5th, plain `400` otherwise to avoid
leaking state). Success consumes the code, verifies the email, invalidates pending
link tokens, enqueues welcome mail. Unknown email → `400` (enumeration-safe).

### `POST /resend-verification` → 200

Throttled to 1/min (by latest verification row). Rotates **both** link token and
OTP (old ones invalidated) and enqueues a fresh email. Already-verified and unknown
emails return success-silently. `429` when throttled.

### `POST /login` → 200

```json
{ "email": "john@test.com", "password": "Str0ngP@ssw0rd!" }
```

bcrypt compare → reject `SUSPENDED/DELETED/INACTIVE` (`403`) → reject unverified
(`403` + `code: EMAIL_NOT_VERIFIED`) → create session (IP/UA/`X-Device-Name`) +
refresh row (30d default) → stamp `last_login_at` → append `login_attempts` on
**every** outcome (success and each failure reason).

```json
{ "accessToken": "eyJ...", "refreshToken": "a3f1..." }
```

Access JWT: 15m default, claims `sub` (user) + `sid` (session). Refresh: opaque
64-hex-char token, only its hash stored.

### `POST /refresh` → 200

Single-use rotation: revoke presented token, issue a new pair, touch session.
**Reuse detection**: presenting an already-revoked token revokes the whole session
(theft signal) — even the fresh pair dies. Expired/revoked/session-revoked →
`401`.

### `POST /logout` (auth) → 200

Revokes the calling session + its refresh rows. Idempotent.

### `GET /me` (auth) → 200

```json
{ "id": "uuid", "email": "john@test.com", "emailVerified": true,
  "profile": { "firstName": "John", "lastName": "Doe" } }
```

### `POST /forgot-password` → 200 always

Invalidates prior reset rows, creates a 1h token, enqueues `send_password_reset_email`
transactionally. Unknown emails return the same 200 (no enumeration).

### `POST /reset-password` → 200

```json
{ "token": "c8f3...", "newPassword": "N3wStr0ngP@ss!" }
```

Consumes the token, re-hashes (bcrypt 12), then revokes **all** sessions + refresh
tokens so every device re-logs in. Reuse → `409`.

## Security decisions

- Passwords: bcrypt only; reset/forgot never reveal account existence.
- Secrets at rest: only hashes (`password_hash`, `token_hash`, `code_hash`); raw
  values exist solely in email bodies and River job args until retention prunes them.
- Access tokens are stateless JWTs but bound to a revocable server-side session
  (`sid`), so logout/reset kill them despite the 15m TTL.
- `login_attempts` records every outcome for brute-force detection and rate limiting.
- JWT secret fail-closes: must be 32+ bytes or the process exits (`ALLOW_INSECURE_JWT=true`
  only for local dev). See `cmd/config/README.md`.

## Email integration

Auth never sends mail directly. It enqueues River jobs (`internal/notification/jobs`)
— transactionally for verification/reset (same GORM tx via unwrapped `*sql.Tx`),
post-commit for welcome. Without an enqueuer (unit tests), delivery is skipped.
Details: `internal/notification/README.md`.

## Testing

```bash
go test ./internal/auth/... -count=1
```

- `token/` — JWT roundtrip, wrong-secret/expired/tampered rejection, opaque uniqueness.
- `service/` — full flows on in-memory SQLite (`testutil`): duplicates, verify
  link+code, attempt lockout, rotation + reuse revocation, logout, throttle,
  reset session invalidation, suspension, job assertions via `FakeEnqueuer`.
- `handler/` — HTTP status matrix incl. middleware 401s through the real router.
