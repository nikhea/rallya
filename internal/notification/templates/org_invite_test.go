package templates

import (
	"strings"
	"testing"
)

func TestRenderOrgInvite(t *testing.T) {
	r := RenderOrgInvite(OrgInviteData{
		AppName: "Rallya", Name: "jane", OrgName: "Acme Inc",
		Role: "ADMIN", InviterName: "John",
		InviteLink: "http://localhost:8080/orgs/invites/accept?token=abc",
	})
	if r.Subject == "" || r.HTML == "" || r.Text == "" {
		t.Fatal("expected subject, html, and text")
	}
	for _, want := range []string{"Acme Inc", "ADMIN", "John", "jane", "token=abc"} {
		if !strings.Contains(r.HTML, want) {
			t.Fatalf("html missing %q", want)
		}
		if !strings.Contains(r.Text, want) {
			t.Fatalf("text missing %q", want)
		}
	}
}
