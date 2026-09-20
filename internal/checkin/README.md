# Check-in

Door scans over the attendee seam. Staff scan QR codes (or fall back to
roster IDs); every attempt logs and returns 200 with its outcome — a
refused scan is a successful request with a negative result, not a 4xx.

## Endpoints (ADMIN+, `checkin` object)

- `POST /orgs/:id/events/:eventId/checkin` — `{code}` (raw QR) or
  `{attendeeId}` (QR-less fallback, logged `manual`).
- `POST .../checkin/batch` — `{codes[]}`, QR-only, max 50 (413 over),
  per-item outcomes, no fail-fast.
- `GET .../checkin/stats` — `{registered, checkedIn, cancelled, total}`.

## Outcomes

`CHECKED_IN` (fresh) · `ALREADY_CHECKED_IN` (rescan, no state change,
first-scan time kept) · `INVALID_CODE` (single stealth bucket for
malformed/forged/unknown — no oracle) · `CANCELLED` (row cancelled) ·
`WRONG_EVENT` (genuine ticket, wrong door).

## Flow

1. Handler: auth → org context → Casbin → service.
2. Service verifies HMAC (`VerifyPayload`), resolves the event, then runs
   one tx: `ApplyCheckin` (row-locked flip in attendees) + `checkin_logs`
   insert. Concurrent scans of one QR serialize on the row lock — exactly
   one lands fresh, the rest report `ALREADY_CHECKED_IN`.
3. The flip lives in the attendees domain (`ApplyCheckin`/`ApplyCheckinManual`
   via the `CheckinApplier` seam); this package never touches that table.

## Tables

`checkin_logs` (`000016`): append-only, `attendee_id` NULL for unscannable
codes. No undo in v1 — mis-scans need the follow-up ADMIN revert.
