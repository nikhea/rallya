package service

import (
	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/organization/model"
)

// GroupSyncer propagates membership changes to the IAM layer (Casbin
// groupings). Declared here (consumer side) so the organization domain
// never imports IAM; IAM implements it. A nil role removes the user.
// Nil-safe: services skip sync when no syncer is wired (pre-IAM, tests).
type GroupSyncer interface {
	SyncMembership(userID, orgID uuid.UUID, role *model.MemberRole) error
	// SyncCustomGrouping grants/revokes one custom-role grouping row
	// (touches only the named row — fixed rows are never swept here).
	SyncCustomGrouping(userID, orgID uuid.UUID, role string, grant bool) error
	// SyncCustomPolicies materializes/revokes one role's p-rows.
	SyncCustomPolicies(orgID uuid.UUID, role string, perms []RolePermission, remove bool) error
}
