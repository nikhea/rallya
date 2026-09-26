package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrKitNotFound            = errors.New("kit not found")
	ErrCollectionNotFound     = errors.New("kit collection not found")
	ErrAttendeeNotFound       = errors.New("attendee not found")
	ErrForbidden              = errors.New("insufficient permission")
	ErrInvalidQuantity        = errors.New("quantityTotal must be at least 1")
	ErrInvalidStatus          = errors.New("collection is not pending")
	ErrNotCheckedIn           = errors.New("attendee is not checked in")
	ErrQuantityExhausted      = errors.New("kit quantity exhausted")
	ErrAlreadyCollected       = errors.New("kit already collected or reserved for attendee")
	ErrQuantityBelowCollected = errors.New("quantityTotal cannot drop below the collected count")
	ErrKitHasCollections      = errors.New("kit has active collections")
)
