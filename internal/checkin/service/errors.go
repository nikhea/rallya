package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrCheckinNotFound  = errors.New("event not found")
	ErrForbidden        = errors.New("insufficient permission")
	ErrBatchTooLarge    = errors.New("batch exceeds maximum size")
	ErrQRSecretUnset    = errors.New("qr signing secret not configured")
	ErrAttendeeNotFound = errors.New("attendee not found")
	// ErrNotCheckedIn signals a revert against a row that is not
	// CHECKED_IN (never scanned, or terminally CANCELLED).
	ErrNotCheckedIn = errors.New("attendee is not checked in")
	// ErrCollectionsOutstanding blocks a revert while the attendee holds
	// non-voided kit collections (void them first — locked kit behavior).
	ErrCollectionsOutstanding = errors.New("attendee has unvoided kit collections")
)
