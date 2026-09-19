package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/auth/model"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgutils "github.com/nikhea/rallya/internal/organization/utils"
)

// ---------- members ----------

// ListMembers returns members (MEMBER+).
func (s *OrgService) ListMembers(userID uuid.UUID, ref string, limit, offset int) ([]orgdto.Member, int64, error) {
	o, _, err := s.requireRole(userID, ref, orgmodel.MemberRoleMember)
	if err != nil {
		return nil, 0, err
	}
	ms, total, err := s.repo.ListMemberships(o.ID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]orgdto.Member, 0, len(ms))
	for _, m := range ms {
		u, err := s.users.GetUserByID(m.UserID)
		if err != nil {
			continue
		}
		out = append(out, *toMember(u, m.Role, m.CreatedAt))
	}
	return out, total, nil
}

// AddMember adds a registered user directly (ADMIN+; granted role must not
// exceed the grantor's).
func (s *OrgService) AddMember(grantorID uuid.UUID, ref, email string, role orgmodel.MemberRole) (*orgdto.Member, error) {
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
	m.Role = role
	return toMember(target, m.Role, m.CreatedAt), nil
}

// UpdateMemberRole changes a member's role (OWNER only, last-owner guard).
func (s *OrgService) UpdateMemberRole(grantorID uuid.UUID, ref string, targetID uuid.UUID, role orgmodel.MemberRole) (*orgdto.Member, error) {
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
	if u == nil {
		u = &model.User{ID: targetID}
	}
	return toMember(u, role, m.CreatedAt), nil
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

// toMember maps identity + membership rows onto the wire shape.
func toMember(u *model.User, role orgmodel.MemberRole, joinedAt time.Time) *orgdto.Member {
	return &orgdto.Member{
		UserID: u.ID.String(), Email: u.Email, Name: orgutils.DisplayNameOf(u),
		EmailVerified: u.EmailVerified, Role: string(role),
		JoinedAt: orgutils.FormatTime(joinedAt),
	}
}

// UpdateMyPreferences flips the caller's announcement opt-in (any MEMBER).
func (s *OrgService) UpdateMyPreferences(userID uuid.UUID, ref string, notify bool) error {
	o, _, err := s.membership(userID, ref)
	if err != nil {
		return err
	}
	return s.repo.SetNotifyEvents(nil, o.ID, userID, notify, time.Now())
}

// ResolveOrgID resolves a UUID-or-slug ref to an org ID (no membership
// check; backs the event domain's OrgResolver).
func (s *OrgService) ResolveOrgID(ref string) (uuid.UUID, error) {
	o, err := s.resolveOrg(ref)
	if err != nil {
		return uuid.Nil, err
	}
	return o.ID, nil
}

// IsMember reports membership (backs the event domain's OrgResolver).
func (s *OrgService) IsMember(userID, orgID uuid.UUID) bool {
	_, err := s.repo.GetMembership(orgID, userID)
	return err == nil
}

// OrgName returns the org display name (backs event announcements).
func (s *OrgService) OrgName(orgID uuid.UUID) (string, error) {
	o, err := s.repo.GetOrgByID(orgID)
	if err != nil {
		return "", err
	}
	return o.Name, nil
}

// NotifyTargets returns opted-in members with display data for event
// announcements (backs the event domain's OrgResolver).
func (s *OrgService) NotifyTargets(orgID uuid.UUID) ([]orgdto.MemberNotify, error) {
	ms, _, err := s.repo.ListMemberships(orgID, 100000, 0)
	if err != nil {
		return nil, err
	}
	out := make([]orgdto.MemberNotify, 0, len(ms))
	for _, m := range ms {
		if !m.NotifyEvents {
			continue
		}
		u, err := s.users.GetUserByID(m.UserID)
		if err != nil {
			continue
		}
		out = append(out, orgdto.MemberNotify{
			UserID: u.ID, Email: u.Email, Name: orgutils.DisplayNameOf(u),
		})
	}
	return out, nil
}
