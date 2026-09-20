package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrCheckinNotFound  = errors.New("event not found")
	ErrForbidden        = errors.New("insufficient permission")
	ErrBatchTooLarge    = errors.New("batch exceeds maximum size")
	ErrQRSecretUnset    = errors.New("qr signing secret not configured")
	ErrAttendeeNotFound = errors.New("attendee not found")
)
