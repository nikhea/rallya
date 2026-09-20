# AGENTS.md — Notification domain (`internal/notification/`)

Outbound mail via River (Postgres queue, in-process workers). Producers enqueue;
workers render templates and send. Never let mail fail a user flow.

## Rules

- Jobs (`jobs/`): one `JobArgs` + `Worker` pair per email, `Kind()` strings
  stable (renames break in-flight jobs — see River docs on renaming).
  Register in `AddAll`; queues: `emails` (MaxWorkers 5), `default` (10).
- `Enqueuer` interface: `EnqueueTx` (same GORM tx via unwrapped `*sql.Tx` —
  user created ⇒ email queued, guaranteed) vs `Enqueue` (post-commit,
  best-effort + warn log). Nil-safe for tests. `FakeEnqueuer` asserts targeting.
- Templates (`templates/`, embedded): every email needs HTML + text fallback;
  field names must match `{{ .Field }}` placeholders. Test render output.
- Workers return errors on SMTP failure (River retries with backoff); success
  marks completed. `river.Job[T]` embeds `*JobRow` — test jobs need a non-nil row.
- Raw secrets (OTP/tokens) travel in job args by necessity; they persist in
  `river_job.args` until retention prunes them (OSS has no encrypted jobs).
- `mailer.Send`: multipart/alternative; SMTP unset ⇒ log-only success (flows
  and workers never break for missing mail). Never swallow production errors.
- `AppName()` from `APP_NAME` (default `Rallya`); links from `config.AppURL()`.

## Tests

`go test ./internal/notification/... -count=1` — template interpolation,
worker render+send plumbing (log-only mode), fake-enqueuer targeting.
Live SMTP verified manually via `river_job` state (see auth README smoke flow).
