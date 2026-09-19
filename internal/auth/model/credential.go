package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Credential stores password authentication material.
// Never store raw passwords — only Argon2id/bcrypt hashes.
type Credential struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"userId"`

	PasswordHash string `gorm:"type:text;not null" json:"-"`

	PasswordChangedAt *time.Time `json:"passwordChangedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Credential) TableName() string { return "credentials" }

func (c *Credential) BeforeCreate(_ *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}
