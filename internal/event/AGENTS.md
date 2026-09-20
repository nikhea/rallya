# AGENTS.md — Events domain (`internal/event/`)

Org-scoped events: CRUD, draft/publish/cancel lifecycle, public discovery,
covers, opt-in announcements. Consumes org via `OrgResolver` (declared in
`service/`, implemented by org service) — never org tables. Casbin from birth.

## Rules

- `:id`/`:eventId` accept UUID or slug (slugs scoped per org, auto-uniquified
  like orgs). Published is public; drafts/cancelled stealth-404 outsiders.
- Lifecycle: `DRAFT → PUBLISHED → DRAFT` (unpublish) or `→ CANCELLED` (terminal);
  anything else 409. Status never changes via update endpoint.
- Dates: `ends_at > starts_at` when both set; open-ended (null end) and past
  dates allowed; normalize to UTC. Capacity nullable = unlimited, unenforced
  until ticketing.
- Covers: 5MB cap pre-read (`MaxBytesReader`), MIME **sniffed from bytes**
  (`http.DetectContentType`), never the client header; jpeg/png/webp only.
  Storage selected at boot: Cloudinary when configured (see `cmd/config`
  Cloudinary section), else local `uploads/events/<orgID>/` with read-only
  `/uploads` mount. Served URLs stay absolute so backend swaps touch only
  `cover/`. Replace/delete clean up best-effort.
- Listing: public `GET /events` (published only); org listing adds drafts for
  members. Filters (`org`, `from`/`to` RFC3339, `q`, `status`), `sort=starts|created`,
  page/perPage (20/max 100). Bad filter values 400, never silently ignored.
- New Casbin objects need matrix rows in `iam/policy.go` + `matrix_test.go`
  expectations + a backfill migration for pre-existing orgs (`000008` pattern).
- `cover/` is local disk: fine for single-instance dev, documented S3 follow-up.

## Tests

`go test ./internal/event/... -count=1` — slug sequences, stealth reads,
lifecycle incl. terminal cancel, opt-in targeting, cover validation (spoofed
MIME, oversize, cleanup), filter matrix; HTTP suite on real auth+org+Casbin.
