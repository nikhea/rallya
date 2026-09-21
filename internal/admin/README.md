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
- Drift flags deferred: the repair endpoints below report drift
  authoritatively — no half-signal in the roster views.

## Repairs

- `POST /admin/orgs/:id/policies/reseed` — re-run the org seed,
  `{added, removed: 0, total}`. Seeds never remove.
- `POST /admin/users/:id/policies/sync` — converge groupings to
  membership truth (sweep-then-add per org, stale orgs swept, `g2`
  untouched), `{added, removed, total}`.

Both audited (`admin.policies_reseeded`, `admin.policies_synced`) and
idempotent — safe to hammer at 2am. Note the Casbin trap this exposed:
`AddPolicies` is all-or-nothing, so `SeedOrgPolicies` adds missing rows
one by one (it heals partial drift; bulk-add cannot).
