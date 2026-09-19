// Package utils holds pure helpers for the events service.
package utils

import (
	"strings"
	"time"
)

// Slugify lowercases a title into a URL slug.
func Slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
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

// FormatTime renders UTC RFC3339 for wire shapes.
func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// FormatTimePtr renders an optional time (nil -> nil).
func FormatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := FormatTime(*t)
	return &s
}
