-- Backfill ticket permissions for organizations created before the
-- ticketing module: per-org ADMIN/MEMBER ticket rows matching SeedOrgPolicies.
-- Idempotent by construction; new orgs seed at runtime.
INSERT INTO casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', perms.role, o.id::text, perms.obj, perms.act
FROM organizations o
CROSS JOIN (VALUES
    ('ADMIN', 'ticket', 'read'),
    ('ADMIN', 'ticket', 'create'),
    ('ADMIN', 'ticket', 'update'),
    ('ADMIN', 'ticket', 'delete'),
    ('MEMBER', 'ticket', 'read')
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
