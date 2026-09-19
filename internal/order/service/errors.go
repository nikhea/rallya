package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrOrderNotFound = errors.New("order not found")
	ErrForbidden     = errors.New("insufficient permission")
	ErrTicketGone    = errors.New("ticket type unavailable")
	ErrSoldOut       = errors.New("sold out")
	ErrTooMany       = errors.New("quantity exceeds per-order limit")
	ErrInvalidQty    = errors.New("quantity must be positive")
	ErrInvalidStatus = errors.New("invalid order status")
)
