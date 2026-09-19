-- Ticketing domain: ticket types per event with pricing and sale rules.
-- SOLD_OUT is computed (sold >= total), never stored. Claims/orders land later;
-- Reserve()/Release() row-locked APIs already prevent oversell.
CREATE TABLE IF NOT EXISTS ticket_types (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,

    name VARCHAR(255) NOT NULL,
    description TEXT,

    -- Integer minor units, never floats. Display divides by 100.
    price_cents INTEGER NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
    currency CHAR(3) NOT NULL DEFAULT 'USD',

    quantity_total INTEGER NOT NULL CHECK (quantity_total > 0),
    quantity_sold INTEGER NOT NULL DEFAULT 0 CHECK (quantity_sold >= 0),

    max_per_order INTEGER CHECK (max_per_order IS NULL OR max_per_order > 0),

    sale_starts_at TIMESTAMPTZ,
    sale_ends_at TIMESTAMPTZ,
    CHECK (sale_ends_at IS NULL OR sale_starts_at IS NULL OR sale_ends_at > sale_starts_at),

    status VARCHAR(20) NOT NULL DEFAULT 'DRAFT'
        CHECK (status IN ('DRAFT', 'ACTIVE', 'PAUSED')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ticket_types_event_id ON ticket_types (event_id);
CREATE INDEX IF NOT EXISTS idx_ticket_types_active ON ticket_types (event_id, status)
    WHERE status = 'ACTIVE' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_ticket_types_deleted_at ON ticket_types (deleted_at)
    WHERE deleted_at IS NOT NULL;
