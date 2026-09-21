# Admin

Platform reads over tenant rows. Owns no tables and no Casbin object —
`RequireSuperAdmin` gates everything. Every call emits an audit entry
(actor = superadmin) so snooping is answerable.

## Reads (this slice)

- `GET /admin/orgs` (`?q=`) — org inventory, newest first, member counts.
- `GET /admin/orgs/:id` — org row + full roster (identity + roles).
- `GET /admin/users` (`?q=` email fragment) — accounts + superadmin flags.
- `GET /admin/users/:id` — profile + every tenant membership.
- `GET /admin/users/:id/orders` — cross-tenant order history in
  `orderdto` shapes (already Stripe-free — no payment identifiers leak).

Roster conventions everywhere (`page`/`perPage` max 100). Bad values 400.

## Rules carried over

- Reader over others' rows: repositories composed via constructor, never
  another domain's tables directly. New reads need no migration.
- Full identity data (superadmin is root); payment identifiers stripped
  (bearer-adjacent, not identity). Audit-on-read is the control, not
  redaction.
- Drift flags deferred: the follow-up repair slice (`policies/reseed`,
  `policies/sync` with `{added, removed, total}` diffs) reports drift
  authoritatively — no half-signal here.
