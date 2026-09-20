-- Backfill checkin:update (revert) for organizations created before the
-- revert endpoint: per-org ADMIN row matching SeedOrgPolicies.
-- Idempotent by construction; new orgs seed at runtime.
INSERT INTO casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', 'ADMIN', o.id::text, 'checkin', 'update'
FROM organizations o
WHERE o.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM casbin_rule r
    WHERE r.ptype = 'p'
      AND r.v0 = 'ADMIN'
      AND r.v1 = o.id::text
      AND r.v2 = 'checkin'
      AND r.v3 = 'update'
);
