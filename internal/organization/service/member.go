package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/auth/model"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgutils "github.com/nikhea/rallya/internal/organization/utils"
)

// ---------- members ----------

// MemberView joins a membership with identity display data.
type MemberView struct {
	UserID        uuid.UUID           `json:"userId"`
	Email         string              `json:"email"`
	Name          string              `json:"name"`
	EmailVerified bool                `json:"emailVerified"`
	Role          orgmodel.MemberRole `json:"role"`
	JoinedAt      time.Time           `json:"joinedAt"`
}

// ListMembers returns member views (MEMBER+).
func (s *OrgService) ListMembers(userID uuid.UUID, ref string, limit, offset int) ([]MemberView, int64, error) {
	o, _, err := s.requireRole(userID, ref, orgmodel.MemberRoleMember)
	if err != nil {
		return nil, 0, err
	}
	ms, total, err := s.repo.ListMemberships(o.ID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]MemberView, 0, len(ms))
	for _, m := range ms {
		u, err := s.users.GetUserByID(m.UserID)
		if err != nil {
			continue
		}
		out = append(out, MemberView{
			UserID: u.ID, Email: u.Email, Name: orgutils.DisplayNameOf(u),
			EmailVerified: u.EmailVerified, Role: m.Role, JoinedAt: m.CreatedAt,
		})
	}
	return out, total, nil
}

// AddMember adds a registered user directly (ADMIN+; granted role must not
// exceed the grantor's).
func (s *OrgService) AddMember(grantorID uuid.UUID, ref, email string, role orgmodel.MemberRole) (*MemberView, error) {
	o, g, err := s.requireRole(grantorID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return nil, err
	}
	if !role.Valid() {
		role = orgmodel.MemberRoleMember
	}
	if g.Role.Level() < role.Level() {
		return nil, ErrForbidden
	}
	target, err := s.users.GetUserByEmail(orgutils.NormalizeEmail(email))
	if err != nil {
		return nil, ErrUserNotFound
	}
	if target.Status != model.UserStatusActive {
		return nil, ErrUserNotFound
	}
	if _, err := s.repo.GetMembership(o.ID, target.ID); err == nil {
		return nil, ErrAlreadyMember
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	m := &orgmodel.Membership{OrganizationID: o.ID, UserID: target.ID, Role: role}
	if err := s.repo.CreateMembership(nil, m); err != nil {
		if orgutils.IsUniqueViolation(err) {
			return nil, ErrAlreadyMember
		}
		return nil, err
	}
	s.sync(target.ID, o.ID, &role)
	return &MemberView{
		UserID: target.ID, Email: target.Email, Name: orgutils.DisplayNameOf(target),
		EmailVerified: target.EmailVerified, Role: role, JoinedAt: m.CreatedAt,
	}, nil
}

// UpdateMemberRole changes a member's role (OWNER only, last-owner guard).
func (s *OrgService) UpdateMemberRole(grantorID uuid.UUID, ref string, targetID uuid.UUID, role orgmodel.MemberRole) (*MemberView, error) {
	o, _, err := s.requireRole(grantorID, ref, orgmodel.MemberRoleOwner)
	if err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, ErrForbidden
	}
	m, err := s.repo.GetMembership(o.ID, targetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrgNotFound
		}
		return nil, err
	}
	if m.Role == orgmodel.MemberRoleOwner && role != orgmodel.MemberRoleOwner {
		if err := s.guardLastOwner(o.ID); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	if err := s.repo.UpdateMemberRole(nil, o.ID, targetID, role, now); err != nil {
		return nil, err
	}
	s.sync(targetID, o.ID, &role)
	u, _ := s.users.GetUserByID(targetID)
	view := &MemberView{UserID: targetID, Role: role, JoinedAt: m.CreatedAt}
	if u != nil {
		view.Email, view.Name, view.EmailVerified = u.Email, orgutils.DisplayNameOf(u), u.EmailVerified
	}
	return view, nil
}

// RemoveMember removes a member or lets one leave. ADMIN+ may remove
// MEMBER/ADMIN; only OWNER may remove an OWNER; anyone may remove
// themselves except the last OWNER.
func (s *OrgService) RemoveMember(grantorID uuid.UUID, ref string, targetID uuid.UUID) error {
	o, g, err := s.membership(grantorID, ref)
	if err != nil {
		return err
	}
	m, err := s.repo.GetMembership(o.ID, targetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrOrgNotFound
		}
		return err
	}
	self := grantorID == targetID
	if !self {
		if g.Role.Level() < orgmodel.MemberRoleAdmin.Level() {
			return ErrForbidden
		}
		if m.Role == orgmodel.MemberRoleOwner && g.Role != orgmodel.MemberRoleOwner {
			return ErrForbidden
		}
	}
	if m.Role == orgmodel.MemberRoleOwner {
		if err := s.guardLastOwner(o.ID); err != nil {
			return err
		}
	}
	if err := s.repo.DeleteMembership(nil, o.ID, targetID); err != nil {
		return err
	}
	s.sync(targetID, o.ID, nil)
	return nil
}
