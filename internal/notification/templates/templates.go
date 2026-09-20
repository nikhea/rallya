// Package templates embeds and renders the auth email templates.
// Field names must match the {{ .Field }} placeholders in the HTML files.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strings"
)

//go:embed *.html
var FS embed.FS

var (
	verifyTmpl    = template.Must(template.ParseFS(FS, "verify_email.html"))
	resetTmpl     = template.Must(template.ParseFS(FS, "reset_password.html"))
	welcomeTmpl   = template.Must(template.ParseFS(FS, "welcome_email.html"))
	inviteTmpl    = template.Must(template.ParseFS(FS, "org_invite_email.html"))
	publishedTmpl = template.Must(template.ParseFS(FS, "event_published_email.html"))
	confirmTmpl   = template.Must(template.ParseFS(FS, "order_confirmation_email.html"))
)

// VerifyEmailData feeds verify_email.html (OTP code + link button).
type VerifyEmailData struct {
	AppName    string
	Name       string
	OTP        string
	VerifyLink string
}

// ResetPasswordData feeds reset_password.html.
type ResetPasswordData struct {
	AppName   string
	Name      string
	ResetLink string
}

// WelcomeData feeds welcome_email.html.
type WelcomeData struct {
	AppName   string
	Name      string
	LoginLink string
}

// OrgInviteData feeds org_invite_email.html.
type OrgInviteData struct {
	AppName     string
	Name        string
	OrgName     string
	Role        string
	InviterName string
	InviteLink  string
}

// EventPublishedData feeds event_published_email.html.
type EventPublishedData struct {
	AppName    string
	Name       string
	OrgName    string
	EventTitle string
	EventLink  string
}

// OrderConfirmationQR is one ticket's scannable block.
type OrderConfirmationQR struct {
	ContentID string
	Payload   string
	No        int
}

// OrderConfirmationData feeds order_confirmation_email.html.
type OrderConfirmationData struct {
	AppName    string
	Name       string
	EventTitle string
	Total      int
	Codes      []OrderConfirmationQR
}

// Rendered is a subject + multipart body pair.
type Rendered struct {
	Subject string
	HTML    string
	Text    string
}

func render(t *template.Template, data any) string {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("render email template: %v", err))
	}
	return buf.String()
}

// RenderVerification builds the verify email (code + link).
func RenderVerification(d VerifyEmailData) Rendered {
	text := fmt.Sprintf("Hi %s,\n\nThanks for registering on %s.\n\n"+
		"Your verification code (expires in 10 minutes): %s\n\n"+
		"Or verify with this link (valid 24 hours):\n%s\n\n"+
		"If you didn't create this account, ignore this email.\n",
		d.Name, d.AppName, d.OTP, d.VerifyLink)
	return Rendered{
		Subject: fmt.Sprintf("Verify your email — %s", d.AppName),
		HTML:    render(verifyTmpl, d),
		Text:    text,
	}
}

// RenderPasswordReset builds the reset email (link only).
func RenderPasswordReset(d ResetPasswordData) Rendered {
	text := fmt.Sprintf("Hi %s,\n\nYou requested a password reset on %s.\n\n"+
		"Set a new password with this link (valid 1 hour):\n%s\n\n"+
		"If you didn't request this, ignore this email.\n",
		d.Name, d.AppName, d.ResetLink)
	return Rendered{
		Subject: fmt.Sprintf("Reset your password — %s", d.AppName),
		HTML:    render(resetTmpl, d),
		Text:    text,
	}
}

// RenderWelcome builds the post-verification welcome email.
func RenderWelcome(d WelcomeData) Rendered {
	text := fmt.Sprintf("Hi %s,\n\nYour email is verified — welcome to %s!\n\n"+
		"Get started here:\n%s\n",
		d.Name, d.AppName, d.LoginLink)
	return Rendered{
		Subject: fmt.Sprintf("Welcome to %s", d.AppName),
		HTML:    render(welcomeTmpl, d),
		Text:    text,
	}
}

// RenderOrgInvite builds the organization invitation email.
func RenderOrgInvite(d OrgInviteData) Rendered {
	text := fmt.Sprintf("Hi %s,\n\n%s invited you to join %s as %s.\n\n"+
		"Accept here (valid 7 days):\n%s\n\n"+
		"If you don't want to join, ignore this email.\n",
		d.Name, d.InviterName, d.OrgName, d.Role, d.InviteLink)
	return Rendered{
		Subject: fmt.Sprintf("You are invited to %s — %s", d.OrgName, d.AppName),
		HTML:    render(inviteTmpl, d),
		Text:    text,
	}
}

// RenderEventPublished builds the publish announcement email.
func RenderEventPublished(d EventPublishedData) Rendered {
	text := fmt.Sprintf("Hi %s,\n\n%s just published %s.\n\n"+
		"View it here:\n%s\n\n"+
		"Turn off announcements in your organization settings to stop these.\n",
		d.Name, d.OrgName, d.EventTitle, d.EventLink)
	return Rendered{
		Subject: fmt.Sprintf("%s is live — %s", d.EventTitle, d.AppName),
		HTML:    render(publishedTmpl, d),
		Text:    text,
	}
}

// RenderOrderConfirmation builds the buyer confirmation email.
func RenderOrderConfirmation(d OrderConfirmationData) Rendered {
	var text strings.Builder
	fmt.Fprintf(&text, "Hi %s,\n\nYour order for %s is confirmed (%d ticket(s)).\n\n",
		d.Name, d.EventTitle, d.Total)
	for _, c := range d.Codes {
		fmt.Fprintf(&text, "Ticket %d of %d code:\n%s\n\n", c.No, d.Total, c.Payload)
	}
	text.WriteString("Show a code at the door to check in.\n")
	return Rendered{
		Subject: fmt.Sprintf("Your tickets for %s — %s", d.EventTitle, d.AppName),
		HTML:    render(confirmTmpl, d),
		Text:    text.String(),
	}
}
