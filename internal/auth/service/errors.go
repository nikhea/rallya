package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrUserExists          = errors.New("email already registered")
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrEmailNotVerified    = errors.New("email not verified")
	ErrAccountSuspended    = errors.New("account suspended")
	ErrAccountDeleted      = errors.New("account deleted")
	ErrAccountInactive     = errors.New("account inactive")
	ErrInvalidToken        = errors.New("invalid or expired token")
	ErrTokenUsed           = errors.New("token already used")
	ErrOTPAttemptsExceeded = errors.New("too many wrong codes, request a new one")
	ErrSessionRevoked      = errors.New("session revoked")
	ErrTooManyRequests     = errors.New("too many requests, try again later")
)
