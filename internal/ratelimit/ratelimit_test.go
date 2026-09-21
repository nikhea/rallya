package ratelimit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/nikhea/rallya/internal/ratelimit"
)

func init() { gin.SetMode(gin.TestMode) }

func TestMiddlewareAllowsThenDenies(t *testing.T) {
	r := gin.New()
	r.Use(ratelimit.Limit(ratelimit.NewMemoryStore(), "test", 2))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	hit := func() int {
		req := httptest.NewRequest("GET", "/ping", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if code := hit(); code != 200 {
		t.Fatalf("first must pass, got %d", code)
	}
	if code := hit(); code != 200 {
		t.Fatalf("second must pass, got %d", code)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ping", nil)
	req.RemoteAddr = "9.9.9.9:1234"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third must 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatalf("429 must carry Retry-After")
	}
	// Other IPs unaffected.
	req2 := httptest.NewRequest("GET", "/ping", nil)
	req2.RemoteAddr = "9.9.9.10:1234"
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("other IP must pass, got %d", w2.Code)
	}
}

func TestMiddlewareSkipsHealth(t *testing.T) {
	r := gin.New()
	r.Use(ratelimit.Limit(ratelimit.NewMemoryStore(), "test", 1))
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("health must never 429, got %d", w.Code)
		}
	}
}

func TestMemoryStoreWindow(t *testing.T) {
	s := ratelimit.NewMemoryStore()
	ctx := t.Context()
	for i := 0; i < 3; i++ {
		ok, _, err := s.Allow(ctx, "k", 3, 50*time.Millisecond)
		if err != nil || !ok {
			t.Fatalf("budget 3: hit %d must pass", i)
		}
	}
	if ok, _, _ := s.Allow(ctx, "k", 3, 50*time.Millisecond); ok {
		t.Fatalf("4th must deny")
	}
	time.Sleep(60 * time.Millisecond)
	if ok, _, _ := s.Allow(ctx, "k", 3, 50*time.Millisecond); !ok {
		t.Fatalf("post-window must pass")
	}
}

// TestRedisStoreAllow runs against local Redis; skips where absent (CI).
func TestRedisStoreAllow(t *testing.T) {
	url := os.Getenv("REDIS_URL_TEST")
	if url == "" {
		url = "redis://localhost:6379/0"
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr(url)})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("no redis: %v", err)
	}
	s := ratelimit.NewRedisStore(rdb)
	key := "rl-test-allow"
	_ = rdb.Del(ctx, key).Err()
	for i := 0; i < 2; i++ {
		ok, _, err := s.Allow(ctx, key, 2, time.Minute)
		if err != nil || !ok {
			t.Fatalf("hit %d must pass: %v", i, err)
		}
	}
	ok, retry, err := s.Allow(ctx, key, 2, time.Minute)
	if err != nil || ok {
		t.Fatalf("3rd must deny: %v %v", ok, err)
	}
	if retry <= 0 {
		t.Fatalf("denial must carry retry-after")
	}
	_ = rdb.Del(ctx, key).Err()
}

func redisAddr(url string) string {
	addr := strings.TrimPrefix(strings.TrimPrefix(url, "redis://"), "rediss://")
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		addr = addr[:i]
	}
	return addr
}
