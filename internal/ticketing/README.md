# Ticketing Domain

Ticket types: pricing tiers per event with sale rules. Consumes events through
`EventResolver` (declared in `service/`, adapted over the event service) —
never event tables. Casbin `ticket` object from birth. Claims/orders and
payments land later; `Reserve()`/`Release()` already prevent oversell.

```
public (no auth)                     org-scoped (auth -> membership -> Casbin)
GET /events/:id/tickets              /orgs/:id/events/:eventId/tickets...
(published only, live availability)  (CRUD, activate/pause)
```

## Tables

`ticket_types` (`migrations/000010`): event FK CASCADE, name/description,
`price_cents` ≥ 0 (integer minor units, never floats) + `currency CHAR(3)`
default USD, `quantity_total`/`quantity_sold`, nullable `max_per_order` and
sale window, `DRAFT/ACTIVE/PAUSED` status, soft delete. `SOLD_OUT` is computed
(`sold >= total`), never written. Event permission rows backfilled (`000011`;
new orgs seed at runtime via extended `SeedOrgPolicies`).

## Rules

- Create (ADMIN+ `ticket:create`): always `DRAFT`; capacity coupling enforced
  **both directions** — new/grown type allocations must fit event headroom,
  and event capacity can't drop below allocated totals (409 either way).
- Lifecycle: `DRAFT/PAUSED ↔ ACTIVE` via explicit activate/pause; nothing else
  transitions (409). Delete requires zero sales (sold history survives).
- Availability (single code path for public reads and future orders):
  published event × ACTIVE × in-window × remaining > 0, else a reason
  (`event_unpublished`, `not_active`, `not_started`, `ended`, `sold_out`).
- `Reserve(typeID, n)` / `Release(typeID, n)`: row-locked (`SELECT … FOR UPDATE`
  in tx), enforcing active status, per-order caps, and headroom — sold can
  never overshoot, proven by the PG concurrency test. SQLite ignores the lock
  clause, so oversell is unit-tested for logic and concurrency-tested on PG.
- Public listing shows ACTIVE types of published events only; member listing
  shows all with live `forSale` flags. No purchase/claim endpoints — priced
  types display only until orders/payments land.

## Testing

```bash
go test ./internal/ticketing/... -count=1
DATABASE_URL_TEST=postgresql://... go test ./internal/ticketing/service/ -run TestReserveConcurrencyPG -count=1
```

SQLite suites: validation, capacity both directions, lifecycle matrix,
availability reasons, guards; HTTP suite on real auth+org+event+Casbin
(co-registered routes prove no conflicts); PG hammer test (20×5 reserves
on 50 units lands exactly 50).
