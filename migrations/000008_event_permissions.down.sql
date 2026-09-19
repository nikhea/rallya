-- Roll back event permission backfill (seed rows only).
DELETE FROM casbin_rule
WHERE ptype = 'p' AND v2 = 'event';
