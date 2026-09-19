// Package model holds the Ticketing domain persistence schema.
// Ticket types price and gate event access. SOLD_OUT is computed
// (sold >= total), never stored. Orders mutate sold counts later via
// Reserve()/Release(); purchase/claim flows land with orders/payments.
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TicketStatus is the sale lifecycle (SOLD_OUT derived, never written).
type TicketStatus string

const (
	TicketStatusDraft  TicketStatus = "DRAFT"
	TicketStatusActive TicketStatus = "ACTIVE"
	TicketStatusPaused TicketStatus = "PAUSED"
)

// Valid reports whether s is a writable status.
func (s TicketStatus) Valid() bool {
	return s == TicketStatusDraft || s == TicketStatusActive || s == TicketStatusPaused
}

// TicketType prices one admission tier of an event.
type TicketType struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	EventID uuid.UUID `gorm:"type:uuid;index;not null" json:"eventId"`

	Name        string  `gorm:"size:255;not null" json:"name"`
	Description *string `gorm:"type:text" json:"description,omitempty"`

	// Integer minor units (cents), never floats. Display divides by 100.
	PriceCents int    `gorm:"not null;default:0" json:"priceCents"`
	Currency   string `gorm:"size:3;not null;default:USD" json:"currency"`

	QuantityTotal int `gorm:"not null" json:"quantityTotal"`
	QuantitySold  int `gorm:"not null;default:0" json:"quantitySold"`

	MaxPerOrder *int `json:"maxPerOrder,omitempty"`

	SaleStartsAt *time.Time `json:"saleStartsAt,omitempty"`
	SaleEndsAt   *time.Time `json:"saleEndsAt,omitempty"`

	Status TicketStatus `gorm:"size:20;not null;default:DRAFT" json:"status"`

	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (TicketType) TableName() string { return "ticket_types" }

func (t *TicketType) BeforeCreate(_ *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

// SoldOut derives sell-out (never persisted).
func (t *TicketType) SoldOut() bool { return t.QuantitySold >= t.QuantityTotal }

// Remaining unsold units.
func (t *TicketType) Remaining() int {
	if r := t.QuantityTotal - t.QuantitySold; r > 0 {
		return r
	}
	return 0
}
