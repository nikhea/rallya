-- Roll back organization domain (reverse dependency order).
DROP TABLE IF EXISTS organization_invites;
DROP TABLE IF EXISTS organization_memberships;
DROP TABLE IF EXISTS organizations;
