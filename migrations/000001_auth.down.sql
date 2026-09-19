-- Roll back Auth domain schema (reverse dependency order).
DROP TABLE IF EXISTS login_attempts;
DROP TABLE IF EXISTS mfa_factors;
DROP TABLE IF EXISTS oauth_accounts;
DROP TABLE IF EXISTS password_resets;
DROP TABLE IF EXISTS email_verifications;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS user_profiles;
DROP TABLE IF EXISTS users;
