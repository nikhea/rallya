package ratelimit

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// skipPrefixes bypass limiting (health probes, API docs).
var skipPrefixes = []string{"/health", "/swagger"}

// Limit returns per-IP fixed-window middleware: perMin requests per minute.
// Denials are 429 + Retry-After (seconds) in the standard error envelope.
// Tier names namespace buckets ("auth", "default").
func Limit(store Store, tier string, perMin int) gin.HandlerFunc {
	if perMin <= 0 {
		perMin = 1
	}
	window := time.Minute
	return func(c *gin.Context) {
		for _, p := range skipPrefixes {
			if strings.HasPrefix(c.Request.URL.Path, p) {
				c.Next()
				return
			}
		}
		key := "rl:" + tier + ":" + c.ClientIP()
		ok, retry, err := store.Allow(c.Request.Context(), key, perMin, window)
		if allowed, _ := permit(ok, retry, err, key); !allowed {
			secs := int(retry.Round(time.Second).Seconds())
			if secs < 1 {
				secs = 1
			}
			c.Header("Retry-After", fmt.Sprintf("%d", secs))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}
