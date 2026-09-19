// Package utils holds pure, dependency-free helpers for the organization
// service. Methods needing the repository, users reader, enqueuer, or
// syncer stay on OrgService.
package utils

import (
	"fmt"
	"strings"

	"github.com/nikhea/rallya/cmd/config"
	"github.com/nikhea/rallya/internal/auth/model"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
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

// Slugify lowercases a name into a URL slug (letters, numbers, hyphens).
// Returns "" when nothing slug-safe remains (callers fall back to "org").
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == ' ' || r == '-' || r == '_':
			if !dash && b.Len() > 0 {
				b.WriteRune('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// InviteLink builds the invite-accept link from AppURL.
func InviteLink(raw string) string {
	return fmt.Sprintf("%s/orgs/invites/accept?token=%s", config.AppURL(), raw)
}

// IsUniqueViolation reports duplicate-key errors across drivers:
// Postgres (`duplicate key ...`) and SQLite (`UNIQUE constraint failed`).
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}

// PtrRole boxes a member role.
func PtrRole(r orgmodel.MemberRole) *orgmodel.MemberRole { return &r }
