-- Per-org custom roles: source of truth materialized into casbin_rule
-- (groupings per assignee + p-rows per permission). Names stored lowercase;
-- fixed names (owner/admin/member/superadmin) are banned at the service.
CREATE TABLE role_definitions (
    id UUID PRIMARY KEY,
    org_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    permissions JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, name)
);
CREATE INDEX idx_role_definitions_org ON role_definitions (org_id);

-- Member custom-role assignments. No FK to role_definitions by design:
-- definitions delete only when unassigned (409 otherwise), and org delete
-- cascades here directly.
CREATE TABLE member_custom_roles (
    org_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, user_id, role_name)
);
CREATE INDEX idx_member_custom_roles_user ON member_custom_roles (user_id);

-- Backfill role:read for pre-existing orgs (mutations stay OWNER-only:
-- no matrix row grants them, so only the OWNER wildcard passes the gate —
-- the UpdateMemberRole trick). Idempotent; new orgs seed at runtime.
INSERT INTO casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', 'ADMIN', o.id::text, 'role', 'read'
FROM organizations o
WHERE o.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM casbin_rule r
    WHERE r.ptype = 'p'
      AND r.v0 = 'ADMIN'
      AND r.v1 = o.id::text
      AND r.v2 = 'role'
      AND r.v3 = 'read'
);
