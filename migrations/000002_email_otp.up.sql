-- Email OTP codes for verification (10-minute 6-digit codes).
-- Used alongside the 24h link tokens in email_verifications:
-- the verify email carries both the code and the link.
CREATE TABLE IF NOT EXISTS email_otp_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- code_hash = SHA-256 hex of the zero-padded 6-digit code.
    -- Raw codes only ever appear in the email body / job args.
    code_hash TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    attempts INTEGER NOT NULL DEFAULT 0,

    consumed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_email_otp_codes_user_id ON email_otp_codes (user_id);
CREATE INDEX IF NOT EXISTS idx_email_otp_codes_pending ON email_otp_codes (user_id, created_at)
    WHERE consumed_at IS NULL;
