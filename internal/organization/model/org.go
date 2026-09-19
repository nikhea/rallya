// Package model holds the Organization domain persistence schema.
//
// Tables: organizations, organization_memberships, organization_invites.
// Auth users are referenced by ID only and resolved via auth.UserReader;
// permissions live in the iam domain (Casbin groupings synced from here).
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MemberRole is the fixed membership role ladder. OWNER > ADMIN > MEMBER.
// Fine-grained permissions are IAM's job; roles are the ceiling here.
type MemberRole string

const (
	MemberRoleOwner  MemberRole = "OWNER"
	MemberRoleAdmin  MemberRole = "ADMIN"
	MemberRoleMember MemberRole = "MEMBER"
)

// Level orders roles for grant/guard comparisons.
func (r MemberRole) Level() int {
	switch r {
	case MemberRoleOwner:
		return 3
	case MemberRoleAdmin:
		return 2
	default:
		return 1
	}
}

// Valid reports whether r is a known role.
func (r MemberRole) Valid() bool {
	return r == MemberRoleOwner || r == MemberRoleAdmin || r == MemberRoleMember
}

// Organization is a tenant root.
type Organization struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	Name string `gorm:"size:255;not null" json:"name"`
	Slug string `gorm:"size:100;uniqueIndex;not null" json:"slug"`

	LogoURL *string `gorm:"type:text" json:"logoUrl,omitempty"`

	CreatedBy *uuid.UUID `gorm:"type:uuid" json:"createdBy,omitempty"`

	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Memberships []Membership `gorm:"foreignKey:OrganizationID" json:"-"`
	Invites     []Invite     `gorm:"foreignKey:OrganizationID" json:"-"`
}

func (Organization) TableName() string { return "organizations" }

func (o *Organization) BeforeCreate(_ *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}

// Membership links a user to an org at a fixed role.
type Membership struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	OrganizationID uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:idx_memberships_org_user" json:"organizationId"`

	UserID uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:idx_memberships_org_user" json:"userId"`

	Role MemberRole `gorm:"size:20;not null;default:MEMBER" json:"role"`

	// NotifyEvents opts into publish announcements (default true).
	NotifyEvents bool `gorm:"not null;default:true" json:"notifyEvents"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Membership) TableName() string { return "organization_memberships" }

func (m *Membership) BeforeCreate(_ *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// Invite is a pending email invitation. Raw tokens only ever appear in
// the email body / job args; only the hash is stored.
type Invite struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	OrganizationID uuid.UUID `gorm:"type:uuid;index;not null" json:"organizationId"`

	Email string     `gorm:"size:255;not null" json:"email"`
	Role  MemberRole `gorm:"size:20;not null;default:MEMBER" json:"role"`

	TokenHash string `gorm:"type:text;not null" json:"-"`

	ExpiresAt time.Time `gorm:"not null" json:"expiresAt"`

	AcceptedAt *time.Time `json:"acceptedAt,omitempty"`
	DeclinedAt *time.Time `json:"declinedAt,omitempty"`

	InvitedBy *uuid.UUID `gorm:"type:uuid" json:"invitedBy,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

func (Invite) TableName() string { return "organization_invites" }

func (i *Invite) BeforeCreate(_ *gorm.DB) error {
	if i.ID == uuid.Nil {
		i.ID = uuid.New()
	}
	return nil
}

// Pending reports whether the invite can still be acted on.
func (i *Invite) Pending(now time.Time) bool {
	return i.AcceptedAt == nil && i.DeclinedAt == nil && now.Before(i.ExpiresAt)
}
