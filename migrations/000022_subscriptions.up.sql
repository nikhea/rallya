-- Organization subscription state (Stripe Billing).
-- One row per org, created lazily on first checkout; absent row = FREE.
-- Plan + status drive entitlement resolution (quotas + feature flags);
-- attendee order checkout stays one-off and never reads this table.
CREATE TABLE org_subscriptions (
    id UUID PRIMARY KEY,
    org_id UUID NOT NULL UNIQUE REFERENCES organizations (id) ON DELETE CASCADE,
    plan TEXT NOT NULL DEFAULT 'FREE',
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    stripe_customer_id TEXT NOT NULL DEFAULT '',
    stripe_subscription_id TEXT NOT NULL DEFAULT '',
    current_period_end TIMESTAMPTZ,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    grace_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_org_subscriptions_customer ON org_subscriptions (stripe_customer_id);
CREATE INDEX idx_org_subscriptions_subscription ON org_subscriptions (stripe_subscription_id);
