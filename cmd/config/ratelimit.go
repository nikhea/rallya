package config

import (
	"os"
	"strconv"
	"strings"
)

// DefaultAuthPerMin caps brute-forceable auth endpoints per IP per minute.
const DefaultAuthPerMin = 10

// DefaultAPIPerMin caps general API use per IP per minute (generous;
// abuse-shaped traffic is handled by lower tiers + login telemetry).
const DefaultAPIPerMin = 600

// AuthPerMin tunes the strict tier (RATE_LIMIT_AUTH_PER_MIN).
func AuthPerMin() int {
	return intEnv("RATE_LIMIT_AUTH_PER_MIN", DefaultAuthPerMin)
}

// APIPerMin tunes the default tier (RATE_LIMIT_DEFAULT_PER_MIN).
func APIPerMin() int {
	return intEnv("RATE_LIMIT_DEFAULT_PER_MIN", DefaultAPIPerMin)
}

// TrustedProxies passes TRUSTED_PROXIES (comma CIDRs/IPs) to Gin so
// ClientIP resolves through load balancers. Unset = RemoteAddr only
// (secure default; correct direct, conservative behind proxies).
func TrustedProxies() []string {
	var out []string
	for _, part := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func intEnv(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}
	return n
}
