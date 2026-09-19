package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserStatus is the lifecycle state of a user identity.
type UserStatus string

const (
	UserStatusActive    UserStatus = "ACTIVE"
	UserStatusInactive  UserStatus = "INACTIVE"
	UserStatusSuspended UserStatus = "SUSPENDED"
	UserStatusDeleted   UserStatus = "DELETED"
)

// User is the core identity record. Auth boundary ends at User:
// org/iam/event domains must consume it via UserReader, never by
// joining or importing auth internals directly.
type User struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	Email         string `gorm:"size:255;uniqueIndex;not null" json:"email"`
	EmailVerified bool   `gorm:"default:false" json:"emailVerified"`

	Phone         *string `gorm:"size:30" json:"phone,omitempty"`
	PhoneVerified bool    `gorm:"default:false" json:"phoneVerified"`

	Status UserStatus `gorm:"size:50;default:ACTIVE" json:"status"`

	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`

	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations (auth scope only).
	Profile        *UserProfile        `gorm:"foreignKey:UserID" json:"profile,omitempty"`
	Credential     *Credential         `gorm:"foreignKey:UserID" json:"-"`
	OAuthAccounts  []OAuthAccount      `gorm:"foreignKey:UserID" json:"-"`
	Sessions       []Session           `gorm:"foreignKey:UserID" json:"-"`
	EmailVerifs    []EmailVerification `gorm:"foreignKey:UserID" json:"-"`
	PasswordResets []PasswordReset     `gorm:"foreignKey:UserID" json:"-"`
	MFAFactors     []MFAFactor         `gorm:"foreignKey:UserID" json:"-"`
}

// TableName pins the table to auth.users spec (no schema prefix so
// search_path / single-schema MVP keeps working).
func (User) TableName() string { return "users" }

// BeforeCreate ensures an ID when the DB default doesn't apply
// (e.g. SQLite in tests).
func (u *User) BeforeCreate(_ *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

// UserProfile keeps display/preference data separate from authentication.
type UserProfile struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"userId"`

	FirstName *string `gorm:"size:100" json:"firstName,omitempty"`
	LastName  *string `gorm:"size:100" json:"lastName,omitempty"`

	AvatarURL *string `gorm:"type:text" json:"avatarUrl,omitempty"`

	Timezone *string `gorm:"size:50" json:"timezone,omitempty"`
	Locale   *string `gorm:"size:10" json:"locale,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (UserProfile) TableName() string { return "user_profiles" }

func (p *UserProfile) BeforeCreate(_ *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}
