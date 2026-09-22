package config

import (
	"testing"
)

func TestRateLimitDefaults(t *testing.T) {
	t.Setenv("RATE_LIMIT_AUTH_PER_MIN", "")
	t.Setenv("RATE_LIMIT_DEFAULT_PER_MIN", "")
	if got := AuthPerMin(); got != DefaultAuthPerMin {
		t.Fatalf("auth default: got %d", got)
	}
	if got := APIPerMin(); got != DefaultAPIPerMin {
		t.Fatalf("api default: got %d", got)
	}
}

func TestRateLimitOverrides(t *testing.T) {
	t.Setenv("RATE_LIMIT_AUTH_PER_MIN", "5")
	t.Setenv("RATE_LIMIT_DEFAULT_PER_MIN", "100")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 1.2.3.4")
	if got := AuthPerMin(); got != 5 {
		t.Fatalf("auth override: got %d", got)
	}
	if got := APIPerMin(); got != 100 {
		t.Fatalf("api override: got %d", got)
	}
	proxies := TrustedProxies()
	if len(proxies) != 2 || proxies[0] != "10.0.0.0/8" || proxies[1] != "1.2.3.4" {
		t.Fatalf("proxies: got %v", proxies)
	}
}

func TestRateLimitBadValues(t *testing.T) {
	t.Setenv("RATE_LIMIT_AUTH_PER_MIN", "junk")
	t.Setenv("RATE_LIMIT_DEFAULT_PER_MIN", "0")
	t.Setenv("TRUSTED_PROXIES", "")
	if got := AuthPerMin(); got != DefaultAuthPerMin {
		t.Fatalf("bad auth falls back: got %d", got)
	}
	if got := APIPerMin(); got != DefaultAPIPerMin {
		t.Fatalf("bad api falls back: got %d", got)
	}
	if got := TrustedProxies(); len(got) != 0 {
		t.Fatalf("empty proxies: got %v", got)
	}
}
