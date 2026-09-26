// Package subdto holds the Subscription domain wire shapes.
package subdto

// TierResponse is one catalog row: limits, features, and price.
type TierResponse struct {
	Plan         string   `json:"plan" example:"PRO"`
	Name         string   `json:"name" example:"Pro"`
	PriceID      string   `json:"priceId,omitempty" example:"price_123"`
	MonthlyCents int64    `json:"monthlyCents,omitempty" example:"2900"`
	Currency     string   `json:"currency,omitempty" example:"usd"`
	Limits       Limits   `json:"limits"`
	Features     []string `json:"features" example:"custom_roles,api_keys,kits"`
}

// Limits mirrors the quota matrix (-1 unlimited, 0 disabled).
type Limits struct {
	MaxEvents            int `json:"maxEvents" example:"25"`
	MaxMembers           int `json:"maxMembers" example:"25"`
	MaxAttendeesPerEvent int `json:"maxAttendeesPerEvent" example:"2000"`
	MaxKitsPerEvent      int `json:"maxKitsPerEvent" example:"10"`
}

// SubscriptionResponse is one org's billing state (no Stripe secrets).
type SubscriptionResponse struct {
	Plan              string  `json:"plan" example:"PRO"`
	Status            string  `json:"status" example:"ACTIVE"`
	CurrentPeriodEnd  *string `json:"currentPeriodEnd,omitempty" example:"2026-10-26T00:00:00Z"`
	CancelAtPeriodEnd bool    `json:"cancelAtPeriodEnd" example:"false"`
}

// CheckoutRequest starts a subscription Checkout for PRO/SCALE.
type CheckoutRequest struct {
	Plan string `json:"plan" example:"PRO"`
}

// CheckoutResponse carries the hosted redirect URL.
type CheckoutResponse struct {
	URL       string `json:"url" example:"https://checkout.stripe.com/c/pay/cs_test_123"`
	SessionID string `json:"sessionId" example:"cs_test_123"`
}

// PortalResponse carries the Customer Portal URL.
type PortalResponse struct {
	URL string `json:"url" example:"https://billing.stripe.com/p/session/test_123"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"plan limit reached — upgrade required"`
	Code  string `json:"code,omitempty" example:"UPGRADE_REQUIRED"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse
