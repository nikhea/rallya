package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Session tracks a logged-in device. Revocation is soft
// (RevokedAt) so audit history is preserved.
type Session struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;index;not null" json:"userId"`

	IPAddress  *string `gorm:"type:inet" json:"ipAddress,omitempty"`
	UserAgent  *string `gorm:"type:text" json:"userAgent,omitempty"`
	DeviceName *string `gorm:"size:255" json:"deviceName,omitempty"`

	LastActiveAt *time.Time `json:"lastActiveAt,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`

	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `gorm:"index" json:"revokedAt,omitempty"`

	RefreshTokens []RefreshToken `gorm:"foreignKey:SessionID" json:"-"`
}

func (Session) TableName() string { return "sessions" }

func (s *Session) BeforeCreate(_ *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// RefreshToken stores only the hash of the opaque refresh token
// for JWT rotation. Raw tokens must never hit the database.
type RefreshToken struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	SessionID uuid.UUID `gorm:"type:uuid;index;not null" json:"sessionId"`

	// TokenHash = hash(refresh_token), e.g. SHA-256 hex.
	TokenHash string `gorm:"type:text;not null" json:"-"`

	ExpiresAt time.Time  `gorm:"not null" json:"expiresAt"`
	RevokedAt *time.Time `gorm:"index" json:"revokedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

func (RefreshToken) TableName() string { return "refresh_tokens" }

func (r *RefreshToken) BeforeCreate(_ *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}
