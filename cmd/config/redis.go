package config

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RDB is the shared Redis client. It stays nil when Redis is unreachable
// at boot — cache helpers treat a nil client as "caching disabled" so the
// API keeps serving from Postgres.
var RDB *redis.Client

// redisOptions resolves connection settings. REDIS_URL wins when set
// (e.g. redis://:password@localhost:6379/0); otherwise the split vars
// REDIS_ADDR, REDIS_PASSWORD, and REDIS_DB are used.
func redisOptions() *redis.Options {
	if raw := redisURL(); raw != "" {
		opts, err := redis.ParseURL(raw)
		if err != nil {
			slog.Warn("Invalid REDIS_URL, falling back to split vars", "error", err)
		} else {
			applyRedisTimeouts(opts)
			return opts
		}
	}

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	db := 0
	if raw := os.Getenv("REDIS_DB"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			db = n
		}
	}

	opts := &redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       db,
	}
	applyRedisTimeouts(opts)
	return opts
}

// redisURL returns the configured Redis URL. REDIS_URL is primary;
// RAILWAY-style REDIS_PRIVATE_URL (or compatible providers) is honored
// as a fallback so deploys need no extra mapping.
func redisURL() string {
	if raw := os.Getenv("REDIS_URL"); raw != "" {
		return raw
	}
	return os.Getenv("REDIS_PRIVATE_URL")
}

func applyRedisTimeouts(opts *redis.Options) {
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second
}

// ConnectRedis dials Redis and verifies with a ping. Unlike the database,
// failure is non-fatal: it warns and leaves RDB nil so the app runs
// without cache.
func ConnectRedis() {
	opts := redisOptions()

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		slog.Warn("Redis unreachable, caching disabled", "addr", opts.Addr, "error", err)
		_ = client.Close()
		return
	}

	RDB = client
	slog.Info("Redis connected", "addr", opts.Addr, "db", opts.DB)
}

// CloseRedis releases the client; nil-safe for when caching is disabled.
func CloseRedis() error {
	if RDB == nil {
		return nil
	}
	return RDB.Close()
}
