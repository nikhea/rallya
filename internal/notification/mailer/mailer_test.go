package mailer

import "testing"

func TestSendDisabledLogsOnly(t *testing.T) {
	t.Setenv("EMAIL_ADDRESS", "")
	t.Setenv("EMAIL_PASSWORD", "")
	if err := Send("jane@test.com", "Subject", "text body", "<p>html</p>"); err != nil {
		t.Fatalf("disabled mailer must not fail, got %v", err)
	}
}

func TestSendRequiresCredentials(t *testing.T) {
	// Only address set: still disabled (needs password too).
	t.Setenv("EMAIL_ADDRESS", "bot@test.com")
	t.Setenv("EMAIL_PASSWORD", "")
	if err := Send("jane@test.com", "Subject", "text", "<p>html</p>"); err != nil {
		t.Fatalf("expected log-only send, got %v", err)
	}
}
