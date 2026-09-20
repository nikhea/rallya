package templates

import (
	"strings"
	"testing"
)

func TestRenderEventPublished(t *testing.T) {
	r := RenderEventPublished(EventPublishedData{
		AppName: "Rallya", Name: "Jane", OrgName: "Acme",
		EventTitle: "Summer Fest", EventLink: "http://localhost:8080/events/abc",
	})
	if r.Subject == "" || r.HTML == "" || r.Text == "" {
		t.Fatal("expected subject, html, and text")
	}
	for _, want := range []string{"Summer Fest", "Acme", "Jane", "/events/abc"} {
		if !strings.Contains(r.HTML, want) {
			t.Fatalf("html missing %q", want)
		}
		if !strings.Contains(r.Text, want) {
			t.Fatalf("text missing %q", want)
		}
	}
}
