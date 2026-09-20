# Attendees Domain

Per-unit door records minted from CONFIRMED orders, plus organizer manual adds.
Rows are never deleted (audit trail); cancellation flips status. QR payloads
authenticate scans for the check-in module.

```
order CONFIRMED (free path / webhook) ──mint(+email)──▶ REGISTERED rows
organizer manual add ──▶ REGISTERED rows (no order link)
order cancel ──▶ CANCELLED rows (history kept)
```

## Tables

`attendees` (`migrations/000014`): order FK (nullable, SET NULL), `unit_index`,
event FK CASCADE, user FK nullable, email/name, token hash only,
`REGISTERED/CHECKED_IN/CANCELLED`, `UNIQUE(order_id, unit_index)` for
redelivery-safe minting.

## Rules

- Mint is synchronous inside the confirmation tx (free checkout + `MarkPaid`),
  skipping existing `(order, unit)` pairs — webhook redelivery converges.
  Raw tokens return alongside IDs for email embedding only (never persisted).
- QR payload `<attendeeUUID>.<token>.<hmac>` (see `utils/`): self-contained,
  forgery-proof without `QR_SIGNING_SECRET`, single-row scoped. Raw tokens are
  scannable by design — payloads surface only to owners/organizers, never
  publicly. Mine endpoints return rows WITHOUT payloads (hash-only storage
  cannot re-issue); email carries them.
- Manual adds need no account (walk-ins/comps/staff); organizer name correction
  allowed; transfers deferred (theft-by-forward risk).
- Reads: owner-own + org ADMIN+ roster (paginated, name/email search). No public
  endpoints — attendee PII never leaks.
- Cancel paths mirror orders (owner self-cancel, order-cancel cascade).

## Seams (consumer-declared)

- Order declares `AttendeeMinter` (+ `MintedAttendee`); attendee implements
  (imports order service types only — no cycle, GroupSyncer precedent).
- Attendee declares `EventResolver` (event implements: `ResolveEventID`,
  `OrgOf`) and reuses org `CanManage` via its own `OrgAccess` interface.
- Confirmation email (`send_order_confirmation_email`, inline QR PNGs via
  go-qrcode + CID multipart) enqueues transactionally with confirmation.

## Testing

```bash
go test ./internal/attendee/... -count=1
```

QR roundtrip/tamper/malformed matrix, mint idempotency, cancel cascades,
manual add/correct guards, roster scoping; HTTP suite on real
auth+org+event+Casbin. `QR_SIGNING_SECRET` must be set in tests (fail-closed).
