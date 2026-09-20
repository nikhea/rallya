// Package utils holds pure helpers for the ticketing service.
package utils

import (
	"time"
)

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
