# AGENTS.md — Admin domain (`internal/admin/`)

Platform reads (superadmin only). No tables, no Casbin object, no tenant
logic — a gated reader over org/auth/order repositories plus the emitter.

## Rules

- `RequireSuperAdmin` on every route (tenant gates never apply here by
  design). Non-superadmin → 403, anonymous → 401.
- Every call emits its `admin.*` audit entry AFTER the successful read
  (post-commit, nil tx — reads have no tx). Actor = caller.
- Reuse owning-domain shapes where safe (`orderdto.OrdersPage`); admin-only
  shapes live in `dto/` (`admindto` — swag fails on duplicate package names).
- New repo needs go in the OWNING domain (`ListOrgs`, `SearchUsers`
  precedent) — never query another domain's table from here.
- Repair triggers are a separate slice (do not freeload mutations here).

## Tests

`go test ./internal/admin/... -count=1` — 403/401 matrix, inventory +
detail shapes, search, audit-per-read assertions; superadmin seeded via
`iam.SeedSuperAdmins` in-fixture.
