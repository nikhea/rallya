// Package ratelimit is fixed-window per-IP rate limiting for the API.
// Fail-open: store errors allow the request (logged) — a cache outage must
// never brick the platform. Redis store shares state across instances;
// memory store is the single-instance fallback.
package ratelimit

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// errUnexpectedReply signals a malformed Lua reply (fail-open downstream).
var errUnexpectedReply = errors.New("unexpected rate limit reply")

// Store answers one question: is this key still within budget?
type Store interface {
	// Allow consumes one unit from key's window (limit per window).
	// Returns allowed + how long until the window resets.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, retryAfter time.Duration, err error)
}

// memoryStore is the process-local fallback (single instance).
type memoryStore struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	count   int
	resetAt time.Time
}

// NewMemoryStore builds the in-process store.
func NewMemoryStore() Store {
	return &memoryStore{buckets: map[string]*bucket{}}
}

// Allow implements Store.
func (s *memoryStore) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.buckets[key]
	if !ok || !now.Before(b.resetAt) {
		b = &bucket{resetAt: now.Add(window)}
		s.buckets[key] = b
	}
	b.count++
	if b.count <= limit {
		return true, 0, nil
	}
	return false, time.Until(b.resetAt), nil
}

// redisStore shares budgets across instances (fixed window, atomic Lua).
type redisStore struct {
	rdb *redis.Client
}

// NewRedisStore builds the shared store on an existing client.
func NewRedisStore(rdb *redis.Client) Store {
	return &redisStore{rdb: rdb}
}

// allowScript increments the counter, arms the TTL on first hit, and
// reports count + remaining TTL atomically.
const allowScript = `
local current = redis.call('INCR', KEYS[1])
if current == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return {current, redis.call('PTTL', KEYS[1])}
`

// Allow implements Store.
func (s *redisStore) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	res, err := s.rdb.Eval(ctx, allowScript, []string{key}, int64(window/time.Millisecond)).Result()
	if err != nil {
		return false, 0, err
	}
	vals, ok := res.([]any)
	if !ok || len(vals) != 2 {
		return false, 0, errUnexpectedReply
	}
	count, ok1 := vals[0].(int64)
	ttl, ok2 := vals[1].(int64)
	if !ok1 || !ok2 {
		return false, 0, errUnexpectedReply
	}
	if count <= int64(limit) {
		return true, 0, nil
	}
	retry := time.Duration(ttl) * time.Millisecond
	if retry < 0 {
		retry = window
	}
	return false, retry, nil
}

// permit logs store failures and fails open (availability over strictness).
func permit(ok bool, retry time.Duration, err error, key string) (bool, time.Duration) {
	if err != nil {
		slog.Warn("rate limit store failed, allowing", "key", key, "error", err)
		return true, 0
	}
	return ok, retry
}
