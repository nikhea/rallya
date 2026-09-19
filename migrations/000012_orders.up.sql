-- Orders domain: claims against ticket types with hold expiry.
-- One ticket type per order. Free (0-cent) orders confirm immediately;
-- priced orders wait in PENDING_PAYMENT for the payments module.
-- PENDING holds expire via the sweep_expired_orders periodic job.
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,

    ticket_type_id UUID NOT NULL REFERENCES ticket_types (id) ON DELETE CASCADE,

    quantity INTEGER NOT NULL CHECK (quantity > 0),

    -- Price snapshot in minor units at order time (never floats).
    price_cents INTEGER NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
    currency CHAR(3) NOT NULL DEFAULT 'USD',

    status VARCHAR(20) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'PENDING_PAYMENT', 'CONFIRMED', 'CANCELLED', 'EXPIRED')),

    -- Client-supplied idempotency key; replays return the original order.
    idempotency_key VARCHAR(100),

    -- Hold deadline for PENDING orders; swept on expiry.
    expires_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (user_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders (user_id);
CREATE INDEX IF NOT EXISTS idx_orders_event_id ON orders (event_id);
CREATE INDEX IF NOT EXISTS idx_orders_expired ON orders (expires_at)
    WHERE status = 'PENDING' AND expires_at IS NOT NULL;
