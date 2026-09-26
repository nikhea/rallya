// Package model holds the Kit domain persistence schema.
// kits: named kit types per event. kit_collections: one row per handout;
// voided rows free re-issue via the partial unique index (mirrors
// migrations/000021_kit.up.sql — GORM tags stay in lockstep with it).
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CollectionStatus is a kit handout state. PENDING reserves a unit;
// COLLECTED is terminally handed over; VOIDED cancels (frees re-issue).
type CollectionStatus string

const (
	CollectionStatusPending   CollectionStatus = "PENDING"
	CollectionStatusCollected CollectionStatus = "COLLECTED"
	CollectionStatusVoided    CollectionStatus = "VOIDED"
)

// Kit is one named kit type for an event (e.g. "VIP pack", "T-shirt M").
type Kit struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	EventID       uuid.UUID `gorm:"type:uuid;index;not null" json:"eventId"`
	Name          string    `gorm:"size:120;not null" json:"name"`
	Description   string    `gorm:"type:text;not null;default:''" json:"description,omitempty"`
	QuantityTotal int       `gorm:"not null;default:0" json:"quantityTotal"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (Kit) TableName() string { return "kits" }

func (k *Kit) BeforeCreate(_ *gorm.DB) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	return nil
}

// KitCollection is one handout of a kit to an attendee. CollectedBy is the
// staffer who marked COLLECTED (NULL for reservations); CollectedAt rides
// the same transition.
type KitCollection struct {
	ID             uuid.UUID        `gorm:"type:uuid;primaryKey" json:"id"`
	KitID          uuid.UUID        `gorm:"type:uuid;index;not null" json:"kitId"`
	EventID        uuid.UUID        `gorm:"type:uuid;index;not null" json:"eventId"`
	AttendeeID     uuid.UUID        `gorm:"type:uuid;index;not null" json:"attendeeId"`
	IdempotencyKey string           `gorm:"size:64;not null;default:''" json:"-"`
	Status         CollectionStatus `gorm:"size:20;not null;default:PENDING" json:"status"`
	CollectedAt    *time.Time       `json:"collectedAt,omitempty"`
	CollectedBy    *uuid.UUID       `gorm:"type:uuid" json:"collectedBy,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

func (KitCollection) TableName() string { return "kit_collections" }

func (c *KitCollection) BeforeCreate(_ *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}
