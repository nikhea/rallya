# AGENTS.md — Orders domain (`internal/order/`)

Claims against ticket inventory with hold expiry. Free confirms now; priced
waits for payments. No purchase endpoints for priced stock yet.

## Rules

- One ticket type per order; quantity > 0; per-order caps from the type.
- Availability via ticketing's `InspectType` (single code path) + path-event
  match (stealth 404 on mismatch). Never read ticket tables directly.
- Holds MUST join the order tx (`ReserveTx`/`ReleaseTx` taking `*gorm.DB`).
  Standalone `Reserve`/`Release` inside an order flow = phantom inventory bug.
  Same class as the PendingInvite lesson: in-tx repo calls take the tx handle.
- Idempotency keys: replay returns the original, touches nothing. NULL keys
  allowed (Postgres treats them distinct).
- Sweeper expires `PENDING` and `PENDING_PAYMENT` alike; batch-capped (100);
  failures log-and-continue per order. Periodic River job every 5 min
  (wired in `cmd/api`; worker nil-safe).
- Cancel releases on every live status; terminal states reject. Strangers get
  404, never 403, on others' orders. Admin path resolves org via event, then
  `CanManage` — no Casbin object for orders (documented deviation).
- Price snapshot (cents + currency) frozen at order time; never floats.
- `jobs/` worker takes `*OrderService`; nil service is a safe no-op.

## Tests

`go test ./internal/order/... -count=1` — fakes implement the three seams
(`TicketStore` returns real ticket sentinels so morph mapping is genuinely
tested). Handler suite wires the real stacks.
