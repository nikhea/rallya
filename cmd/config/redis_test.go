package config

import (
	"testing"
)

func TestRedisOptionsFromURL(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://:s3cret@redis-host:6380/2")
	t.Setenv("REDIS_ADDR", "ignored:6379")
	t.Setenv("REDIS_PRIVATE_URL", "")

	opts := redisOptions()
	if opts.Addr != "redis-host:6380" {
		t.Fatalf("addr = %q", opts.Addr)
	}
	if opts.Password != "s3cret" {
		t.Fatalf("password = %q", opts.Password)
	}
	if opts.DB != 2 {
		t.Fatalf("db = %d", opts.DB)
	}
}

func TestRedisOptionsSplitVarsFallback(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	t.Setenv("REDIS_PRIVATE_URL", "")
	t.Setenv("REDIS_ADDR", "cache:6379")
	t.Setenv("REDIS_PASSWORD", "pw")
	t.Setenv("REDIS_DB", "3")

	opts := redisOptions()
	if opts.Addr != "cache:6379" || opts.Password != "pw" || opts.DB != 3 {
		t.Fatalf("unexpected opts: %+v", opts)
	}
}

func TestRedisOptionsInvalidURLFallsBack(t *testing.T) {
	t.Setenv("REDIS_URL", "://bad-url")
	t.Setenv("REDIS_PRIVATE_URL", "")
	t.Setenv("REDIS_ADDR", "fallback:6379")
	t.Setenv("REDIS_DB", "")

	opts := redisOptions()
	if opts.Addr != "fallback:6379" {
		t.Fatalf("expected fallback addr, got %+v", opts)
	}
}

func TestRedisOptionsDefaults(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	t.Setenv("REDIS_PRIVATE_URL", "")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("REDIS_DB", "")

	opts := redisOptions()
	if opts.Addr != "localhost:6379" || opts.DB != 0 {
		t.Fatalf("unexpected defaults: %+v", opts)
	}
}
