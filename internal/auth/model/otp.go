package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OTP constants shared by repo/service.
const (
	// OTPCodeLength is the zero-padded digit count of verification codes.
	OTPCodeLength = 6
	// OTPMaxAttempts caps guesses per code before it is invalidated.
	OTPMaxAttempts = 5
)

// EmailOTPCode is a short-lived numeric verification code.
// The raw 6-digit code is emailed (and passed in job args); only its
// SHA-256 hash is stored here. Complements the 24h link tokens in
// email_verifications — either path verifies the address.
type EmailOTPCode struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;index;not null" json:"userId"`

	// CodeHash = hex(sha256("%06d", code)).
	CodeHash string `gorm:"type:text;not null" json:"-"`

	ExpiresAt time.Time `gorm:"not null" json:"expiresAt"`

	Attempts int `gorm:"not null;default:0" json:"-"`

	ConsumedAt *time.Time `json:"consumedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

func (EmailOTPCode) TableName() string { return "email_otp_codes" }

func (e *EmailOTPCode) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
