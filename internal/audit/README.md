# Audit

Unified append-only paper trail. Domains emit entries inside their own
transactions (entry and mutation commit atomically; emit failure fails the
action — loud beats gappy). Reads are thin filtered lists over one table.

## Reads (no new writes here)

- `GET /orgs/:id/audit` — tenant-scoped, ADMIN+ (`audit:read`).
- `GET /admin/audit` — cross-tenant, superadmin only (first platform read;
  accountability backbone for the future `/admin/*` surface).

Filters: `action`, `actor`, `objectType`+`objectId`, `since`/`until`,
`org` (platform only); roster-style `page`/`perPage` (max 100). Bad filter
values 400, never silently ignored.

## v1 taxonomy

`org.created/updated/deleted` · `member.added/role_changed/removed`
(invite accepts emit `member.added` with `via: invite`) ·
`event.created/updated/published/unpublished/cancelled/deleted` ·
`ticket.created/activated/paused/deleted` ·
`order.created/confirmed/cancelled/expired` ·
`checkin.*` (every door outcome as `checkin.<outcome>`, slim pointer to the
`checkin_log` row — scan payloads are never duplicated) · `checkin.reverted`.

Deferred: ticket field updates (price/qty diffs), invite decline/revoke,
auth security events (login-anomaly semantics need their own design),
cover/gallery edits.

## Rules carried over

- Fixed columns + changed-fields-only diffs; never tokens, hashes, secrets.
- `org_id` is deliberately NOT a foreign key — deleting an org must not
  wipe its trail (reads scope by value). `actor_id` SET NULL on user
  delete; NULL actor = system (sweepers, webhooks).
- No purge in v1 — `(org_id, created_at)` index keeps a future ranged
  purge cheap. Revisit past 1M rows or on a compliance ask.
- Emitters are nil-safe when unwired (tests); production wires every
  mutating domain in `cmd/api`.
