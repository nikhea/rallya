-- Append-only scan log: every door attempt (success + refusal) with the
-- staffer behind it. attendee_id NULL for unscannable/unknown codes.
CREATE TABLE checkin_logs (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    attendee_id UUID REFERENCES attendees (id) ON DELETE CASCADE,
    scanned_by UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    outcome TEXT NOT NULL,
    method TEXT NOT NULL DEFAULT 'qr',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_checkin_logs_event ON checkin_logs (event_id);
CREATE INDEX idx_checkin_logs_attendee ON checkin_logs (attendee_id);

-- Backfill checkin permissions for organizations created before the
-- check-in module: per-org ADMIN checkin rows matching SeedOrgPolicies.
-- Idempotent by construction; new orgs seed at runtime.
INSERT INTO casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', perms.role, o.id::text, perms.obj, perms.act
FROM organizations o
CROSS JOIN (VALUES
    ('ADMIN', 'checkin', 'read'),
    ('ADMIN', 'checkin', 'create')
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
