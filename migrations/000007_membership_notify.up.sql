-- Per-member event announcement preference (opt-in/out for
-- publish announcements). Default TRUE per the events design grill.
ALTER TABLE organization_memberships
    ADD COLUMN IF NOT EXISTS notify_events BOOLEAN NOT NULL DEFAULT TRUE;
