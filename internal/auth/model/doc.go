// Package model holds the Auth domain persistence schema.
//
// Tables (auth boundary only, no org/membership here):
//   - users, user_profiles, credentials, sessions, refresh_tokens,
//     email_verifications, password_resets, oauth_accounts,
//     mfa_factors, login_attempts
//
// Organization membership and permissions live in the
// organization and iam domains, which consume Auth only via
// UserReader (see internal/auth/reader.go).
//
// IDs are app-generated in BeforeCreate hooks, with no DB-side default
// in the GORM tags, so the schema migrates on both Postgres and SQLite.
// The canonical DDL in migrations/ keeps gen_random_uuid() defaults for
// raw-SQL safety.
package model
