# IAM Domain (Casbin)

Enforcement reader for authorization: **who may do what, in which tenant**.
Organization is the sole writer (memberships → groupings, per-org policy seeds);
iam never touches org tables and exposes no public routes in this pass — only
`RequirePermission` / `RequireSuperAdmin` middleware plus the sync/seeder
implementations org consumes.

```
org mutation ──sync/grouping──▶ casbin_rule ◀──enforce── RequirePermission(obj, act)
org create/delete ──seed/remove──▶ casbin_rule (per-org role policies)
SUPERADMIN_EMAILS ──boot seed──▶ g2 superadmin groupings (domain-less bypass)
```

## Model (`model.conf`, embedded)

RBAC with domains; the domain **is** the org UUID:

```
r = sub, dom, obj, act        # user UUID, org UUID, resource, action
p = sub, dom, obj, act        # role/user, org UUID (or *), object (or *), action (or *)
g = _, _, _                   # user UUID, role, org UUID
g2 = _, _                     # user UUID, superadmin (platform, no domain)

m = g2(r.sub, "superadmin")
    || g(r.sub, p.sub, r.dom) && (r.dom == p.dom || p.dom == "*")
       && (r.obj == p.obj || p.obj == "*") && (r.act == p.act || p.act == "*")
```

Unnamed grouping APIs default to `g` — every superadmin operation **must** use the
`Named` variants with `"g2"` (a live bug caught in tests: 2-element rows against
`g` fail with "elements do not meet role definition").

## Matrices (`policy.go`)

Seeded per org on creation (`SeedOrgPolicies`, idempotent → doubles as repair):

| Role | Grants |
|---|---|
| `OWNER` | `*, *` in its org (wildcard) |
| `ADMIN` | org read/update; member read/create/delete; invite read/create/delete |
| `MEMBER` | org read; member read |

`member:update` is deliberately absent for ADMIN (role changes stay OWNER-only,
matching the org service). Event objects land with the events module.
`RemoveOrgPolicies` drops an org's `p` + `g` rows on org delete.

## Sync (`sync.go`)

`MembershipSyncer` implements org's `GroupSyncer`. Every sync **sweeps the user's
existing rows for the org first, then adds** — role changes never leave stale
rows (second live bug caught in tests: demote without sweep kept old rights).
Removals (`nil` role) sweep only; 2-field superadmin rows are never touched.
All ops idempotent.

## Superadmin

- Source: `SUPERADMIN_EMAILS` (comma-separated, lowercased; see `cmd/config`).
- `SeedSuperAdmins` at boot: unknown emails skipped (warn), missing groupings
  added, stale ones removed — **except on empty input, which is a strict no-op**
  (rotation requires an explicit list; never wipes on misconfiguration).
- Bypass is total but authenticated: still needs a valid verified login; intended
  for support/ops, never tenant flows. Future `/api/v1/admin/*` routes gate on
  `RequireSuperAdmin` (middleware ships now, routes later).
- Explicit non-goal: superadmins do **not** pass tenant gates — `RequireOrgContext`
  still 404s foreign orgs for everyone. Cross-tenant action happens only through
  the future domain-less admin surface, keeping it auditable.

## Middleware (`middleware.go`)

Chain: `RequireAuth → RequireOrgContext → RequirePermission(obj, act)`.
Denials are 403; enforcer errors fail closed (deny + warn log). Object/action
constants (`ObjOrg/ObjMember/ObjInvite/ObjEvent`, `ActRead/Create/Update/Delete/Manage`)
are the shared vocabulary — handlers never inline strings.

## Storage & ops

- `casbin_rule(id, ptype, v0..v5)` via `migrations/000004_iam_casbin`; the gorm
  adapter's own AutoMigrate stays OFF (`TurnOffAutoMigrate`).
- One enforcer singleton built in `main` (goroutine-safe for concurrent Enforce);
  `LoadPolicy()` at boot, auto-save on mutations.
- All enforcer access serializes through a package-level RWMutex (`enforcer.go`):
  Casbin's management-API reads don't self-synchronize against writes, and
  `-race` testing proved concurrent syncs corrupt the in-memory policy without
  it. Reads take RLock, mutations Lock. Single-enforcer assumption holds.
- Tests run the real stack on SQLite (adapter automigrates there): full matrix,
  cross-org isolation, demote/remove semantics, superadmin seed/rotate/bypass,
  middleware allow/deny shapes.

## Deferred (explicitly out)

Policy admin CRUD endpoints, custom roles, ABAC/conditions, watchers for
multi-instance invalidation (single process for MVP), `keyMatch` patterns.
