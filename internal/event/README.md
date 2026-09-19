# Events Domain

Org-scoped events with a draft/publish/cancel lifecycle, public discovery,
cover uploads, and opt-in publish announcements. Consumes organization through
`OrgResolver` (declared here, implemented by org service); never touches org
tables. Enforcement is Casbin from birth — no retrofitted gates.

```
public (no auth)                    org-scoped (auth -> membership -> Casbin)
GET /events, GET /events/:id        /orgs/:id/events... (create/read/update/
(published only)                    delete/publish/unpublish/cancel/cover)
```

## Architecture

```
routes.go ── public group + org group (mirrors org route guards)
handler/event_handler.go ── bind/validate (dates, multipart sniff) → service
dto/ (package eventdto) ── shapes + binding tags + swagger examples
service/
  errors.go, event_service.go ── CRUD, lifecycle, covers, announcements
repository/ ── GORM CRUD + typed filters (portable LOWER() LIKE search)
model/ ── events (per-org unique slug, UTC timestamps, nullable capacity)
cover/local.go ── disk storage (swappable for S3; URLs stay stable)
utils/ ── Slugify, FormatTime helpers
```

## Tables

`events` (`migrations/000006`): org FK CASCADE, `UNIQUE(organization_id, slug)`,
`DRAFT/PUBLISHED/CANCELLED` check, `ends_at > starts_at` check, nullable
positive capacity (unenforced until ticketing), cover URL, soft delete.
`organization_memberships.notify_events` (`000007`, default TRUE) drives
announcement targeting. Event Casbin rows backfilled per org (`000008`;
new orgs seed at runtime via `SeedOrgPolicies`).

## Lifecycle & rules

- Create (ADMIN+ `event:create`): always `DRAFT`; empty slugs auto-uniquify per
  org (`fest`, `fest-2`, …); explicit taken slug → 409; `ends_at > starts_at`
  when both set (open-ended allowed); past dates allowed; stored UTC.
- `DRAFT → PUBLISHED` (ADMIN+ `event:publish`): queues `send_event_published_email`
  per opted-in member **in the same transaction** as the transition.
- `PUBLISHED → DRAFT` (unpublish) and `PUBLISHED → CANCELLED` (terminal) allowed;
  anything else → 409. Cancel is terminal: republish after cancel is rejected.
- Update (ADMIN+ `event:update`): field edits only — status never changes here.
- Delete (OWNER via `event:delete` wildcard): hard delete + cover cleanup.
- Visibility: published is public; drafts (and cancelled) 404 for outsiders,
  MEMBER+ reads drafts in-org (stealth preserved end to end).

## Covers

`POST .../cover` (multipart `file`, `event:update`): 5MB cap enforced pre-read
(`MaxBytesReader`), MIME **sniffed from bytes** (never the client header),
jpeg/png/webp only. Storage is selected at boot: **Cloudinary** when configured
(`CLOUDINARY_URL` or key triple + `CLOUDINARY_UPLOAD_PRESET`, uploads to
`rallya/events/<orgID>/`, public ID derived back from the secure URL on delete)
else **local disk** (`uploads/events/<orgID>/`, read-only `/uploads` mount).
Served URLs are absolute either way, so swapping backends touches only `cover/`.
Replace/delete clean up old files (best-effort, warn-logged).

Cloudinary access lives in the reusable `internal/media` package (generic
upload/delete by public ID); `cover/` only sets event naming policy. Future
uploads (avatars, galleries) should build on `internal/media` directly.

## Gallery

`POST .../images` (multipart `files[]`, up to 10, same validation as covers)
stores one `event_images` row per file with provider metadata (URL, public ID,
format, bytes, dimensions) and returns them. When the event has no cover yet,
the **first image is cloned as the cover photo** (same asset, no re-upload).
All-or-nothing: provider files uploaded before a row-transaction failure are
cleaned up. `GET .../images` (member read) lists oldest-first. Event delete
removes cover + gallery assets (best-effort) with rows cascading in DDL.

`POST .../cover` (multipart `file`, `event:update`): 5MB cap enforced pre-read
(`MaxBytesReader`), MIME **sniffed from bytes** (never the client header),
jpeg/png/webp only. Storage is selected at boot: **Cloudinary** when configured
(`CLOUDINARY_URL` or key triple + `CLOUDINARY_UPLOAD_PRESET`, uploads to
`rallya/events/<orgID>/`, public ID derived back from the secure URL on delete)
else **local disk** (`uploads/events/<orgID>/`, read-only `/uploads` mount).
Served URLs are absolute either way, so swapping backends touches only `cover/`.
Replace/delete clean up old files (best-effort, warn-logged).

Cloudinary access lives in the reusable `internal/media` package (generic
upload/delete by public ID); `cover/` only sets event naming policy. Future
uploads (avatars, galleries) should build on `internal/media` directly.

## Announcements & opt-in

Publish collects `NotifyTargets` (opted-in members + display names) and enqueues
one job each, atomically with the status flip — no phantom announcements on
rollback, no silent publishes on enqueue failure. Preference: `notify_events`
(default TRUE), self-service via `PATCH /orgs/:id/members/me {notifyEvents}`.

## Listing

Public `GET /events` (published only): `org` slug, `from`/`to` RFC3339,
`q` title/description search, `status` (accepted, published-only effect),
`sort=starts|created`, `page/perPage` (20/max 100). Org `GET /orgs/:id/events`
adds drafts for members. Bad filter values → 400 (never silent ignore).

## Testing

```bash
go test ./internal/event/... -count=1
```

SQLite suites: slug sequences, stealth reads, lifecycle matrix (incl. terminal
cancel), opt-in/out announcement targeting, cover validation (spoofed MIME,
oversize, replace/delete cleanup), filter matrix; HTTP suite runs the real auth
+ org + Casbin stack with real multipart uploads against temp dirs.
