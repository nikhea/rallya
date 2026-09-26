package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/nikhea/rallya/cmd/config"
	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
	"github.com/nikhea/rallya/internal/subscription/repository"
)

// PastDueGraceDays keeps the paid tier after a failed renewal before the
// org drops to Free (grandfathered usage stays regardless).
const PastDueGraceDays = 7

// OwnerChecker gates billing mutations to org owners (implemented by org).
type OwnerChecker interface {
	IsOwner(userID, orgID uuid.UUID) (bool, error)
}

// CheckoutArgs carries a subscription-checkout request to the provider.
type CheckoutArgs struct {
	CustomerID     string
	PriceID        string
	OrgID          uuid.UUID
	Plan           submodel.Plan
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string
}

// SubInfo is a provider-side subscription snapshot (webhook sync).
type SubInfo struct {
	Status            string
	CurrentPeriodEnd  *time.Time
	CancelAtPeriodEnd bool
	Plan              submodel.Plan
	CustomerID        string
	SubscriptionID    string
}

// BillingProvider creates Stripe Billing objects. StripeBilling is the
// production implementation; fakes cover tests. Nil provider means billing
// is unconfigured: entitlements still resolve (Free default), money
// movement fails with ErrBillingUnavailable (503, checkout precedent).
type BillingProvider interface {
	GetOrCreateCustomer(ctx context.Context, orgID uuid.UUID) (string, error)
	CreateSubscriptionCheckout(ctx context.Context, a CheckoutArgs) (url, sessionID string, err error)
	CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, error)
	GetSubscription(ctx context.Context, subscriptionID string) (SubInfo, error)
	GetPrice(ctx context.Context, priceID string) (unitAmount int64, currency string, err error)
}

// SubscriptionService resolves entitlements and drives Stripe Billing.
type SubscriptionService struct {
	repo    *repository.SubscriptionRepository
	owners  OwnerChecker
	billing BillingProvider
	auditor auditsvc.Emitter
}

// NewSubscriptionService builds the service. OwnerChecker arrives from org;
// BillingProvider via SetBillingProvider (nil = unconfigured).
func NewSubscriptionService(repo *repository.SubscriptionRepository, owners OwnerChecker) *SubscriptionService {
	return &SubscriptionService{repo: repo, owners: owners}
}

// SetBillingProvider wires Stripe Billing (nil-safe when unconfigured).
func (s *SubscriptionService) SetBillingProvider(b BillingProvider) { s.billing = b }

// SetAuditEmitter wires audit-trail emission (nil-safe when absent).
func (s *SubscriptionService) SetAuditEmitter(a auditsvc.Emitter) { s.auditor = a }

// EntitlementFor resolves one org's effective tier. Absent rows, canceled
// plans, unknown plans, and lapsed grace all resolve Free (fail-closed:
// restrictive, never open). Nil repo errors propagate.
func (s *SubscriptionService) EntitlementFor(orgID uuid.UUID) (submodel.Entitlement, error) {
	sub, err := s.repo.GetByOrg(orgID)
	if err != nil {
		return submodel.FreeEntitlement(), err
	}
	if sub == nil || !sub.Plan.Valid() {
		return submodel.FreeEntitlement(), nil
	}
	switch sub.Status {
	case submodel.StatusActive:
		return submodel.EntitlementForPlan(sub.Plan), nil
	case submodel.StatusPastDue:
		if sub.GraceUntil == nil || time.Now().Before(*sub.GraceUntil) {
			return submodel.EntitlementForPlan(sub.Plan), nil
		}
		return submodel.FreeEntitlement(), nil
	default:
		return submodel.FreeEntitlement(), nil
	}
}

// TierView is one catalog row (amounts resolve from Stripe when configured).
type TierView struct {
	Plan         submodel.Plan
	Name         string
	PriceID      string
	MonthlyCents int64
	Currency     string
	Limits       submodel.Limits
	Features     []string
}

