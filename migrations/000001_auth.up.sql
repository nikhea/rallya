-- Auth domain schema (MVP multi-tenant event platform).
-- Owns User identity only. Org membership/permissions live in
-- organization and iam domains (separate migrations).
--
-- Tables:
--   users, user_profiles, credentials, sessions, refresh_tokens,
--   email_verifications, password_resets, oauth_accounts,
--   mfa_factors, login_attempts

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- 1. users: core identity ------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    email VARCHAR(255) UNIQUE NOT NULL,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,

    phone VARCHAR(30),
    phone_verified BOOLEAN NOT NULL DEFAULT FALSE,

    status VARCHAR(50) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'SUSPENDED', 'DELETED')),

    last_login_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_users_status ON users (status);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at)
    WHERE deleted_at IS NOT NULL;

-- 2. user_profiles: display data, separate from auth ----------------------
CREATE TABLE IF NOT EXISTS user_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID UNIQUE NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    first_name VARCHAR(100),
    last_name VARCHAR(100),

    avatar_url TEXT,

    timezone VARCHAR(50),
    locale VARCHAR(10),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. credentials: password hashes only (Argon2id/bcrypt) -----------------
CREATE TABLE IF NOT EXISTS credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID UNIQUE NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    password_hash TEXT NOT NULL,

    password_changed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 4. sessions: logged-in devices ------------------------------------------
CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    ip_address INET,
    user_agent TEXT,
    device_name VARCHAR(255),

    last_active_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_revoked_at ON sessions (revoked_at)
    WHERE revoked_at IS NOT NULL;

-- 5. refresh_tokens: store hash(refresh_token) only -----------------------
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    session_id UUID NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,

    token_hash TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    revoked_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_session_id ON refresh_tokens (session_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_revoked_at ON refresh_tokens (revoked_at)
    WHERE revoked_at IS NOT NULL;

-- 6. email_verifications ---------------------------------------------------
CREATE TABLE IF NOT EXISTS email_verifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    token_hash TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    verified_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_email_verifications_user_id ON email_verifications (user_id);

-- 7. password_resets -------------------------------------------------------
CREATE TABLE IF NOT EXISTS password_resets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    token_hash TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    used_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_password_resets_user_id ON password_resets (user_id);

-- 8. oauth_accounts: Google/GitHub/Apple/Microsoft ------------------------
CREATE TABLE IF NOT EXISTS oauth_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    provider VARCHAR(50) NOT NULL
        CHECK (provider IN ('GOOGLE', 'GITHUB', 'APPLE', 'MICROSOFT')),
    provider_account_id VARCHAR(255) NOT NULL,

    access_token TEXT,
    refresh_token TEXT,

    expires_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ,

    UNIQUE (provider, provider_account_id)
);
CREATE INDEX IF NOT EXISTS idx_oauth_accounts_user_id ON oauth_accounts (user_id);

-- 9. mfa_factors (future ready: TOTP / SMS / PASSKEY) ----------------------
CREATE TABLE IF NOT EXISTS mfa_factors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    type VARCHAR(50) CHECK (type IN ('TOTP', 'SMS', 'PASSKEY')),

    secret TEXT,

    verified BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_mfa_factors_user_id ON mfa_factors (user_id);

-- 10. login_attempts: brute-force / rate-limit telemetry (append-only) ----
CREATE TABLE IF NOT EXISTS login_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    email VARCHAR(255),
    user_id UUID REFERENCES users (id) ON DELETE SET NULL,

    ip_address INET,

    success BOOLEAN NOT NULL,

    failure_reason VARCHAR(255),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_login_attempts_email ON login_attempts (email);
CREATE INDEX IF NOT EXISTS idx_login_attempts_user_id ON login_attempts (user_id);
CREATE INDEX IF NOT EXISTS idx_login_attempts_created_at ON login_attempts (created_at);
