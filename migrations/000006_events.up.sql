-- Events domain: org-scoped events with draft/publish/cancel lifecycle.
-- Slugs unique per org (application-enforced with auto-uniquify).
-- Ticketing (types, pricing, enforcement of capacity) lands later.
CREATE TABLE IF NOT EXISTS events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,

    title VARCHAR(255) NOT NULL,
    slug VARCHAR(150) NOT NULL,

    description TEXT,

    venue VARCHAR(255),
    location VARCHAR(255),

    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at),

    -- NULL = unlimited; unenforced until ticketing.
    capacity INTEGER CHECK (capacity IS NULL OR capacity > 0),

    cover_url TEXT,

    status VARCHAR(20) NOT NULL DEFAULT 'DRAFT'
        CHECK (status IN ('DRAFT', 'PUBLISHED', 'CANCELLED')),

    created_by UUID REFERENCES users (id) ON DELETE SET NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ,

    UNIQUE (organization_id, slug)
);
CREATE INDEX IF NOT EXISTS idx_events_org_status ON events (organization_id, status);
CREATE INDEX IF NOT EXISTS idx_events_published_start ON events (starts_at)
    WHERE status = 'PUBLISHED' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_events_deleted_at ON events (deleted_at)
    WHERE deleted_at IS NOT NULL;
