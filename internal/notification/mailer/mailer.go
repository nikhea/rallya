// Package mailer delivers multipart (text + HTML) emails over SMTP.
// When SMTP credentials are absent it logs instead of failing, so auth
// flows and workers never break for missing mail config.
package mailer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/smtp"

	"github.com/nikhea/rallya/cmd/config"
)

// Send delivers a text+HTML email. Falls back to logging when disabled.
func Send(to, subject, textBody, htmlBody string) error {
	return SendWithImages(to, subject, textBody, htmlBody, nil)
}

// InlineImage is one CID-referenced attachment (e.g. QR codes).
type InlineImage struct {
	ContentID string
	MIME      string
	Data      []byte
}

// SendWithImages delivers multipart mail with inline CID images referenced
// as <img src="cid:..."> in the HTML part. Falls back to logging when disabled.
func SendWithImages(to, subject, textBody, htmlBody string, images []InlineImage) error {
	cfg := config.MailConfigFromEnv()
	if !config.MailEnabled() {
		slog.Info("email (mailer disabled, logging only)",
			"to", to, "subject", subject, "text", textBody, "images", len(images))
		return nil
	}

	outer := "rallya-mail-outer"
	inner := "rallya-mail-inner"
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s <%s>\r\n", cfg.FromName, cfg.Address)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	if len(images) == 0 {
		fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%s\r\n", inner)
		fmt.Fprintf(&buf, "\r\n--%s\r\n", inner)
		fmt.Fprintf(&buf, "Content-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", textBody)
		fmt.Fprintf(&buf, "--%s\r\n", inner)
		fmt.Fprintf(&buf, "Content-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n", htmlBody)
		fmt.Fprintf(&buf, "--%s--\r\n", inner)
	} else {
		fmt.Fprintf(&buf, "Content-Type: multipart/related; boundary=%s\r\n", outer)
		fmt.Fprintf(&buf, "\r\n--%s\r\n", outer)
		fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", inner)
		fmt.Fprintf(&buf, "--%s\r\n", inner)
		fmt.Fprintf(&buf, "Content-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", textBody)
		fmt.Fprintf(&buf, "--%s\r\n", inner)
		fmt.Fprintf(&buf, "Content-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n", htmlBody)
		fmt.Fprintf(&buf, "--%s--\r\n\r\n", inner)
		for _, img := range images {
			mime := img.MIME
			if mime == "" {
				mime = "image/png"
			}
			fmt.Fprintf(&buf, "--%s\r\n", outer)
			fmt.Fprintf(&buf, "Content-Type: %s\r\n", mime)
			fmt.Fprintf(&buf, "Content-Transfer-Encoding: base64\r\n")
			fmt.Fprintf(&buf, "Content-ID: <%s>\r\n", img.ContentID)
			fmt.Fprintf(&buf, "Content-Disposition: inline; filename=\"%s\"\r\n\r\n", img.ContentID)
			encodeBase64Chunked(&buf, img.Data)
			fmt.Fprintf(&buf, "\r\n")
		}
		fmt.Fprintf(&buf, "--%s--\r\n", outer)
	}

	addr := cfg.Host + ":" + cfg.Port
	auth := smtp.PlainAuth("", cfg.Address, cfg.Password, cfg.Host)
	if err := smtp.SendMail(addr, auth, cfg.Address, []string{to}, buf.Bytes()); err != nil {
		return fmt.Errorf("smtp send to %s: %w", to, err)
	}
	return nil
}

// encodeBase64Chunked writes RFC 2045 base64 wrapped at 76 columns.
func encodeBase64Chunked(buf *bytes.Buffer, data []byte) {
	const width = 76
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > width {
		buf.WriteString(enc[:width] + "\r\n")
		enc = enc[width:]
	}
	buf.WriteString(enc + "\r\n")
}
