// Package cache is a nil-safe JSON cache-aside over Redis.
//
// A nil client means "caching disabled": Get always misses, Set/Del are
// no-ops. Domains declare their own consumer interface over these three
// methods (see the event service's DetailCache) and stay correct with or
// without Redis — an outage degrades reads to Postgres, never to errors.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store is a JSON codec over a Redis client. The zero value is a disabled
// store; prefer New. Safe for concurrent use (the client is goroutine-safe).
type Store struct {
	rdb *redis.Client
}

// New builds a Store. A nil client disables caching (fail-open).
func New(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

// Enabled reports whether operations reach Redis.
func (s *Store) Enabled() bool {
	return s != nil && s.rdb != nil
}

// Get fetches key into dst (JSON decode). A miss or a disabled store
// returns (false, nil); transport and codec failures return (false, err)
// so callers can log and fall through to the database.
func (s *Store) Get(ctx context.Context, key string, dst any) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	raw, err := s.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return false, err
	}
	return true, nil
}

// Set stores val under key (JSON encode) with a TTL. A disabled store is
// a no-op. TTL must be positive; zero/negative expires immediately.
func (s *Store) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	if !s.Enabled() {
		return nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, key, raw, ttl).Err()
}

// Del drops keys. A disabled store (or empty key list) is a no-op.
func (s *Store) Del(ctx context.Context, keys ...string) error {
	if !s.Enabled() || len(keys) == 0 {
		return nil
	}
	return s.rdb.Del(ctx, keys...).Err()
}
