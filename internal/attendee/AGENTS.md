# AGENTS.md — Attendees domain (`internal/attendee/`)

Per-unit door records from CONFIRMED orders + organizer manual adds.
Rows never deleted; QR payloads authenticate future check-ins.

## Rules

- Mint inside confirmation txs via `MintForOrder` (skip existing
  `(order_id, unit_index)` pairs — redelivery converges). Raw tokens return
  for email embedding only; store `HashToken` exclusively.
- QR: `BuildPayload`/`VerifyPayload` in `utils/`; `QR_SIGNING_SECRET`
  fail-closed (tests must `t.Setenv` it — unset kills the runner via os.Exit).
  Mine endpoints return NO payloads (hash-only storage cannot re-issue them);
  email carries them.
- Reads: owner-own + ADMIN+ roster (searchable); stealth 404s; nothing public.
- Cancel mirrors orders (owner + cascade); terminal states reject.
- Seams: implement order's `AttendeeMinter` (+`MintedAttendee`); declare
  `EventResolver`/`OrgAccess` locally for events/org. `MemberView`-style
  shapes banned outside `dto/` (same rule as org).
- New Casbin objects need matrix rows + backfill migration (`000015` pattern).

## Tests

`go test ./internal/attendee/... -count=1` — QR matrix, mint idempotency,
cancel cascades, manual/correct guards, roster scoping; HTTP suite on the
full stack.
