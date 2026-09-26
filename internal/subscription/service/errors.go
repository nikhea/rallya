package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrSubscriptionNotFound = errors.New("subscription not found")
	ErrForbidden            = errors.New("insufficient permission")
	ErrInvalidPlan          = errors.New("plan must be PRO or SCALE")
	ErrAlreadySubscribed    = errors.New("organization already has an active subscription")
	ErrNoSubscription       = errors.New("organization has no subscription")
	ErrBillingUnavailable   = errors.New("subscription billing unavailable")
	// ErrUpgradeRequired blocks organizer-side creates over quota or gated
	// features (wire: 402). Existing usage is grandfathered — only new
	// writes trip it.
	ErrUpgradeRequired = errors.New("plan limit reached — upgrade required")
	// ErrEventAtCapacity is the buyer-facing twin: attendee quota hit looks
	// like fullness, never billing state (wire: 409).
	ErrEventAtCapacity = errors.New("event is at full capacity")
)
