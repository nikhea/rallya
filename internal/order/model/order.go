// Package model holds the Orders domain persistence schema.
// One ticket type per order. Free orders confirm immediately; priced
// orders wait in PENDING_PAYMENT for payments. Holds expire via sweeper.
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OrderStatus is the order lifecycle.
type OrderStatus string

const (
	OrderStatusPending        OrderStatus = "PENDING"
	OrderStatusPendingPayment OrderStatus = "PENDING_PAYMENT"
	OrderStatusConfirmed      OrderStatus = "CONFIRMED"
	OrderStatusCancelled      OrderStatus = "CANCELLED"
	OrderStatusExpired        OrderStatus = "EXPIRED"
)

// Valid reports whether s is a writable status.
func (s OrderStatus) Valid() bool {
	switch s {
	case OrderStatusPending, OrderStatusPendingPayment, OrderStatusConfirmed,
		OrderStatusCancelled, OrderStatusExpired:
		return true
	default:
		return false
	}
}

// Terminal reports end states (no further transitions).
func (s OrderStatus) Terminal() bool {
	return s == OrderStatusConfirmed || s == OrderStatusCancelled || s == OrderStatusExpired
}

// Order claims ticket inventory for a user.
type Order struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;index;not null" json:"userId"`

	EventID      uuid.UUID `gorm:"type:uuid;index;not null" json:"eventId"`
	TicketTypeID uuid.UUID `gorm:"type:uuid;index;not null" json:"ticketTypeId"`

	Quantity int `gorm:"not null" json:"quantity"`

	// Price snapshot at order time (minor units, never floats).
	PriceCents int    `gorm:"not null;default:0" json:"priceCents"`
	Currency   string `gorm:"size:3;not null;default:USD" json:"currency"`

	Status OrderStatus `gorm:"size:20;not null;default:PENDING" json:"status"`

	// Stripe linkage (checkout sessions + charges). Session IDs reconcile
	// webhooks; NULL until a checkout is created.
	StripeSessionID       *string    `gorm:"uniqueIndex" json:"-"`
	StripePaymentIntentID *string    `json:"-"`
	PaidAt                *time.Time `json:"paidAt,omitempty"`

	IdempotencyKey *string `gorm:"size:100" json:"-"`

	ExpiresAt *time.Time `json:"expiresAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Order) TableName() string { return "orders" }

func (o *Order) BeforeCreate(_ *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}
