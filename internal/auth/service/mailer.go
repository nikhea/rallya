package service

import (
	"fmt"

	"github.com/nikhea/rallya/cmd/config"
)

// verificationLink builds the click-through link from AppURL.
func verificationLink(raw string) string {
	return fmt.Sprintf("%s/api/v1/auth/verify-email?token=%s", config.AppURL(), raw)
}

// resetLink builds the password-reset link from AppURL.
func resetLink(raw string) string {
	return fmt.Sprintf("%s/reset-password?token=%s", config.AppURL(), raw)
}
