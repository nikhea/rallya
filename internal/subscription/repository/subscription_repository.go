// Package repository owns org_subscriptions persistence.
package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/subscription/model"
)

// SubscriptionRepository persists org billing state.
type SubscriptionRepository struct {
	db *gorm.DB
}

// NewSubscriptionRepository builds the repository on the shared handle.
func NewSubscriptionRepository(db *gorm.DB) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

// GetByOrg loads one org's subscription. Nil, nil when the org never
// subscribed (absent row = FREE).
func (r *SubscriptionRepository) GetByOrg(orgID uuid.UUID) (*model.OrgSubscription, error) {
	var s model.OrgSubscription
	if err := r.db.Where("org_id = ?", orgID).First(&s).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// GetBySubscriptionID loads the row for a Stripe subscription (webhooks).
// Nil, nil when unknown (ack, never fail the webhook).
func (r *SubscriptionRepository) GetBySubscriptionID(subID string) (*model.OrgSubscription, error) {
	var s model.OrgSubscription
	if err := r.db.Where("stripe_subscription_id = ?", subID).First(&s).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// GetByCustomerID loads the row for a Stripe customer (portal fallback).
// Nil, nil when unknown.
func (r *SubscriptionRepository) GetByCustomerID(customerID string) (*model.OrgSubscription, error) {
	var s model.OrgSubscription
	if err := r.db.Where("stripe_customer_id = ?", customerID).First(&s).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// Upsert creates or replaces one org's row by org_id.
func (r *SubscriptionRepository) Upsert(s *model.OrgSubscription) error {
	existing, err := r.GetByOrg(s.OrgID)
	if err != nil {
		return err
	}
	if existing == nil {
		return r.db.Create(s).Error
	}
	s.ID = existing.ID
	return r.db.Save(s).Error
}

// Save persists subscription edits.
func (r *SubscriptionRepository) Save(s *model.OrgSubscription) error {
	return r.db.Save(s).Error
}
