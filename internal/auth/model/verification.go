package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EmailVerification confirms email ownership.
// Flow: register -> create token -> send email -> user clicks link -> verified.
type EmailVerification struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;index;not null" json:"userId"`

	// TokenHash = hash(verification token). Raw token only goes in email.
	TokenHash string `gorm:"type:text;not null" json:"-"`

	ExpiresAt  time.Time  `gorm:"not null" json:"expiresAt"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

func (EmailVerification) TableName() string { return "email_verifications" }

func (e *EmailVerification) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

// PasswordReset implements the forgot-password flow.
// On successful reset, all sessions for the user must be invalidated.
type PasswordReset struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;index;not null" json:"userId"`

	// TokenHash = hash(reset token). Raw token only goes in email.
	TokenHash string `gorm:"type:text;not null" json:"-"`

	ExpiresAt time.Time  `gorm:"not null" json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

func (PasswordReset) TableName() string { return "password_resets" }

func (p *PasswordReset) BeforeCreate(_ *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}
