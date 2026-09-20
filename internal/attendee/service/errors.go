package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrAttendeeNotFound = errors.New("attendee not found")
	ErrForbidden        = errors.New("insufficient permission")
	ErrInvalidEmail     = errors.New("valid email is required")
	ErrInvalidStatus    = errors.New("invalid attendee status")
)
