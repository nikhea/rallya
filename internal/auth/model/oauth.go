package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OAuthProvider identifies an external identity provider.
type OAuthProvider string

const (
	OAuthGoogle    OAuthProvider = "GOOGLE"
	OAuthGitHub    OAuthProvider = "GITHUB"
	OAuthApple     OAuthProvider = "APPLE"
	OAuthMicrosoft OAuthProvider = "MICROSOFT"
)

// OAuthAccount links a user to Google/GitHub/etc. logins.
// UNIQUE(provider, provider_account_id) prevents double-linking.
type OAuthAccount struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;index;not null" json:"userId"`

	Provider          OAuthProvider `gorm:"size:50;not null;uniqueIndex:idx_oauth_provider_account" json:"provider"`
	ProviderAccountID string        `gorm:"size:255;not null;uniqueIndex:idx_oauth_provider_account" json:"providerAccountId"`

	// Provider tokens are stored encrypted at rest in production.
	// Kept nullable so password-only users have no rows here.
	AccessToken  *string    `gorm:"type:text" json:"-"`
	RefreshToken *string    `gorm:"type:text" json:"-"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`

	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

func (OAuthAccount) TableName() string { return "oauth_accounts" }

func (o *OAuthAccount) BeforeCreate(_ *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}
