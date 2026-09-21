DELETE FROM casbin_rule WHERE ptype = 'p' AND v2 = 'audit';
DROP TABLE IF EXISTS audit_events;
