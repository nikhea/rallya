// Package model holds the Attendees domain persistence schema.
// One row per ticket unit, minted from CONFIRMED orders or manually by
// organizers. Rows are never deleted (door audit trail); cancellation flips
// status. Token hashes only; raw tokens live in confirmation emails and QR.
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AttendeeStatus is the door lifecycle (check-in transitions land later).
type AttendeeStatus string

const (
	AttendeeStatusRegistered AttendeeStatus = "REGISTERED"
	AttendeeStatusCheckedIn  AttendeeStatus = "CHECKED_IN"
	AttendeeStatusCancelled  AttendeeStatus = "CANCELLED"
)

// Valid reports whether s is a writable status in this slice.
func (s AttendeeStatus) Valid() bool {
	return s == AttendeeStatusRegistered || s == AttendeeStatusCancelled
}

// Attendee is one person's admission record for one ticket unit.
type Attendee struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	// OrderID NULL for organizer manual adds (walk-ins, comps, staff).
	OrderID *uuid.UUID `gorm:"type:uuid;index" json:"orderId,omitempty"`

	// Zero-based unit position within the order (redelivery-safe mint).
	UnitIndex int `gorm:"not null;default:0" json:"unitIndex"`

	EventID uuid.UUID `gorm:"type:uuid;index;not null" json:"eventId"`

	UserID *uuid.UUID `gorm:"type:uuid;index" json:"userId,omitempty"`

	Email string  `gorm:"size:255;not null" json:"email"`
	Name  *string `gorm:"size:255" json:"name,omitempty"`

	// TokenHash = SHA-256 hex of the scannable token.
	TokenHash string `gorm:"type:text;not null" json:"-"`

	Status AttendeeStatus `gorm:"size:20;not null;default:REGISTERED" json:"status"`

	CheckedInAt *time.Time `json:"checkedInAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Attendee) TableName() string { return "attendees" }

func (a *Attendee) BeforeCreate(_ *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}
