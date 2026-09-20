-- Backfill event permissions for organizations created before the events
-- module: per-org ADMIN/MEMBER event rows matching SeedOrgPolicies.
-- Idempotent by construction (unique guard below); new orgs seed at runtime.
INSERT INTO casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', perms.role, o.id::text, perms.obj, perms.act
FROM organizations o
CROSS JOIN (VALUES
    ('ADMIN', 'event', 'read'),
    ('ADMIN', 'event', 'create'),
    ('ADMIN', 'event', 'update'),
    ('ADMIN', 'event', 'delete'),
    ('ADMIN', 'event', 'publish'),
    ('MEMBER', 'event', 'read')
) AS perms(role, obj, act)
WHERE NOT EXISTS (
    SELECT 1 FROM casbin_rule r
    WHERE r.ptype = 'p'
      AND r.v0 = perms.role
      AND r.v1 = o.id::text
      AND r.v2 = perms.obj
      AND r.v3 = perms.act
);
