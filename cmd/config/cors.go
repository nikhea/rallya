package config

import (
	"os"
	"strings"
)

// AllowedOrigins returns the CORS origins from CORS_ALLOWED_ORIGINS
// (comma-separated, trimmed). No hardcoded default: empty means no
// cross-origin access (fail closed — browsers block, same-origin and
// server-to-server traffic unaffected). A single "*" allows all origins
// (without credentials — see main wiring).
func AllowedOrigins() []string {
	var out []string
	for _, part := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		if o := strings.TrimSpace(part); o != "" {
			out = append(out, o)
		}
	}
	return out
}
