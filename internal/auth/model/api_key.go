package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ApiKey is a per-organization secret for server-to-server SDK use.
// Only the SHA-256 hash of the raw secret is stored; the raw value is
// shown once at creation and never hits the database.
// Keys act as their creator for membership/Casbin checks (fail-closed
// when the creator loses membership) and are scope-intersected in the
// IAM middleware against the snapshot in Scopes.
type ApiKey struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	OrgID uuid.UUID `gorm:"type:uuid;index;not null" json:"orgId"`

	CreatedBy uuid.UUID `gorm:"type:uuid;index;not null" json:"createdBy"`

	Name string `gorm:"size:100;not null" json:"name"`

	// Prefix is the display fragment (e.g. rk_live_ab12cd34) so owners
	// can identify a key without ever seeing the secret again.
	Prefix string `gorm:"size:32;not null" json:"prefix"`

	// KeyHash = SHA-256 hex of the full raw secret.
	KeyHash string `gorm:"type:text;not null;uniqueIndex" json:"-"`

	// ScopesJSON holds the snapshotted "object:action" grants as a JSON
	// array (e.g. ["event:read","checkin:create"]). Empty/null means
	// inherit the creator's live role (no extra restriction).
	ScopesJSON string `gorm:"type:text;not null;default:'[]'" json:"-"`

	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`

	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `gorm:"index" json:"revokedAt,omitempty"`
}

func (ApiKey) TableName() string { return "api_keys" }

func (k *ApiKey) BeforeCreate(_ *gorm.DB) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	return nil
}

// Scopes decodes the snapshot into "object:action" entries.
func (k *ApiKey) Scopes() []string {
	if k.ScopesJSON == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(k.ScopesJSON), &out); err != nil {
		return nil
	}
	return out
}

// SetScopes encodes the snapshot.
func (k *ApiKey) SetScopes(scopes []string) error {
	if scopes == nil {
		scopes = []string{}
	}
	raw, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	k.ScopesJSON = string(raw)
	return nil
}
