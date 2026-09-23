package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nikhea/rallya/internal/cache"
)

func TestDisabledStore(t *testing.T) {
	var nilStore *cache.Store
	for _, s := range []*cache.Store{nil, cache.New(nil), nilStore} {
		if s.Enabled() {
			t.Fatalf("nil store must report disabled")
		}
		var dst map[string]string
		if hit, err := s.Get(context.Background(), "k", &dst); hit || err != nil {
			t.Fatalf("disabled Get = (%v, %v), want (false, nil)", hit, err)
		}
		if err := s.Set(context.Background(), "k", "v", time.Minute); err != nil {
			t.Fatalf("disabled Set = %v, want nil", err)
		}
		if err := s.Del(context.Background(), "k"); err != nil {
			t.Fatalf("disabled Del = %v, want nil", err)
		}
	}
}

// dialTestRedis connects to local Redis; skips where absent (CI).
func dialTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("no redis: %v", err)
	}
	return rdb
}

type payload struct {
	Title string `json:"title"`
	N     int    `json:"n"`
}

func TestRoundTripAndDel(t *testing.T) {
	rdb := dialTestRedis(t)
	defer rdb.Close()
	s, ctx := cache.New(rdb), context.Background()
	key := "cache-test-roundtrip"
	_ = rdb.Del(ctx, key).Err()

	want := payload{Title: "Show", N: 3}
	if err := s.Set(ctx, key, want, time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	var got payload
	hit, err := s.Get(ctx, key, &got)
	if err != nil || !hit {
		t.Fatalf("get = (%v, %v), want (true, nil)", hit, err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if err := s.Del(ctx, key); err != nil {
		t.Fatalf("del: %v", err)
	}
	hit, err = s.Get(ctx, key, &got)
	if err != nil || hit {
		t.Fatalf("post-del get = (%v, %v), want (false, nil)", hit, err)
	}
}

func TestExpiry(t *testing.T) {
	rdb := dialTestRedis(t)
	defer rdb.Close()
	s, ctx := cache.New(rdb), context.Background()
	key := "cache-test-expiry"
	_ = rdb.Del(ctx, key).Err()

	if err := s.Set(ctx, key, payload{Title: "x"}, 50*time.Millisecond); err != nil {
		t.Fatalf("set: %v", err)
	}
	var got payload
	if hit, err := s.Get(ctx, key, &got); err != nil || !hit {
		t.Fatalf("immediate get = (%v, %v), want hit", hit, err)
	}
	time.Sleep(150 * time.Millisecond)
	if hit, err := s.Get(ctx, key, &got); err != nil || hit {
		t.Fatalf("post-expiry get = (%v, %v), want miss", hit, err)
	}
}

func TestCorruptPayloadIsError(t *testing.T) {
	rdb := dialTestRedis(t)
	defer rdb.Close()
	s, ctx := cache.New(rdb), context.Background()
	key := "cache-test-corrupt"
	if err := rdb.Set(ctx, key, "not-json{{{", time.Minute).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// NOTE: do NOT write `defer rdb.Del(ctx, key).Err()` — the Del would
	// execute immediately (only .Err() is deferred), wiping the seed.
	defer func() { _ = rdb.Del(ctx, key).Err() }()
	var got payload
	if hit, err := s.Get(ctx, key, &got); hit || err == nil {
		t.Fatalf("corrupt get = (%v, %v), want (false, err)", hit, err)
	}
}
