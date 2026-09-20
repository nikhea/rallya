-- Roll back announcement preference (data loss accepted: preference only).
ALTER TABLE organization_memberships DROP COLUMN IF EXISTS notify_events;
