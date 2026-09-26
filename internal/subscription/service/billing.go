package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"

	"github.com/nikhea/rallya/cmd/config"
	"github.com/nikhea/rallya/internal/subscription/repository"
)

// StripeBilling implements BillingProvider over a stripe.Client instance
// (never the deprecated global key pattern).
type StripeBilling struct {
	client *stripe.Client
	repo   *repository.SubscriptionRepository
}

// NewStripeBilling builds the provider. Errors when unconfigured so main
// can degrade (billing endpoints 503) instead of crashing boot.
func NewStripeBilling(repo *repository.SubscriptionRepository) (*StripeBilling, error) {
	key := strings.TrimSpace(config.StripeConfigFromEnv().SecretKey)
	if key == "" {
		return nil, fmt.Errorf("stripe unconfigured (set STRIPE_SECRET_KEY)")
	}
	return &StripeBilling{client: stripe.NewClient(key), repo: repo}, nil
}

// GetOrCreateCustomer returns the org's Stripe customer, creating one with
// the org in metadata when first billed. The ID persists on our row so
// lookups never scan Stripe.
func (s *StripeBilling) GetOrCreateCustomer(ctx context.Context, orgID uuid.UUID) (string, error) {
	if existing, err := s.repo.GetByOrg(orgID); err != nil {
		return "", err
	} else if existing != nil && existing.StripeCustomerID != "" {
		return existing.StripeCustomerID, nil
	}
	cust, err := s.client.V1Customers.Create(ctx, &stripe.CustomerCreateParams{
		Metadata: map[string]string{"org_id": orgID.String()},
	})
	if err != nil {
		return "", fmt.Errorf("stripe customer create: %w", err)
	}
	if err := s.repo.Upsert(newCustomerRow(orgID, cust.ID)); err != nil {
		return "", err
	}
	return cust.ID, nil
}

// CreateSubscriptionCheckout opens a hosted subscription-mode Checkout for
// a recurring price. Idempotency-keyed per org+plan.
func (s *StripeBilling) CreateSubscriptionCheckout(ctx context.Context, a CheckoutArgs) (string, string, error) {
	params := &stripe.CheckoutSessionCreateParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:   stripe.String(a.CustomerID),
		SuccessURL: stripe.String(a.SuccessURL),
		CancelURL:  stripe.String(a.CancelURL),
		Metadata: map[string]string{
			"org_id": a.OrgID.String(),
			"plan":   string(a.Plan),
		},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{Price: stripe.String(a.PriceID), Quantity: stripe.Int64(1)},
		},
	}
	params.SetIdempotencyKey(a.IdempotencyKey)
	sess, err := s.client.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", "", fmt.Errorf("stripe subscription checkout: %w", err)
	}
	return sess.URL, sess.ID, nil
}

// CreatePortalSession opens the Customer Portal for self-serve
// manage/cancel (downgrades land at period end via webhook).
func (s *StripeBilling) CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, error) {
	sess, err := s.client.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	})
	if err != nil {
		return "", fmt.Errorf("stripe portal session: %w", err)
	}
	return sess.URL, nil
}

// GetSubscription snapshots a Stripe subscription for webhook sync.
func (s *StripeBilling) GetSubscription(ctx context.Context, subscriptionID string) (SubInfo, error) {
	sub, err := s.client.V1Subscriptions.Retrieve(ctx, subscriptionID, nil)
	if err != nil {
		return SubInfo{}, fmt.Errorf("stripe subscription get: %w", err)
	}
	return subInfoFrom(sub), nil
}

// GetPrice resolves a recurring price's display amount.
func (s *StripeBilling) GetPrice(ctx context.Context, priceID string) (int64, string, error) {
	p, err := s.client.V1Prices.Retrieve(ctx, priceID, nil)
	if err != nil {
		return 0, "", fmt.Errorf("stripe price get: %w", err)
	}
	return p.UnitAmount, string(p.Currency), nil
}
