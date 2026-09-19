package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"

	"github.com/nikhea/rallya/cmd/config"
)

// CheckoutParams carries a validated checkout request to the provider.
type CheckoutParams struct {
	OrderID  uuid.UUID
	UserID   uuid.UUID
	Name     string // line-item display: ticket + event
	Amount   int64  // minor units
	Currency string // ISO code; lowercased for Stripe
	Quantity int64
	// SuccessURL/CancelURL land the buyer back in-app after payment.
	// Fulfillment NEVER runs there — only the webhook fulfills.
	SuccessURL string
	CancelURL  string
}

// CheckoutSession is the created session handle.
type CheckoutSession struct {
	ID  string
	URL string
}

// CheckoutProvider creates hosted checkout sessions. StripeCheckout is the
// production implementation; fakes cover tests.
type CheckoutProvider interface {
	CreateSession(ctx context.Context, p CheckoutParams) (*CheckoutSession, error)
}

// StripeCheckout implements CheckoutProvider over a StripeClient instance
// (never the deprecated global key pattern).
type StripeCheckout struct {
	client *stripe.Client
}

// NewStripeCheckout builds the provider. Errors when unconfigured so main
// can degrade (checkout endpoint 503s) instead of crashing boot.
func NewStripeCheckout() (*StripeCheckout, error) {
	key := strings.TrimSpace(config.StripeConfigFromEnv().SecretKey)
	if key == "" {
		return nil, fmt.Errorf("stripe unconfigured (set STRIPE_SECRET_KEY)")
	}
	return &StripeCheckout{client: stripe.NewClient(key)}, nil
}

// CreateSession creates a hosted Checkout Session in payment mode with
// dynamic payment methods (no payment_method_types — Stripe chooses).
// Idempotency-keyed by order so retries never double-create sessions.
func (s *StripeCheckout) CreateSession(ctx context.Context, p CheckoutParams) (*CheckoutSession, error) {
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return nil, err
	}
	params := &stripe.CheckoutSessionCreateParams{
		Mode:                  stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:            stripe.String(p.SuccessURL),
		CancelURL:             stripe.String(p.CancelURL),
		IntegrationIdentifier: stripe.String("rallya-checkout-" + hex.EncodeToString(suffix)),
		Metadata: map[string]string{
			"order_id": p.OrderID.String(),
			"user_id":  p.UserID.String(),
		},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
					Currency: stripe.String(strings.ToLower(p.Currency)),
					ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
						Name: stripe.String(p.Name),
					},
					UnitAmount: stripe.Int64(p.Amount),
				},
				Quantity: stripe.Int64(p.Quantity),
			},
		},
	}
	params.SetIdempotencyKey("order-" + p.OrderID.String())
	sess, err := s.client.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe checkout create: %w", err)
	}
	return &CheckoutSession{ID: sess.ID, URL: sess.URL}, nil
}
