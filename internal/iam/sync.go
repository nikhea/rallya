package iam

import (
	"github.com/casbin/casbin/v3"
	"github.com/google/uuid"

	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
)

// MembershipSyncer implements the organization service's GroupSyncer:
// membership mutations propagate to Casbin groupings (user, role, org).
type MembershipSyncer struct {
	E *casbin.Enforcer
}

// NewMembershipSyncer wraps the enforcer for org wiring.
func NewMembershipSyncer(e *casbin.Enforcer) *MembershipSyncer {
	return &MembershipSyncer{E: e}
}

// SyncMembership adds (role != nil) or removes (nil) the grouping.
// Role changes replace: the user's existing FIXED-role rows for the org
// are swept first so stale roles never linger. Custom-role rows are never
// touched here (they have their own lifecycle below). Idempotent;
// 2-field superadmin rows are never touched.
func (s *MembershipSyncer) SyncMembership(userID, orgID uuid.UUID, role *orgmodel.MemberRole) error {
	user, dom := userID.String(), orgID.String()
	return LockWrite(func() error {
		rows, err := s.E.GetFilteredGroupingPolicy(0, user)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if len(row) == 3 && row[2] == dom && isFixedRole(row[1]) {
				if _, err := s.E.RemoveGroupingPolicy(row); err != nil {
					return err
				}
			}
		}
		if role == nil {
			return nil
		}
		_, err = s.E.AddGroupingPolicy(user, string(*role), dom)
		return err
	})
}

// SyncCustomGrouping grants (grant) or revokes one custom-role grouping
// row. Touches only the named row — fixed rows and other customs are left
// alone. Idempotent (Casbin reports duplicates/misses without error).
func (s *MembershipSyncer) SyncCustomGrouping(userID, orgID uuid.UUID, role string, grant bool) error {
	user, dom := userID.String(), orgID.String()
	return LockWrite(func() error {
		var err error
		if grant {
			_, err = s.E.AddGroupingPolicy(user, role, dom)
		} else {
			_, err = s.E.RemoveGroupingPolicy(user, role, dom)
		}
		return err
	})
}

// isFixedRole reports the three built-in role values (custom names can
// never collide — reserved at definition time).
func isFixedRole(role string) bool {
	switch orgmodel.MemberRole(role) {
	case orgmodel.MemberRoleOwner, orgmodel.MemberRoleAdmin, orgmodel.MemberRoleMember:
		return true
	default:
		return false
	}
}

var _ orgservice.GroupSyncer = (*MembershipSyncer)(nil)

// SyncCustomPolicies materializes (remove=false) or revokes one custom
// role's p-rows. Implements the org domain's GroupSyncer seam.
func (s *MembershipSyncer) SyncCustomPolicies(orgID uuid.UUID, role string, perms []orgservice.RolePermission, remove bool) error {
	mapped := make([]Permission, 0, len(perms))
	for _, p := range perms {
		mapped = append(mapped, Permission{Obj: p.Object, Act: p.Action})
	}
	if remove {
		_, err := RemoveCustomRolePolicies(s.E, orgID, role)
		return err
	}
	_, err := EnsureCustomRolePolicies(s.E, orgID, role, mapped)
	return err
}
