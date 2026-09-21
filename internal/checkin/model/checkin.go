// Package model holds the Check-in domain persistence schema.
// Append-only scan log; the attendee row flip itself lives in the
// attendees domain (applied atomically through its seam).
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Outcome is a scan result. Refusals are data, not errors: every attempt
// logs and returns 200 with its outcome (a refused scan is a successful
// request with a negative result).
type Outcome string

const (
	OutcomeCheckedIn        Outcome = "CHECKED_IN"
	OutcomeAlreadyCheckedIn Outcome = "ALREADY_CHECKED_IN"
	OutcomeInvalidCode      Outcome = "INVALID_CODE"
	OutcomeCancelled        Outcome = "CANCELLED"
	OutcomeWrongEvent       Outcome = "WRONG_EVENT"
	OutcomeReverted         Outcome = "REVERTED"
)

// Method records how the person was identified at the door: cryptographic
// proof (qr) or staff judgment against the roster (manual).
type Method string

const (
	MethodQR     Method = "qr"
	MethodManual Method = "manual"
)

// CheckinLog is one door attempt. AttendeeID is NULL for unscannable or
// unknown codes (nothing to attribute the attempt to).
type CheckinLog struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	EventID    uuid.UUID  `gorm:"type:uuid;index;not null" json:"eventId"`
	AttendeeID *uuid.UUID `gorm:"type:uuid;index" json:"attendeeId,omitempty"`
	ScannedBy  uuid.UUID  `gorm:"type:uuid;not null" json:"scannedBy"`
	Outcome    Outcome    `gorm:"size:20;not null" json:"outcome"`
	Method     Method     `gorm:"size:10;not null;default:qr" json:"method"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func (CheckinLog) TableName() string { return "checkin_logs" }

func (c *CheckinLog) BeforeCreate(_ *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}
