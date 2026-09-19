package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MFAFactorType lists supported second factors (future ready).
type MFAFactorType string

const (
	MFATOTP    MFAFactorType = "TOTP"
	MFASMS     MFAFactorType = "SMS"
	MFAPasskey MFAFactorType = "PASSKEY"
)

// MFAFactor stores one enrolled second factor per row.
// Secret must be encrypted at rest (e.g. KMS / pgcrypto).
type MFAFactor struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;index;not null" json:"userId"`

	Type MFAFactorType `gorm:"size:50" json:"type"`

	Secret *string `gorm:"type:text" json:"-"`

	Verified bool `gorm:"default:false" json:"verified"`

	CreatedAt time.Time `json:"createdAt"`
}

func (MFAFactor) TableName() string { return "mfa_factors" }

func (m *MFAFactor) BeforeCreate(_ *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// LoginAttempt is append-only security telemetry for
// brute-force detection, rate limiting, and analytics.
type LoginAttempt struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	Email  *string    `gorm:"size:255;index" json:"email,omitempty"`
	UserID *uuid.UUID `gorm:"type:uuid;index" json:"userId,omitempty"`

	IPAddress *string `gorm:"type:inet" json:"ipAddress,omitempty"`

	Success       bool    `json:"success"`
	FailureReason *string `gorm:"size:255" json:"failureReason,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

func (LoginAttempt) TableName() string { return "login_attempts" }

func (l *LoginAttempt) BeforeCreate(_ *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}
