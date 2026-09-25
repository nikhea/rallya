package token

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// ApiKeyPrefix is the human-visible key family tag (Stripe-style).
const ApiKeyPrefix = "rk_live_"

// GenerateApiKey mints a raw org API key. Returns the full raw secret
// (show once, never store), its SHA-256 hash (store), and the display
// prefix (store). Raw format: rk_live_<43 base64url chars from 32B>.
func GenerateApiKey() (raw, hash, prefix string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("generate api key: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	raw = ApiKeyPrefix + secret
	hash = HashToken(raw)
	frag := secret
	if len(frag) > 8 {
		frag = frag[:8]
	}
	prefix = ApiKeyPrefix + frag
	return raw, hash, prefix, nil
}

// IsApiKeyShape reports whether s looks like an issued org API key.
// Used to avoid hashing obvious non-keys; validation still fails closed.
func IsApiKeyShape(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, ApiKeyPrefix) {
		return false
	}
	return len(s) > len(ApiKeyPrefix)+8
}
