# AGENTS.md — Auth domain (`internal/auth/`)

Auth ends at User identity. Membership/permissions are organization/iam;
events consume identity via `UserReader` — never auth internals.

## Seams

- `UserReader` (`reader.go`): `GetUserByID`, `GetUserByEmail`. `AuthService`
  asserts conformance (`var _ auth.UserReader`). Downstream fakes live in
  `testutil.FakeUserReader`.
- `dto.MembershipLister`: org implements it so `GET /auth/me` embeds
  `organizations[]`. Nil-safe (field omitted when unwired); lister errors
  are swallowed to 200 — never fail identity for org trouble.
- `jobs.Enqueuer`: verification/reset mail enqueues **transactionally**
  (same GORM tx via unwrapped `*sql.Tx`); welcome mail post-commit best-effort.

## Rules

- Hashes only at rest: `password_hash` (bcrypt 12), `token_hash`, `code_hash`
  (SHA-256). Raw values exist solely in email bodies and `river_job.args`
  until retention prunes them.
- Login blocked until verified (`403 EMAIL_NOT_VERIFIED`); forgot-password
  always 200 (no enumeration); reset revokes ALL sessions.
- Refresh rotation is single-use; reuse of a revoked token revokes the whole
  session (theft signal). Access JWT 15m (`sub` + `sid`), bound to a revocable
  server-side session.
- OTP: 6-digit, 10-min TTL, 5-attempt lockout; link tokens 24h, single-use.
- Record every login outcome in `login_attempts` (success + failure reason).
- JWT secret fail-closes (32+ bytes or exit); `ALLOW_INSECURE_JWT=true` local only.

## Tests

`go test ./internal/auth/... -count=1` — token unit, service flows (SQLite +
`FakeEnqueuer`), HTTP matrix incl. middleware 401s, `testutil` helpers.
