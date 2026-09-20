-- IAM (Casbin) policy store. Shape mirrors the gorm-adapter CasbinRule
-- struct (adapter AutoMigrate stays OFF; this file is the source of truth).
-- Rows:
--   p, <ROLE>, <orgID>, <object>, <action>      -- role permission
--   p, OWNER, <orgID>, *, *                      -- owner wildcard (per org)
--   g, <userID>, <ROLE>, <orgID>                 -- membership grouping
--   g, <userID>, superadmin                      -- platform superadmin
CREATE TABLE IF NOT EXISTS casbin_rule (
    id BIGSERIAL PRIMARY KEY,

    ptype VARCHAR(100) NOT NULL,

    v0 VARCHAR(100),
    v1 VARCHAR(100),
    v2 VARCHAR(100),
    v3 VARCHAR(100),
    v4 VARCHAR(100),
    v5 VARCHAR(100)
);
CREATE INDEX IF NOT EXISTS idx_casbin_rule_ptype ON casbin_rule (ptype);
