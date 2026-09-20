# Orders Domain

Claims against ticket types with hold expiry. Free (0-cent) orders confirm
immediately; priced orders wait in `PENDING_PAYMENT` for payments. One ticket
type per order. Authenticated buyers need no org membership; org ADMIN+ gets a
support path. No purchase of priced inventory exists yet — that's payments.

```
POST /events/:id/orders ──reserve+create (one tx)──▶ CONFIRMED (free)
                                                   └▶ PENDING_PAYMENT (priced)
sweep_expired_orders (5 min) ──▶ EXPIRED + release
POST /orders/:id/cancel ──▶ CANCELLED + release (owner or org ADMIN+)
```

## Tables

`orders` (`migrations/000012`): user/event/type FKs (CASCADE), quantity,
price snapshot (cents + currency, never floats), status
`PENDING/PENDING_PAYMENT/CONFIRMED/CANCELLED/EXPIRED`, optional idempotency key
(`UNIQUE(user_id, key)`, NULL keys unrestricted), hold `expires_at` with a
partial index for the sweeper.

## Rules

- Availability reuses ticketing's single code path (`InspectType`): unpublished
  event, inactive/paused window, sold out, and per-order caps all 404/400/409
  before any hold. The ticket must belong to the path event (stealth 404).
- Holds join the order transaction (`ReserveTx`): failed inserts roll holds
  back — no phantom inventory. Same for cancel/sweep (`ReleaseTx`).
- Idempotency: replays with the same key return the original order without
  touching inventory.
- Hold TTL 15 min on priced orders; `sweep_expired_orders` (periodic River job,
  batch 100) expires `PENDING` and `PENDING_PAYMENT` alike — unpaid holds never
  pin inventory. Payments charges only unexpired `PENDING_PAYMENT`.
- Cancel works on any live order (holds and confirmed-free), releasing stock;
  terminal states reject. Strangers get stealth 404s, never 403s, on others'
  orders.
- No Casbin object: buyer routes are auth-only by design; admin support resolves
  org rights in service (`CanManage`). Documented deviation, revisited if order
  permissions ever need matrices.

## Seams (consumer-declared, provider-implemented)

- `TicketStore` (ticket service): `InspectType`, `ReserveTx`, `ReleaseTx`.
  Error mapping (`morphReserveErr`) keeps handlers on order sentinels only.
- `EventLookup.OrgOf` (event service) + `OrgAccess.CanManage` (org service)
  for the admin path. Main wires the real services (compiler-checked).

## Testing

```bash
go test ./internal/order/... -count=1
```

SQLite suites: confirm-now vs wait-for-payment, idempotent replay (no double
hold), guards matrix, cancel/release semantics, admin path, sweeper (backdated
holds expire exactly once), worker nil-safety. Handler suite on real
auth+org+event+ticket stacks.
