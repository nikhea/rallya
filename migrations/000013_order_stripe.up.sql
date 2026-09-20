-- Stripe checkout linkage for orders. Session IDs reconcile webhooks;
-- payment intent + paid timestamp record completed charges.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS stripe_session_id TEXT UNIQUE,
    ADD COLUMN IF NOT EXISTS stripe_payment_intent_id TEXT,
    ADD COLUMN IF NOT EXISTS paid_at TIMESTAMPTZ;
