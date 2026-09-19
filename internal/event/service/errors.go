package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrEventNotFound = errors.New("event not found")
	ErrForbidden     = errors.New("insufficient permission")
	ErrSlugTaken     = errors.New("slug already taken")
	ErrInvalidSlug   = errors.New("invalid slug (lowercase letters, numbers, hyphens)")
	ErrInvalidTitle  = errors.New("title is required")
	ErrInvalidDates  = errors.New("ends_at must be after starts_at")
	ErrInvalidCap    = errors.New("capacity must be positive")
	ErrInvalidStatus = errors.New("invalid status transition")
	ErrOrgUnresolved = errors.New("organization not found")
	ErrCoverTooLarge = errors.New("cover image exceeds 5MB")
	ErrCoverType     = errors.New("cover must be jpeg, png, or webp")
	ErrCoverRequired = errors.New("cover file is required")
	ErrTooManyFiles  = errors.New("at most 10 files per upload")
)
