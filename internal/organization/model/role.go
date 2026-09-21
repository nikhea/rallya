package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RoleDefinition is a per-org custom role: a named set of (object, action)
// grants, unioned with the member's fixed role at enforcement. Source of
// truth materialized into casbin_rule (p-rows per permission, grouping
// rows per assignee). Names stored lowercase; fixed names are banned.
type RoleDefinition struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	OrganizationID uuid.UUID `gorm:"column:org_id;type:uuid;index;not null;uniqueIndex:idx_role_defs_org_name" json:"organizationId"`

	Name string `gorm:"size:64;not null;uniqueIndex:idx_role_defs_org_name" json:"name"`

	// Permissions JSON-encodes []RolePermission (validated at the service).
	Permissions string `gorm:"type:jsonb;not null;default:'[]'" json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (RoleDefinition) TableName() string { return "role_definitions" }

func (r *RoleDefinition) BeforeCreate(_ *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}

// MemberCustomRole assigns one custom role to one membership. No FK to
// role_definitions by design: definitions delete only when unassigned,
// and org delete cascades here directly.
type MemberCustomRole struct {
	OrganizationID uuid.UUID `gorm:"column:org_id;type:uuid;not null;primaryKey" json:"organizationId"`
	UserID         uuid.UUID `gorm:"type:uuid;not null;primaryKey" json:"userId"`
	RoleName       string    `gorm:"column:role_name;size:64;not null;primaryKey" json:"roleName"`

	CreatedAt time.Time `json:"createdAt"`
}

func (MemberCustomRole) TableName() string { return "member_custom_roles" }
