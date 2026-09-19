# Notification Domain

Outbound email for the platform, delivered through **River** (Postgres-backed job
queue). Producers (currently auth) enqueue jobs; River workers render templates
and send over SMTP. Workers run **in-process with the API** (`cmd/api`); the API
only ever enqueues.

```
service flow (GORM tx) ──EnqueueTx──▶ river_job ──worker──▶ template ──SMTP──▶ inbox
                                              ▲                  │
                                     LISTEN/NOTIFY (or poll)     └── log-only if SMTP unset
```

## Layout

```
notification/
  notification.go        ── AppName() (APP_NAME env, default "Rallya")
  templates/
    verify_email.html    ── OTP code block + 24h link button
    reset_password.html  ── 1h link button
    welcome_email.html   ── post-verification onboarding
    templates.go         ── embed.FS + html/template render + text fallbacks
  mailer/
    mailer.go            ── multipart/alternative SMTP send; logs when disabled
  jobs/
    jobs.go              ── JobArgs + Workers + Enqueuer + RiverEnqueuer + FakeEnqueuer
```

## Jobs

| Kind | Enqueued by | Args | Template fields |
|---|---|---|---|
| `send_verification_email` | register, resend (same tx) | user, email, name, **raw OTP**, verify link | `AppName, Name, OTP, VerifyLink` |
| `send_password_reset_email` | forgot-password (same tx) | user, email, name, **raw reset token** link | `AppName, Name, ResetLink` |
| `send_welcome_email` | email/code verification (post-commit) | user, email, name, login link | `AppName, Name, LoginLink` |

Queues: `emails` (`MaxWorkers: 5`) for all mail, `default` (`MaxWorkers: 10`) reserved.
Workers return errors on SMTP failure so River retries with backoff; success marks
the job `completed`. Name falls back to the email local-part when no first name.

## Enqueue contract (`jobs.Enqueuer`)

```go
type Enqueuer interface {
    EnqueueTx(ctx context.Context, tx *gorm.DB, args river.JobArgs) error
    Enqueue(ctx context.Context, args river.JobArgs) error
}
```

- `RiverEnqueuer` — production. `EnqueueTx` unwraps `*sql.Tx` from the GORM handle
  so the job commits atomically with the flow's rows (user created ⇒ email queued,
  guaranteed). Falls back to plain insert when called without a tx.
- `FakeEnqueuer` — records args for test assertions (`OfKind(kind)`).
- Services nil-check the enqueuer: unit tests without mail wiring skip delivery.

## Templates

Go `html/template`, embedded via `embed.FS` (no disk dependency in the image).
`templates.go` exposes `RenderVerification / RenderPasswordReset / RenderWelcome`,
each returning `{ Subject, HTML, Text }` — every send is multipart with a
plain-text fallback. Field names must match the `{{ .Field }}` placeholders.

## River operations

- **Schema**: River's `river_*` tables are migrated by `rivermigrate` inside
  `cmd/migrate up` (after the app tables), tracked in `river_migration`.
  `version` reports both engines.
- **Driver**: `riverdatabasesql` on the shared `*sql.DB` from `cmd/config`, plus a
  dedicated pgx pool for LISTEN/NOTIFY (falls back to polling if unreachable —
  still correct, slower pickup).
- **Inspecting**: `SELECT kind, state FROM river_job ORDER BY id DESC LIMIT n;`
  states: `available → running → completed | retryable | discarded`.
- **Retention**: completed/discarded jobs are pruned by River maintenance. Note raw
  OTP/tokens persist in `river_job.args` until pruned (OSS River has no encrypted
  jobs — Pro does).
- **Resend race**: resend invalidates old OTP rows, but an already-queued job may
  still deliver the superseded code (it will simply fail verification). The 60s
  resend throttle keeps this window tiny.

## Mailer behavior

`mailer.Send(to, subject, text, html)` uses `cmd/config.MailConfigFromEnv`. When
`EMAIL_ADDRESS`/`EMAIL_PASSWORD` are unset, it logs the payload and returns nil —
auth flows and workers never break for missing mail config. SMTP errors are returned
(River retries); nothing is silently swallowed in production.

## Testing

```bash
go test ./internal/notification/... -count=1
```

Template render tests assert subject/HTML/text interpolation of every field.
Worker SMTP paths are covered indirectly: service/handler tests assert the right
jobs are enqueued (`FakeEnqueuer`); live SMTP is verified manually against the
`river_job` table (see Auth README smoke flow).
