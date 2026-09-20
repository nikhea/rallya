-- Roll back Stripe linkage (charge references only).
ALTER TABLE orders
    DROP COLUMN IF EXISTS stripe_session_id,
    DROP COLUMN IF EXISTS stripe_payment_intent_id,
    DROP COLUMN IF EXISTS paid_at;