// Catalog returns the three tiers with limits, features, and amounts.
func (s *SubscriptionService) Catalog(ctx context.Context) ([]TierView, error) {
	cfg := config.StripeConfigFromEnv()
	priceIDs := map[submodel.Plan]string{submodel.PlanPro: cfg.PricePro, submodel.PlanScale: cfg.PriceScale}
	names := map[submodel.Plan]string{submodel.PlanFree: "Free", submodel.PlanPro: "Pro", submodel.PlanScale: "Scale"}
	out := make([]TierView, 0, 3)
	for _, p := range []submodel.Plan{submodel.PlanFree, submodel.PlanPro, submodel.PlanScale} {
		v := TierView{Plan: p, Name: names[p], PriceID: priceIDs[p], Limits: submodel.TierLimits[p], Features: []string{}}
		for _, f := range []string{submodel.FeatureCustomRoles, submodel.FeatureAPIKeys, submodel.FeatureKits} {
			if submodel.TierFeatures[p][f] {
				v.Features = append(v.Features, f)
			}
		}
		if p.Paid() && s.billing != nil && v.PriceID != "" {
			if amount, currency, err := s.billing.GetPrice(ctx, v.PriceID); err == nil {
				v.MonthlyCents = amount
				v.Currency = currency
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// GetSubscription returns one org's billing row (nil when never subscribed).
func (s *SubscriptionService) GetSubscription(orgID uuid.UUID) (*submodel.OrgSubscription, error) {
	return s.repo.GetByOrg(orgID)
}

// StartCheckout opens a Stripe subscription-mode Checkout for PRO/SCALE.
// OWNER-only. Idempotency-keyed per org+plan; fulfillment lands via
// webhook (no row is written here — paid state arrives as events).
func (s *SubscriptionService) StartCheckout(ctx context.Context, userID, orgID uuid.UUID, plan submodel.Plan, successURL, cancelURL string) (url, sessionID string, err error) {
	if !plan.Paid() {
		return "", "", ErrInvalidPlan
	}
	if s.billing == nil {
		return "", "", ErrBillingUnavailable
	}
	ok, err := s.owners.IsOwner(userID, orgID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", ErrForbidden
	}
	if existing, err := s.repo.GetByOrg(orgID); err != nil {
		return "", "", err
	} else if existing != nil && existing.Status == submodel.StatusActive &&
		existing.Plan == plan && existing.StripeSubscriptionID != "" {
		return "", "", ErrAlreadySubscribed
	}
	cfg := config.StripeConfigFromEnv()
	priceID := cfg.PricePro
	if plan == submodel.PlanScale {
		priceID = cfg.PriceScale
	}
	if priceID == "" {
		return "", "", ErrBillingUnavailable
	}
	customerID, err := s.billing.GetOrCreateCustomer(ctx, orgID)
	if err != nil {
		return "", "", err
	}
	url, sessionID, err = s.billing.CreateSubscriptionCheckout(ctx, CheckoutArgs{
		CustomerID: customerID, PriceID: priceID, OrgID: orgID, Plan: plan,
		SuccessURL: successURL, CancelURL: cancelURL,
		IdempotencyKey: "sub-" + orgID.String() + "-" + string(plan),
	})
	if err != nil {
		return "", "", err
	}
	if s.auditor != nil {
		_ = s.auditor.EmitTx(nil, auditsvc.Entry{
			OrgID: &orgID, ActorID: &userID,
			Action: "subscription.checkout_started", ObjectType: auditmodel.ObjectSubscription,
			After: map[string]any{"plan": string(plan)},
		})
	}
	return url, sessionID, nil
}

// CreatePortalSession opens the Stripe Customer Portal for self-serve
// manage/cancel (downgrades land at period end via webhook). OWNER-only.
func (s *SubscriptionService) CreatePortalSession(ctx context.Context, userID, orgID uuid.UUID, returnURL string) (string, error) {
	if s.billing == nil {
		return "", ErrBillingUnavailable
	}
	ok, err := s.owners.IsOwner(userID, orgID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrForbidden
	}
	sub, err := s.repo.GetByOrg(orgID)
	if err != nil {
		return "", err
	}
	if sub == nil || sub.StripeCustomerID == "" {
		return "", ErrNoSubscription
	}
	return s.billing.CreatePortalSession(ctx, sub.StripeCustomerID, returnURL)
}
