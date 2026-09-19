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

// ConnectRedis dials Redis (REDIS_ADDR, REDIS_PASSWORD, REDIS_DB) and
// verifies with a ping. Unlike the database, failure is non-fatal: it
// warns and leaves RDB nil so the app runs without cache.
func ConnectRedis() {
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

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		slog.Warn("Redis unreachable, caching disabled", "addr", addr, "error", err)
		_ = client.Close()
		return
	}

	RDB = client
	slog.Info("Redis connected", "addr", addr, "db", db)
}

// CloseRedis releases the client; nil-safe for when caching is disabled.
func CloseRedis() error {
	if RDB == nil {
		return nil
	}
	return RDB.Close()
}
