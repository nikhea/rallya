# AGENTS.md — Organization domain (`internal/organization/`)

Tenants: orgs, fixed-role memberships (`OWNER > ADMIN > MEMBER`), invites.
Consumes auth via `auth.UserReader` only. IAM implements this domain's
`GroupSyncer` + `PolicySeeder` (declared in `service/`, never import iam).

## Rules

- `:id` params accept UUID **or** slug; non-members get stealth 404s everywhere
  (never confirm org existence to outsiders).
- Roles: ADMIN+ manages, OWNER-only for role changes/deletes; grant role must
  not exceed grantor's; last OWNER can never be demoted/removed (fail closed
  on counting errors too).
- Slugs: empty input auto-derives from name and uniquifies (`acme`, `acme-2`,
  …); explicit taken slugs 409. DB check constraint backs the regex.
- Invites: 7d TTL, token hashes only, supersede-on-reinvite; accept needs
  account-email match (idempotent re-accept); decline is the reject path
  (idempotent; accepted invites 409).
- `notify_events` (default TRUE) drives announcement targeting; self-service
  via `PATCH members/me`. `NotifyTargets`/`ResolveOrgID`/`IsMember`/`OrgName`
  back the event domain's `OrgResolver`.
- Membership mutations sync groupings post-commit (best-effort + loud log;
  `SyncUserPolicies` repairs). Org create/delete seeds/removes Casbin policies.
- `MemberView`/`OrgDetail` style shapes are banned outside `dto/` — services
  return `orgdto` types directly (private mappers stay in service).

## Tests

`go test ./internal/organization/... -count=1` — guards matrix, last-owner,
invite lifecycle, HTTP suite on the real auth stack (shared SQLite).
