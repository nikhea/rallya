package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// JWTSecret returns the HMAC secret used to sign access tokens.
// It fail-closes: missing, known-dev, or short (<32 byte) secrets exit
// the process unless ALLOW_INSECURE_JWT=true explicitly opts into them
// (local development only).
func JWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	insecureAllowed := os.Getenv("ALLOW_INSECURE_JWT") == "true"
	if secret == "" || secret == "dev-only-insecure-secret-change-me" {
		if insecureAllowed {
			slog.Warn("JWT_SECRET not securely set, using insecure dev fallback", "hint", "Set a 32+ byte JWT_SECRET in production")
			return []byte("dev-only-insecure-secret-change-me")
		}
		slog.Error("JWT_SECRET must be set to 32+ random bytes (or set ALLOW_INSECURE_JWT=true for local dev only)")
		os.Exit(1)
		return nil
	}
	if len(secret) < 32 {
		if insecureAllowed {
			slog.Warn("JWT_SECRET shorter than 32 bytes", "hint", "Use 32+ random bytes in production")
			return []byte(secret)
		}
		slog.Error("JWT_SECRET must be 32+ bytes", "hint", "Generate with: openssl rand -hex 32")
		os.Exit(1)
		return nil
	}
	return []byte(secret)
}

// AccessTTL returns how long access JWTs stay valid.
// Precedence: ACCESS_TTL_MINUTES, then legacy JWT_TTL_HOURS, then 15m.
func AccessTTL() time.Duration {
	if raw := os.Getenv("ACCESS_TTL_MINUTES"); raw != "" {
		if minutes, err := strconv.Atoi(raw); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
		slog.Warn("Invalid ACCESS_TTL_MINUTES, falling back", "value", raw)
	}
	if raw := os.Getenv("JWT_TTL_HOURS"); raw != "" {
		if hours, err := strconv.Atoi(raw); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return 15 * time.Minute
}

// RefreshTTL returns how long refresh tokens stay valid (default 30d).
func RefreshTTL() time.Duration {
	raw := os.Getenv("REFRESH_TTL_DAYS")
	if raw == "" {
		return 30 * 24 * time.Hour
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days <= 0 {
		slog.Warn("Invalid REFRESH_TTL_DAYS, falling back to 30d", "value", raw)
		return 30 * 24 * time.Hour
	}
	return time.Duration(days) * 24 * time.Hour
}

// JWTTTL is kept for backward compatibility; prefer AccessTTL.
func JWTTTL() time.Duration {
	return AccessTTL()
}

// AppURL is the public base URL used to build email links.
func AppURL() string {
	if url := os.Getenv("APP_URL"); url != "" {
		return url
	}
	return "http://localhost:8080"
}

// SuperAdminEmails returns platform superadmin account emails
// (comma-separated SUPERADMIN_EMAILS). Empty means no superadmins:
// rotation requires an explicit non-empty list (seeding never wipes
// existing grants on empty input).
func SuperAdminEmails() []string {
	var out []string
	for _, e := range strings.Split(os.Getenv("SUPERADMIN_EMAILS"), ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			out = append(out, e)
		}
	}
	return out
}
