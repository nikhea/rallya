DELETE FROM casbin_rule WHERE ptype = 'p' AND v2 = 'role';
DROP TABLE IF EXISTS member_custom_roles;
DROP TABLE IF EXISTS role_definitions;
