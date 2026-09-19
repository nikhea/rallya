package config

import (
	"log/slog"
	"os"
	"strings"
)

// MailConfig carries the SMTP credentials used for auth emails.
// Secrets always come from the environment — never hardcode them.
type MailConfig struct {
	Address  string
	Password string
	Host     string
	Port     string
	FromName string
}

// MailConfigFromEnv resolves SMTP settings. EMAIL_SERVICE=Gmail maps to
// smtp.gmail.com:587; otherwise EMAIL_HOST/EMAIL_PORT are honored.
func MailConfigFromEnv() MailConfig {
	service := strings.ToLower(strings.TrimSpace(os.Getenv("EMAIL_SERVICE")))
	host := os.Getenv("EMAIL_HOST")
	port := os.Getenv("EMAIL_PORT")

	if host == "" {
		switch service {
		case "gmail", "google":
			host = "smtp.gmail.com"
			if port == "" {
				port = "587"
			}
		default:
			host = "smtp.gmail.com"
			if port == "" {
				port = "587"
			}
		}
	}

	return MailConfig{
		Address:  strings.TrimSpace(os.Getenv("EMAIL_ADDRESS")),
		Password: os.Getenv("EMAIL_PASSWORD"),
		Host:     host,
		Port:     port,
		FromName: "Grip",
	}
}

// MailEnabled reports whether SMTP credentials are present.
// When false, callers must log emails instead of failing requests.
func MailEnabled() bool {
	cfg := MailConfigFromEnv()
	if cfg.Address == "" || cfg.Password == "" {
		slog.Warn("Mailer disabled: emails will be logged only", "hint", "Set EMAIL_ADDRESS/EMAIL_PASSWORD")
		return false
	}
	return true
}
