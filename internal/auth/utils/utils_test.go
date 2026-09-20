package utils

import (
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  JOHN@Test.COM "); got != "john@test.com" {
		t.Fatalf("got %q", got)
	}
}

func TestDisplayName(t *testing.T) {
	if got := DisplayName("john@test.com", strPtr("  John  ")); got != "John" {
		t.Fatalf("got %q", got)
	}
	if got := DisplayName("john@test.com", nil); got != "john" {
		t.Fatalf("got %q", got)
	}
	if got := DisplayName("john@test.com", strPtr("")); got != "john" {
		t.Fatalf("got %q", got)
	}
	if got := DisplayName("", nil); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := DisplayNameOf(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestGenerateOTPFormat(t *testing.T) {
	for i := 0; i < 50; i++ {
		otp, err := GenerateOTP()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if len(otp) != 6 {
			t.Fatalf("expected 6 chars, got %q", otp)
		}
		for _, r := range otp {
			if r < '0' || r > '9' {
				t.Fatalf("non-digit in %q", otp)
			}
		}
	}
}

func TestLinks(t *testing.T) {
	t.Setenv("APP_URL", "https://example.com")
	if got := VerificationLink("abc"); got != "https://example.com/api/v1/auth/verify-email?token=abc" {
		t.Fatalf("got %q", got)
	}
	if got := ResetLink("abc"); got != "https://example.com/reset-password?token=abc" {
		t.Fatalf("got %q", got)
	}
	if PtrTime(time.Now()).IsZero() {
		t.Fatal("expected non-zero time")
	}
}

func strPtr(s string) *string { return &s }
