// Package model holds the Subscription domain persistence schema and the
// entitlement vocabulary shared with enforcing domains (they consume the
// types only — resolution stays in service).
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Plan is an organization subscription tier.
type Plan string

const (
	PlanFree  Plan = "FREE"
	PlanPro   Plan = "PRO"
	PlanScale Plan = "SCALE"
)

// Valid reports whether p is a known plan.
func (p Plan) Valid() bool {
	switch p {
	case PlanFree, PlanPro, PlanScale:
		return true
	default:
		return false
	}
}

// Paid reports whether p bills through Stripe.
func (p Plan) Paid() bool { return p == PlanPro || p == PlanScale }

// SubStatus is the billing state of an org subscription.
type SubStatus string

const (
	StatusActive   SubStatus = "ACTIVE"
	StatusPastDue  SubStatus = "PAST_DUE"
	StatusCanceled SubStatus = "CANCELED"
)

// Feature flags gated per tier.
const (
	FeatureCustomRoles = "custom_roles"
	FeatureAPIKeys     = "api_keys"
	FeatureKits        = "kits"
)

// Limits are per-tier quotas. -1 means unlimited, 0 disables the feature.
type Limits struct {
	MaxEvents            int
	MaxMembers           int
	MaxAttendeesPerEvent int
	MaxKitsPerEvent      int
}

// TierLimits is the static quota matrix (amounts resolve from Stripe).
var TierLimits = map[Plan]Limits{
	PlanFree:  {MaxEvents: 3, MaxMembers: 5, MaxAttendeesPerEvent: 200, MaxKitsPerEvent: 0},
	PlanPro:   {MaxEvents: 25, MaxMembers: 25, MaxAttendeesPerEvent: 2000, MaxKitsPerEvent: 10},
	PlanScale: {MaxEvents: 200, MaxMembers: 200, MaxAttendeesPerEvent: 20000, MaxKitsPerEvent: -1},
}

// TierFeatures is the static feature matrix.
var TierFeatures = map[Plan]map[string]bool{
	PlanFree:  {},
	PlanPro:   {FeatureCustomRoles: true, FeatureAPIKeys: true, FeatureKits: true},
	PlanScale: {FeatureCustomRoles: true, FeatureAPIKeys: true, FeatureKits: true},
}

// Entitlement is the resolved effective tier for an org: what enforcing
// domains check (quotas + flags). Absent subscription rows resolve Free.
type Entitlement struct {
	Plan     Plan
	Limits   Limits
	Features map[string]bool
}

// Can reports whether a feature flag is on.
func (e Entitlement) Can(feature string) bool { return e.Features[feature] }

// FreeEntitlement is the default (fail-closed: restrictive, never open).
func FreeEntitlement() Entitlement {
	return Entitlement{Plan: PlanFree, Limits: TierLimits[PlanFree], Features: map[string]bool{}}
}

// EntitlementForPlan builds the entitlement for a known plan.
func EntitlementForPlan(p Plan) Entitlement {
	if !p.Valid() {
		return FreeEntitlement()
	}
	features := map[string]bool{}
	for f := range TierFeatures[p] {
		features[f] = true
	}
	return Entitlement{Plan: p, Limits: TierLimits[p], Features: features}
}

// OrgSubscription is one org's billing state. A missing row means FREE.
type OrgSubscription struct {
	ID                   uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	OrgID                uuid.UUID  `gorm:"type:uuid;uniqueIndex;not null" json:"orgId"`
	Plan                 Plan       `gorm:"size:10;not null;default:FREE" json:"plan"`
	Status               SubStatus  `gorm:"size:10;not null;default:ACTIVE" json:"status"`
	StripeCustomerID     string     `gorm:"size:64;not null;default:''" json:"-"`
	StripeSubscriptionID string     `gorm:"size:64;not null;default:''" json:"-"`
	CurrentPeriodEnd     *time.Time `json:"currentPeriodEnd,omitempty"`
	CancelAtPeriodEnd    bool       `gorm:"not null;default:false" json:"cancelAtPeriodEnd"`
	GraceUntil           *time.Time `json:"graceUntil,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

func (OrgSubscription) TableName() string { return "org_subscriptions" }

func (s *OrgSubscription) BeforeCreate(_ *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
