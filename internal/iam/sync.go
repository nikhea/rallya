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
// Role changes replace: the user's existing rows for the org are swept
// first so stale roles never linger. Idempotent; 2-field superadmin rows
// are never touched.
func (s *MembershipSyncer) SyncMembership(userID, orgID uuid.UUID, role *orgmodel.MemberRole) error {
	user, dom := userID.String(), orgID.String()
	return LockWrite(func() error {
		rows, err := s.E.GetFilteredGroupingPolicy(0, user)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if len(row) == 3 && row[2] == dom {
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

var _ orgservice.GroupSyncer = (*MembershipSyncer)(nil)
