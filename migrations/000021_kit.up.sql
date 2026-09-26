-- Event kit catalog + per-attendee collection records.
-- kits: named kit types per event (e.g. "VIP pack", "T-shirt M").
-- kit_collections: one row per handout, PENDING -> COLLECTED (-> VOIDED).
CREATE TABLE kits (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    quantity_total INTEGER NOT NULL CHECK (quantity_total >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_kits_event ON kits (event_id);

CREATE TABLE kit_collections (
    id UUID PRIMARY KEY,
    kit_id UUID NOT NULL REFERENCES kits (id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    attendee_id UUID NOT NULL REFERENCES attendees (id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'PENDING',
    collected_at TIMESTAMPTZ,
    collected_by UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_kit_collections_kit ON kit_collections (kit_id);
CREATE INDEX idx_kit_collections_event ON kit_collections (event_id);
CREATE INDEX idx_kit_collections_attendee ON kit_collections (attendee_id);
-- One active collection per attendee per kit; voided rows free re-issue.
CREATE UNIQUE INDEX uq_kit_collections_active ON kit_collections (kit_id, attendee_id) WHERE status <> 'VOIDED';
-- Tablet retries: same idempotency key returns the existing record.
CREATE UNIQUE INDEX uq_kit_collections_idem ON kit_collections (kit_id, attendee_id, idempotency_key) WHERE idempotency_key <> '';

-- Backfill kit permissions for organizations created before the kit
-- module: per-org ADMIN kit rows matching SeedOrgPolicies. Idempotent
-- by construction; new orgs seed at runtime.
INSERT INTO casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', perms.role, o.id::text, perms.obj, perms.act
FROM organizations o
CROSS JOIN (VALUES
    ('ADMIN', 'kit', 'read'),
    ('ADMIN', 'kit', 'create'),
    ('ADMIN', 'kit', 'update'),
    ('ADMIN', 'kit', 'delete')
) AS perms(role, obj, act)
WHERE o.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM casbin_rule r
    WHERE r.ptype = 'p'
      AND r.v0 = perms.role
      AND r.v1 = o.id::text
      AND r.v2 = perms.obj
      AND r.v3 = perms.act
);
