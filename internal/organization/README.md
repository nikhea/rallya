# Organization Domain

Multi-tenant roots: orgs, fixed-role memberships, and email invitations.
Consumes auth only via `auth.UserReader`; exposes membership state to IAM
through `GroupSyncer` (declared in `service/`, implemented later by `iam`).
Permissions beyond roles are IAM's job (Casbin).

```
Auth (identity) → Organization (tenants) → IAM (permissions) → Events
```

## Architecture

Same layering as auth. Handlers are thin; guards split between middleware
(coarse role gates) and service (last-owner, grant hierarchy).

```
routes.go ── wires /api/v1/orgs (auth required on all routes)
handler/
  org_handler.go  ── bind JSON → service → status codes
  middleware.go   ── RequireOrgContext (:id UUID-or-slug → membership → ctx),
                     RequireRole(min) gate
dto/ (package orgdto) ── shapes + binding tags + swagger examples
service/
  org_service.go  ── CRUD, guards, invites, GroupSyncer emission
  groupsync.go    ── GroupSyncer interface (org declares, iam implements)
  errors.go       ── sentinel errors
repository/       ── GORM CRUD only; all in-tx reads take the tx handle
model/            ── organizations, organization_memberships, organization_invites
```

## Tables

| Table | Purpose | Key rule |
|---|---|---|
| `organizations` | Tenant root | unique slug (`^[a-z0-9]+(-[a-z0-9]+)*$`), soft delete |
| `organization_memberships` | user × org at a role | unique(user, org); `OWNER/ADMIN/MEMBER`; CASCADE |
| `organization_invites` | Email invites | token hash only; 7d TTL; `accepted_at`/`declined_at` |

Roles order `OWNER(3) > ADMIN(2) > MEMBER(1)`. Canonical DDL:
`migrations/000003_organization.up/down.sql`.

## Endpoints (`/api/v1/orgs`, all Bearer-authed)

| Method & Path | Min role | Notes |
|---|---|---|
| `POST /orgs` | — | Create; empty slug auto-derives from name and uniquifies (`acme`, `acme-2`, …); explicit taken slug → 409; caller → OWNER |
| `GET /orgs` | — | My orgs with roles (org switcher; mirrors `/auth/me`) |
| `GET /orgs/:id` | MEMBER | `:id` = UUID or slug; non-members get 404 (stealth) |
| `PATCH /orgs/:id` | ADMIN | Rename/re-slug/logo; 409 on slug clash |
| `DELETE /orgs/:id` | OWNER | Hard delete; memberships/invites cascade |
| `GET /orgs/:id/members` | MEMBER | Paginated, with identity display data |
| `POST /orgs/:id/members` | ADMIN | Direct add of a **registered** user; granted role ≤ grantor's |
| `PATCH /orgs/:id/members/:userId` | OWNER | Role change; last-OWNER demote → 409 |
| `DELETE /orgs/:id/members/:userId` | MEMBER+ route, hierarchy in service | anyone self-leaves (except last OWNER); ADMIN+ removes MEMBER/ADMIN; only OWNER removes OWNER |
| `POST /orgs/:id/invites` | ADMIN | Invite `{email, role}`; supersedes pending; email queued transactionally |
| `GET /orgs/:id/invites` | ADMIN | Pending invites, paginated |
| `DELETE /orgs/:id/invites/:inviteId` | ADMIN | Revoke |
| `POST /orgs/invites/accept` | auth'd, email match | Creates membership; idempotent for members |
| `POST /orgs/invites/decline` | auth'd, email match | "Reject" path; idempotent; accepted invites → 409 |

`GET /auth/me` embeds `organizations[]` via `dto.MembershipLister` (nil-safe).

## Cross-domain rules (modular-monolith seams)

- **org → auth**: `UserReader` (`GetUserByID/Email`) for invitee resolution, member
  display data, invite email-match. Batch note: member listing is N+1 by user;
  fine at org scale, revisit with `GetUsersByIDs` if it ever matters.
- **org → iam**: `GroupSyncer.SyncMembership(user, org, role|nil)` after every
  membership mutation (nil = removal). Best-effort post-commit: failures log loud
  (`slog.Error`), membership stands, `SyncUserPolicies` repairs. No IAM import —
  the interface lives on the consumer side.
- **No cross-domain joins**; no auth-table writes; stealth 404s (never confirm
  org existence to outsiders); forgot-style enumeration safety on invites is
  intentionally *not* applied to admin invite reads (admins see their org).

## Email integration

`send_org_invite_email` job (org name/slug, invitee, role, inviter, 7d link),
rendered from `internal/notification/templates/org_invite_email.html` by the
existing workers. Enqueued inside the invite transaction.

## Testing

```bash
go test ./internal/organization/... -count=1
```

SQLite suites: slug validation/conflicts, stealth 404s, guard matrix (403s),
last-owner demote/remove, grant hierarchy, invite accept/decline/revoke/expiry/
mismatch, job assertions via `FakeEnqueuer`, grouping assertions via stub syncer;
HTTP suite runs the real auth stack (register → verify → login → JWT) against a
shared in-memory DB.
