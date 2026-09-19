package utils

import (
	"errors"
	"testing"

	orgmodel "github.com/nikhea/rallya/internal/organization/model"
)

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Jane@Test.COM "); got != "jane@test.com" {
		t.Fatalf("got %q", got)
	}
}

func TestDisplayName(t *testing.T) {
	if got := DisplayName("jane@test.com", strPtr("Jane")); got != "Jane" {
		t.Fatalf("got %q", got)
	}
	if got := DisplayName("jane@test.com", nil); got != "jane" {
		t.Fatalf("got %q", got)
	}
	if got := DisplayNameOf(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Acme Inc":         "acme-inc",
		"  Hello  World  ": "hello-world",
		"Rallya_Events-26": "rallya-events-26",
		"!!!":              "",
		"":                 "",
		"--a--b--":         "a-b",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Fatalf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInviteLink(t *testing.T) {
	t.Setenv("APP_URL", "https://example.com")
	if got := InviteLink("abc"); got != "https://example.com/orgs/invites/accept?token=abc" {
		t.Fatalf("got %q", got)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	if IsUniqueViolation(nil) {
		t.Fatal("nil must be false")
	}
	if !IsUniqueViolation(errors.New(`pq: duplicate key value violates unique constraint "x"`)) {
		t.Fatal("expected postgres match")
	}
	if !IsUniqueViolation(errors.New("UNIQUE constraint failed: organizations.slug")) {
		t.Fatal("expected sqlite match")
	}
	if IsUniqueViolation(errors.New("connection refused")) {
		t.Fatal("expected false")
	}
}

func TestPtrRole(t *testing.T) {
	r := PtrRole(orgmodel.MemberRoleAdmin)
	if r == nil || *r != orgmodel.MemberRoleAdmin {
		t.Fatalf("got %+v", r)
	}
}

func strPtr(s string) *string { return &s }
