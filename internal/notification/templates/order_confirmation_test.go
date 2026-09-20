package templates

import (
	"strings"
	"testing"
)

func TestRenderOrderConfirmation(t *testing.T) {
	r := RenderOrderConfirmation(OrderConfirmationData{
		AppName: "Rallya", Name: "Jane", EventTitle: "Fest", Total: 2,
		Codes: []OrderConfirmationQR{
			{ContentID: "qr0", Payload: "aaa.111.sig", No: 1},
			{ContentID: "qr1", Payload: "bbb.222.sig", No: 2},
		},
	})
	if r.Subject == "" || r.HTML == "" || r.Text == "" {
		t.Fatal("expected subject, html, and text")
	}
	for _, want := range []string{"Fest", "Jane", "cid:qr0", "cid:qr1", "aaa.111.sig", "TICKET 2 OF 2"} {
		if !strings.Contains(r.HTML, want) {
			t.Fatalf("html missing %q", want)
		}
	}
	for _, want := range []string{"Fest", "aaa.111.sig", "Ticket 2 of 2"} {
		if !strings.Contains(r.Text, want) {
			t.Fatalf("text missing %q", want)
		}
	}
}
