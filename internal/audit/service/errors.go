package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrAuditNotFound = errors.New("organization not found")
	ErrForbidden     = errors.New("insufficient permission")
)
