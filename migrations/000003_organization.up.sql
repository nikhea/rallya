-- Organization domain: multi-tenant orgs, memberships, invites.
-- Consumes auth.users by reference only (no changes to auth tables).
-- Fixed roles: OWNER > ADMIN > MEMBER. Fine-grained permissions land in IAM.

-- 1. organizations ----------------------------------------------------------
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    name VARCHAR(255) NOT NULL,

    slug VARCHAR(100) UNIQUE NOT NULL
        CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),

    logo_url TEXT,

    created_by UUID REFERENCES users (id) ON DELETE SET NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_organizations_deleted_at ON organizations (deleted_at)
    WHERE deleted_at IS NOT NULL;

-- 2. organization_memberships ------------------------------------------------
CREATE TABLE IF NOT EXISTS organization_memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,

    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    role VARCHAR(20) NOT NULL DEFAULT 'MEMBER'
        CHECK (role IN ('OWNER', 'ADMIN', 'MEMBER')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (organization_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_memberships_user_id ON organization_memberships (user_id);
CREATE INDEX IF NOT EXISTS idx_memberships_org_role ON organization_memberships (organization_id, role);

-- 3. organization_invites ----------------------------------------------------
CREATE TABLE IF NOT EXISTS organization_invites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,

    email VARCHAR(255) NOT NULL,

    role VARCHAR(20) NOT NULL DEFAULT 'MEMBER'
        CHECK (role IN ('OWNER', 'ADMIN', 'MEMBER')),

    -- token_hash = hash(raw invite token). Raw token only goes in email.
    token_hash TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    accepted_at TIMESTAMPTZ,
    declined_at TIMESTAMPTZ,

    invited_by UUID REFERENCES users (id) ON DELETE SET NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_invites_org_id ON organization_invites (organization_id);
CREATE INDEX IF NOT EXISTS idx_invites_email ON organization_invites (email);
CREATE INDEX IF NOT EXISTS idx_invites_pending ON organization_invites (organization_id, email)
    WHERE accepted_at IS NULL AND declined_at IS NULL;
