# AGENTS.md — Ticketing domain (`internal/ticketing/`)

Ticket types: pricing tiers per event with sale rules. Consumes events via
`EventResolver` (declared in `service/`, adapted over the event service) —
never event tables. No purchase/claim endpoints; orders/payments land later.

## Rules

- Money is integer minor units (`price_cents >= 0`) + `currency CHAR(3)`
  default USD. Never floats. Display divides by 100 (client-side for now).
- Capacity couples both directions at config time: new/grown allocations must
  fit event headroom; event capacity can't drop below allocated; 409 either way.
- `SOLD_OUT` is computed (`sold >= total`), never written. Manual status writes
  limited to `DRAFT/PAUSED ↔ ACTIVE` via explicit endpoints; delete requires
  zero sales (sold history survives).
- `Reserve()`/`Release()` are the oversell-critical path: row-locked
  (`SELECT … FOR UPDATE`) inside a tx, enforcing active status, per-order caps,
  and headroom. SQLite ignores the lock clause — oversell is proven by the
  PG-gated hammer test (`DATABASE_URL_TEST`, skips otherwise), not SQLite.
- Availability is one shared code path (`Availability()`) with explicit reasons
  (`event_unpublished`, `not_active`, `not_started`, `ended`, `sold_out`);
  public reads show ACTIVE types of published events only.
- New Casbin objects need matrix rows in `iam/policy.go` + `matrix_test.go`
  expectations + a backfill migration for pre-existing orgs (`000011` pattern).
- DTO package is `ticketdto` (unique per domain — swag fails on duplicates).

## Tests

`go test ./internal/ticketing/... -count=1` — validation, capacity both
directions, lifecycle, availability reasons, guards; HTTP suite on real
auth+org+event+Casbin (co-registered routes prove no conflicts).
`DATABASE_URL_TEST=… go test ./internal/ticketing/service/ -run TestReserveConcurrencyPG`
hammers 20×5 reserves onto 50 units and asserts exactly 50 succeed.
