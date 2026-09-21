-- Unified audit trail: append-only, one row per action. org_id NULL for
-- platform-scope events; actor_id NULL for system actions (sweepers).
-- before/after carry changed-fields-only diffs, never secrets or tokens.
-- org_id is deliberately NOT a foreign key: deleting an org must not wipe
-- its paper trail (reads scope by value, not by join).
CREATE TABLE audit_events (
    id UUID PRIMARY KEY,
    org_id UUID,
    actor_id UUID REFERENCES users (id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id UUID,
    before JSONB,
    after JSONB,
    ip TEXT,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_events_org_time ON audit_events (org_id, created_at DESC);
CREATE INDEX idx_audit_events_object ON audit_events (object_type, object_id);
CREATE INDEX idx_audit_events_actor ON audit_events (actor_id);

-- Backfill audit:read for organizations created before the audit module:
-- per-org ADMIN row matching SeedOrgPolicies. Idempotent by construction.
INSERT INTO casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', 'ADMIN', o.id::text, 'audit', 'read'
FROM organizations o
WHERE o.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM casbin_rule r
    WHERE r.ptype = 'p'
      AND r.v0 = 'ADMIN'
      AND r.v1 = o.id::text
      AND r.v2 = 'audit'
      AND r.v3 = 'read'
);
