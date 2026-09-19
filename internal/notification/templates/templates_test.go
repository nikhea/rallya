package templates

import (
	"strings"
	"testing"
)

func TestRenderVerification(t *testing.T) {
	r := RenderVerification(VerifyEmailData{
		AppName: "Rallya", Name: "John", OTP: "482910",
		VerifyLink: "http://localhost:8080/api/v1/auth/verify-email?token=abc",
	})
	if r.Subject == "" || r.HTML == "" || r.Text == "" {
		t.Fatal("expected subject, html, and text")
	}
	for _, want := range []string{"482910", "John", "Rallya", "token=abc"} {
		if !strings.Contains(r.HTML, want) {
			t.Fatalf("html missing %q", want)
		}
		if !strings.Contains(r.Text, want) {
			t.Fatalf("text missing %q", want)
		}
	}
}

func TestRenderPasswordReset(t *testing.T) {
	r := RenderPasswordReset(ResetPasswordData{
		AppName: "Rallya", Name: "John", ResetLink: "http://localhost:8080/reset-password?token=xyz",
	})
	for _, want := range []string{"John", "Rallya", "token=xyz"} {
		if !strings.Contains(r.HTML, want) {
			t.Fatalf("html missing %q", want)
		}
		if !strings.Contains(r.Text, want) {
			t.Fatalf("text missing %q", want)
		}
	}
}

func TestRenderWelcome(t *testing.T) {
	r := RenderWelcome(WelcomeData{
		AppName: "Rallya", Name: "John", LoginLink: "http://localhost:8080",
	})
	for _, want := range []string{"John", "Rallya", "http://localhost:8080"} {
		if !strings.Contains(r.HTML, want) {
			t.Fatalf("html missing %q", want)
		}
		if !strings.Contains(r.Text, want) {
			t.Fatalf("text missing %q", want)
		}
	}
}
