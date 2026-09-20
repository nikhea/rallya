// Package utils holds QR payload primitives: build + verify.
// Payload: <attendeeUUID>.<randomToken>.<hmacSHA256(attendeeUUID.randomToken)>.
// Self-contained (no DB touch to route a scan); forgery-proof without the
// server secret; single-row scoped on theft. Raw tokens are scannable by
// design — expose payloads only to owners/organizers, never publicly.
package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GenerateToken returns a random 32-hex-char scannable token.
func GenerateToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// HashToken hashes a token for storage (matches auth/token.HashToken).
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// BuildPayload signs attendee+token into a QR string.
func BuildPayload(attendeeID uuid.UUID, rawToken string, secret []byte) string {
	msg := attendeeID.String() + "." + rawToken
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	return msg + "." + hex.EncodeToString(mac.Sum(nil))
}

// VerifiedPayload is a parsed, authenticated QR payload.
type VerifiedPayload struct {
	AttendeeID uuid.UUID
	RawToken   string
}

// VerifyPayload parses and authenticates a scanned string. Reasons:
// malformed (structure), forged (HMAC), unknown shape otherwise.
// Existence + status gates live in service (needs the row).
func VerifyPayload(payload string, secret []byte) (*VerifiedPayload, error) {
	parts := strings.Split(payload, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, errors.New("malformed payload")
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return nil, errors.New("malformed payload")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want, err := hex.DecodeString(parts[2])
	if err != nil || !hmac.Equal(mac.Sum(nil), want) {
		return nil, errors.New("forged payload")
	}
	return &VerifiedPayload{AttendeeID: id, RawToken: parts[1]}, nil
}

// FormatTime renders UTC RFC3339 for wire shapes.
func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// FormatTimePtr renders an optional time (nil -> nil).
func FormatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := FormatTime(*t)
	return &s
}
