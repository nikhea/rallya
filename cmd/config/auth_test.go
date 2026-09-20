package config

import "testing"

func TestSuperAdminEmails(t *testing.T) {
	t.Setenv("SUPERADMIN_EMAILS", " Boss@Test.com ,, ops@test.com,")
	got := SuperAdminEmails()
	if len(got) != 2 || got[0] != "boss@test.com" || got[1] != "ops@test.com" {
		t.Fatalf("got %v", got)
	}
	t.Setenv("SUPERADMIN_EMAILS", "")
	if len(SuperAdminEmails()) != 0 {
		t.Fatal("expected empty")
	}
}
