package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrTicketNotFound  = errors.New("ticket type not found")
	ErrForbidden       = errors.New("insufficient permission")
	ErrInvalidName     = errors.New("name is required")
	ErrInvalidPrice    = errors.New("price_cents must be >= 0")
	ErrInvalidQty      = errors.New("quantity_total must be positive")
	ErrInvalidWindow   = errors.New("sale_ends_at must be after sale_starts_at")
	ErrInvalidStatus   = errors.New("invalid status transition")
	ErrEventCapacity   = errors.New("exceeds event capacity")
	ErrEventUnresolved = errors.New("event not found")
	ErrHasSales        = errors.New("ticket type already has sales")
	ErrSoldOut         = errors.New("sold out")
	ErrNotOnSale       = errors.New("not on sale")
	ErrTooMany         = errors.New("quantity exceeds per-order limit")
)
