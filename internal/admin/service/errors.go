package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrOrgNotFound  = errors.New("organization not found")
	ErrUserNotFound = errors.New("user not found")
)
