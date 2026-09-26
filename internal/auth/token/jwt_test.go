package token

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGenerateParseRoundtrip(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-abcdefgh")
	userID := uuid.New()
	sessionID := uuid.New()

	raw, err := GenerateAccessToken(secret, 15*time.Minute, userID, sessionID)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	gotUser, gotSession, err := ParseAccessToken(secret, raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if gotUser != userID || gotSession != sessionID {
		t.Fatalf("got (%v, %v), want (%v, %v)", gotUser, gotSession, userID, sessionID)
	}
}

func TestParseRejectsWrongSecret(t *testing.T) {
	raw, err := GenerateAccessToken([]byte("test-secret-32-bytes-long-abcdefgh"), time.Minute, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, _, err := ParseAccessToken([]byte("different-secret-32-bytes-xxxxxxxx"), raw); err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-abcdefgh")
	raw, err := GenerateAccessToken(secret, -time.Minute, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, _, err := ParseAccessToken(secret, raw); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestParseRejectsTampered(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-abcdefgh")
	raw, err := GenerateAccessToken(secret, time.Minute, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// Flip the first signature char: fully significant base64 bits, so the
	// decoded signature always changes. (Flipping the LAST char is a no-op
	// ~1/16 of the time — trailing zero bits decode identically — which
	// made this test flaky.)
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed token: %v", raw)
	}
	first := byte('A')
	if parts[2][0] == 'A' {
		first = 'B'
	}
	tampered := parts[0] + "." + parts[1] + "." + string(first) + parts[2][1:]
	if _, _, err := ParseAccessToken(secret, tampered); err == nil {
		t.Fatal("expected error for tampered token")
	}
}
