-- API keys for server-to-server SDK use (per-organization, first-class principals).
-- Raw secret shown once at creation; only SHA-256 hash hits this table.
-- Keys act as their creator for membership/Casbin checks (fail-closed when
-- the creator loses membership) and are scope-intersected in middleware.

CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    org_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,

    created_by UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    name VARCHAR(100) NOT NULL,

    prefix VARCHAR(32) NOT NULL,

    key_hash TEXT NOT NULL UNIQUE,

    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,

    expires_at TIMESTAMPTZ,

    last_used_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_api_keys_org_id ON api_keys (org_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_created_by ON api_keys (created_by);
CREATE INDEX IF NOT EXISTS idx_api_keys_revoked_at ON api_keys (revoked_at)
    WHERE revoked_at IS NOT NULL;
