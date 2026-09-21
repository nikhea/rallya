# AGENTS.md — Audit domain (`internal/audit/`)

Append-only trail over every mutating domain. Entries commit with actions.

## Rules

- Domains emit via the `Emitter` seam (`EmitTx(tx, Entry)`) INSIDE their
  tx; emit failure fails the action. `SetAuditEmitter` setters stay
  nil-safe (tests); `cmd/api` wires all five emitters (org, event,
  ticketing, order, checkin).
- Entry shape: fixed columns + changed-fields-only `before`/`after` maps;
  nil OrgID = platform scope, nil ActorID = system. No tokens/secrets —
  the table must be dispute-dumpable without redaction.
- Actions namespaced `<object>.<verb>`; checkin outcomes ride the verb
  (`checkin.checked_in`) with `ObjectCheckinLog` pointers (slim, never
  duplicated payloads).
- Reads: org-scoped ADMIN+ (`audit:read` + backfill pattern) and
  superadmin platform read. Filters + offset pagination; bad values 400.
- New event types need taxonomy review (see README deferred list) — don't
  freeload the table with noisy or secret-bearing entries.

## Tests

`go test ./internal/audit/... -count=1` — emit/list/filter matrix,
pagination; HTTP suite on the full stack (org gates, superadmin promote,
stealth shapes). Emit coupling proven end-to-end: the handler fixture
writes through the real org service and asserts entries land.
