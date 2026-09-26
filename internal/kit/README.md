# Kit domain (`internal/kit/`)

Event kit (merch/welcome-pack) collections. Organizers define named kit
types per event; door staff hand units to checked-in attendees through
`PENDING -> COLLECTED (-> VOIDED)` records with per-kit tallies.

## Flows

- **Define**: `POST .../kits {name, description?, quantityTotal}` (ADMIN+,
  `kit:create`). Quantity must be ≥ 1. Audited `kit.created`.
- **Hand out**: `POST .../kits/:kitId/collect {attendeeId, reserve?,
  idempotencyKey?}` (ADMIN+, `kit:update`). Attendee must be `CHECKED_IN`
  for the event (`422` otherwise — the check-in connection is an
  eligibility gate, not auto-creation). Default collects immediately;
  `reserve: true` holds `PENDING`. Same `idempotencyKey` replays return the
  existing record (tablet retries converge). Audited `kit.collected` /
  `kit.reserved`.
- **Pickup**: `POST .../kit-collections/:id/collect` flips `PENDING ->
  COLLECTED` with staff stamps; anything else is `422`.
- **Void**: `POST .../kit-collections/:id/void` from `PENDING` or
  `COLLECTED` (frees re-issue); anything else is `422`. Audited `kit.voided`.
- **Reads**: `GET .../kits` (types with live
  pending/collected/voided/remaining), `GET .../kits/:kitId/collections`,
  `GET .../kit-collections?kitId=&status=&attendeeId=`.
- **Revert interplay**: check-in `Revert` consults the collection guard and
  fails `422 ATTENDEE_HAS_COLLECTIONS` while non-voided handouts exist —
  void first, then revert.

## Rules

- Oversell guard: kit row locked `FOR UPDATE` in-tx, active (non-voided)
  count vs `quantity_total` (`409` on exhaustion). Per-attendee dupes fall
  on the partial unique index (`409`); both engines' violation strings map.
- One active collection per attendee per kit (partial unique
  `WHERE status <> 'VOIDED'`); voided rows free re-issue.
- Catalog clamp: `quantityTotal` cannot drop below the collected count
  (`422`); delete only with zero active rows (`422`).
- Wrong-event kit/collection/attendee refs are stealth `404`s.
- Seams, not imports: `EventResolver` (events) + `AttendeeChecker`
  (attendees implement `AttendeeStatusInEvent`); checkin consumes
  `CollectionGuard` (`HasActiveCollections`). No service-to-service imports.
- Audit taxonomy: `kit.created/updated/deleted` (`ObjectKit`),
  `kit.reserved/collected/voided` (`ObjectKitCollection`).
- GORM tags mirror `migrations/000021_kit`; `LOWER() LIKE` not `ILIKE`;
  repo methods in a tx take the tx handle.

## Tests

`go test ./internal/kit/... -count=1` — status matrix, eligibility gate,
quantity exhaustion + clamp, idempotent replay, double-collect
convergence, revert-guard coupling; HTTP suite on the full stack
(permission gates + stealth shapes).
