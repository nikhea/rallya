# AGENTS.md — IAM domain (`internal/iam/`)

Enforcement reader (Casbin RBAC with domains; domain = org UUID). No public
routes; only `RequirePermission` / `RequireSuperAdmin` middleware plus the
sync/seeder implementations org consumes.

## Rules

- Model (`model.conf`, embedded): `g = _, _, _`, `g2 = _, _` for superadmin;
  matcher needs `(p.obj == "*")`-style arms or the OWNER wildcard never fires.
- **Always `Named` grouping variants with `"g2"` for superadmin** — unnamed
  defaults to `g` and fails on 2-element rows ("elements do not meet role definition").
- Serialize ALL enforcer access via the package RWMutex (reads RLock, writes
  Lock) — Casbin management reads don't self-synchronize; `-race` proved it.
- `MembershipSyncer` sweeps-then-adds per org (stale roles never linger);
  removals sweep only; superadmin rows untouched. Everything idempotent.
- Matrices live in `policy.go` (`Admin/MemberPermissions`); `matrix_test.go`
  pins them two-way against a hardcoded table — matrix changes MUST update both.
- `SeedOrgPolicies` idempotent (doubles as repair); superadmin seed from
  `SUPERADMIN_EMAILS`, empty list is a strict no-op (never wipes on misconfig).
- `Enforce` wrapper fails closed on errors AND blank inputs (wildcards must
  never match malformed requests). Tenant gates (`RequireOrgContext`) still
  404 foreign orgs for everyone incl. superadmins — cross-tenant action only
  via future `/admin/*`.
- Adapter AutoMigrate stays OFF (`TurnOffAutoMigrate`); `migrations/000004*`
  owns `casbin_rule`. Tests may automigrate the adapter struct on SQLite.

## Tests

`go test ./internal/iam/ -count=1` (+ `-race` regularly) — exhaustive matrix,
persistence reload, row-count idempotency, concurrency, middleware shapes.
