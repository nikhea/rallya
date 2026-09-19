// Package mailer delivers multipart (text + HTML) emails over SMTP.
// When SMTP credentials are absent it logs instead of failing, so auth
// flows and workers never break for missing mail config.
package mailer

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/smtp"

	"github.com/nikhea/rallya/cmd/config"
)

// Send delivers a text+HTML email. Falls back to logging when disabled.
func Send(to, subject, textBody, htmlBody string) error {
	cfg := config.MailConfigFromEnv()
	if !config.MailEnabled() {
		slog.Info("email (mailer disabled, logging only)",
			"to", to, "subject", subject, "text", textBody)
		return nil
	}

	boundary := "rallya-mail-boundary"
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s <%s>\r\n", cfg.FromName, cfg.Address)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%s\r\n", boundary)
	fmt.Fprintf(&buf, "\r\n--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", textBody)
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n", htmlBody)
	fmt.Fprintf(&buf, "--%s--\r\n", boundary)

	addr := cfg.Host + ":" + cfg.Port
	auth := smtp.PlainAuth("", cfg.Address, cfg.Password, cfg.Host)
	if err := smtp.SendMail(addr, auth, cfg.Address, []string{to}, buf.Bytes()); err != nil {
		return fmt.Errorf("smtp send to %s: %w", to, err)
	}
	return nil
}
