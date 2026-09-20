package utils

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nikhea/rallya/cmd/config"
)

func testSecret(t *testing.T) []byte {
	t.Helper()
	t.Setenv("QR_SIGNING_SECRET", "test-qr-secret-32-bytes-long-abcdef")
	return config.QRSigningSecret()
}

func TestPayloadRoundtrip(t *testing.T) {
	secret := testSecret(t)
	id := uuid.New()
	raw, err := GenerateToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("expected 32 hex chars, got %q", raw)
	}
	payload := BuildPayload(id, raw, secret)
	got, err := VerifyPayload(payload, secret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.AttendeeID != id || got.RawToken != raw {
		t.Fatalf("mismatch: %+v", got)
	}
	if HashToken(raw) == raw {
		t.Fatal("hash must differ from raw")
	}
}

func TestPayloadTamperRejected(t *testing.T) {
	secret := testSecret(t)
	id := uuid.New()
	payload := BuildPayload(id, "abc123", secret)
	parts := strings.Split(payload, ".")
	// Swap token keeps structure, breaks HMAC.
	bad := parts[0] + ".zzz999." + parts[2]
	if _, err := VerifyPayload(bad, secret); err == nil {
		t.Fatal("expected forged error")
	}
	// Wrong secret.
	if _, err := VerifyPayload(payload, []byte("wrong-secret-32-bytes-long-xxxxxx")); err == nil {
		t.Fatal("expected forged error for wrong secret")
	}
	// Truncated signature.
	if _, err := VerifyPayload(parts[0]+"."+parts[1]+".abcd", secret); err == nil {
		t.Fatal("expected error for short sig")
	}
}

func TestPayloadMalformed(t *testing.T) {
	secret := testSecret(t)
	for _, bad := range []string{"", "a", "a.b", "a.b.c.d", "..", "notauuid.token.sig"} {
		if _, err := VerifyPayload(bad, secret); err == nil {
			t.Fatalf("expected malformed for %q", bad)
		}
	}
}
