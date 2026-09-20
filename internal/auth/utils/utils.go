// Package utils holds pure, dependency-free helpers for the auth service.
// Methods needing the repository or enqueuer stay on AuthService.
package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/nikhea/rallya/cmd/config"
	"github.com/nikhea/rallya/internal/auth/model"
)

// NormalizeEmail lowercases and trims an address for storage and lookup.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// DisplayName prefers a profile first name, else the email local part.
func DisplayName(email string, firstName *string) string {
	if firstName != nil && strings.TrimSpace(*firstName) != "" {
		return strings.TrimSpace(*firstName)
	}
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return email
}

// DisplayNameOf derives the greeting name from a loaded user.
func DisplayNameOf(u *model.User) string {
	if u == nil {
		return ""
	}
	var first *string
	if u.Profile != nil {
		first = u.Profile.FirstName
	}
	return DisplayName(u.Email, first)
}

// GenerateOTP returns a zero-padded 6-digit code from crypto/rand.
func GenerateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// VerificationLink builds the click-through link from AppURL.
func VerificationLink(raw string) string {
	return fmt.Sprintf("%s/api/v1/auth/verify-email?token=%s", config.AppURL(), raw)
}

// ResetLink builds the password-reset link from AppURL.
func ResetLink(raw string) string {
	return fmt.Sprintf("%s/reset-password?token=%s", config.AppURL(), raw)
}

// PtrTime boxes a time value.
func PtrTime(t time.Time) *time.Time { return &t }
