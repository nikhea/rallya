// Package templates embeds and renders the auth email templates.
// Field names must match the {{ .Field }} placeholders in the HTML files.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
)

//go:embed *.html
var FS embed.FS

var (
	verifyTmpl  = template.Must(template.ParseFS(FS, "verify_email.html"))
	resetTmpl   = template.Must(template.ParseFS(FS, "reset_password.html"))
	welcomeTmpl = template.Must(template.ParseFS(FS, "welcome_email.html"))
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
