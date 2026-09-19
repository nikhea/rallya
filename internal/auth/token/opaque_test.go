package token

import (
	"testing"
)

func TestGenerateRawTokenUnique(t *testing.T) {
	a, err := GenerateRawToken(32)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	b, err := GenerateRawToken(32)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("expected 64 hex chars, got %d and %d", len(a), len(b))
	}
	if a == b {
		t.Fatal("expected unique tokens")
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	h1 := HashToken("some-raw-token")
	h2 := HashToken("some-raw-token")
	if h1 != h2 {
		t.Fatal("expected deterministic hash")
	}
	if h1 == "some-raw-token" {
		t.Fatal("hash must not equal raw value")
	}
	if HashToken("other-token") == h1 {
		t.Fatal("different inputs must hash differently")
	}
}
