-- Attendees domain: per-unit door records minted from CONFIRMED orders
-- plus organizer manual adds. Rows are never deleted (audit trail);
-- cancellation flips status. Token hashes only; raw tokens live in
-- confirmation emails and QR payloads.
CREATE TABLE IF NOT EXISTS attendees (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    order_id UUID REFERENCES orders (id) ON DELETE SET NULL,

    -- Zero-based unit position within the order (redelivery-safe mint).
    unit_index INTEGER NOT NULL DEFAULT 0,

    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,

    user_id UUID REFERENCES users (id) ON DELETE SET NULL,

    email VARCHAR(255) NOT NULL,
    name VARCHAR(255),

    -- token_hash = SHA-256 hex of the scannable token.
    token_hash TEXT NOT NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'REGISTERED'
        CHECK (status IN ('REGISTERED', 'CHECKED_IN', 'CANCELLED')),

    checked_in_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (order_id, unit_index)
);
CREATE INDEX IF NOT EXISTS idx_attendees_event_id ON attendees (event_id);
CREATE INDEX IF NOT EXISTS idx_attendees_user_id ON attendees (user_id);
CREATE INDEX IF NOT EXISTS idx_attendees_email ON attendees (email);
