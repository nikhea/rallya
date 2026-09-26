package service

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgutils "github.com/nikhea/rallya/internal/organization/utils"
	subservice "github.com/nikhea/rallya/internal/subscription/service"
)

// ---------- invites ----------

// InviteMember invites an email at a role (ADMIN+; role within grantor's).
// Supersedes pending invites; the email job enqueues transactionally.
func (s *OrgService) InviteMember(grantorID uuid.UUID, ref, email string, role orgmodel.MemberRole) error {
	o, g, err := s.requireRole(grantorID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return err
	}
	if !role.Valid() {
		role = orgmodel.MemberRoleMember
	}
	if g.Role.Level() < role.Level() {
		return ErrForbidden
	}
	email = orgutils.NormalizeEmail(email)
	if target, err := s.users.GetUserByEmail(email); err == nil {
		if _, merr := s.repo.GetMembership(o.ID, target.ID); merr == nil {
			return ErrAlreadyMember
		}
	}
	raw, err := token.GenerateRawToken(32)
	if err != nil {
		return err
	}
	now := time.Now()
	inv := &orgmodel.Invite{
		OrganizationID: o.ID, Email: email, Role: role,
		TokenHash: token.HashToken(raw), ExpiresAt: now.Add(inviteTTL),
		InvitedBy: &grantorID,
	}
	grantor, _ := s.users.GetUserByID(grantorID)
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if pending, err := s.repo.PendingInvite(tx, o.ID, email); err == nil {
			if err := s.repo.ConsumeInvite(tx, pending.ID, false, now); err != nil {
				return err
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := s.repo.CreateInvite(tx, inv); err != nil {
			return err
		}
		return s.enqueueTx(tx, jobs.SendOrgInviteEmailArgs{
			OrgID: o.ID, OrgName: o.Name, OrgSlug: o.Slug,
			Email: email, Name: orgutils.DisplayName(email, nil), Role: string(role),
			InviterName: orgutils.DisplayNameOf(grantor), InviteLink: orgutils.InviteLink(raw),
		})
	}); err != nil {
		return err
	}
	return nil
}

// ListInvites returns pending invites (ADMIN+).
func (s *OrgService) ListInvites(userID uuid.UUID, ref string, limit, offset int) ([]orgdto.Invite, int64, error) {
	o, _, err := s.requireRole(userID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return nil, 0, err
	}
	invites, total, err := s.repo.ListInvites(o.ID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]orgdto.Invite, 0, len(invites))
	for _, in := range invites {
		out = append(out, *toInvite(in))
	}
	return out, total, nil
}

// toInvite maps an invite row onto the wire shape (no token hash).
func toInvite(in orgmodel.Invite) *orgdto.Invite {
	return &orgdto.Invite{
		ID: in.ID.String(), Email: in.Email, Role: string(in.Role),
		ExpiresAt: orgutils.FormatTime(in.ExpiresAt),
		CreatedAt: orgutils.FormatTime(in.CreatedAt),
	}
}

// RevokeInvite cancels a pending invite (ADMIN+).
func (s *OrgService) RevokeInvite(userID uuid.UUID, ref string, inviteID uuid.UUID) error {
	o, _, err := s.requireRole(userID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return err
	}
	return s.revokeInvite(o.ID, inviteID)
}

func (s *OrgService) revokeInvite(orgID, inviteID uuid.UUID) error {
	now := time.Now()
	inv, err := s.getInvite(inviteID)
	if err != nil {
		return err
	}
	if inv.OrganizationID != orgID || !inv.Pending(now) {
		return ErrInvalidInvite
	}
	return s.repo.ConsumeInvite(nil, inv.ID, false, now)
}

// AcceptInvite consumes an invite, creating the membership. The caller's
// account email must match the invite (case-insensitive). Re-accepting a
// consumed invite as a member succeeds idempotently (safe client retries).
func (s *OrgService) AcceptInvite(userID uuid.UUID, raw string) (*orgdto.Org, error) {
	now := time.Now()
	inv, err := s.repo.GetInviteByHash(token.HashToken(raw))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidInvite
		}
		return nil, err
	}
	u, err := s.users.GetUserByID(userID)
	if err != nil {
		return nil, ErrInvalidInvite
	}
	if u.Status != model.UserStatusActive {
		return nil, ErrForbidden
	}
	o, err := s.repo.GetOrgByID(inv.OrganizationID)
	if err != nil {
		return nil, ErrInvalidInvite
	}
	if !inv.Pending(now) {
		if inv.AcceptedAt == nil {
			return nil, ErrInvalidInvite
		}
		existing, err := s.repo.GetMembership(o.ID, u.ID)
		if err != nil {
			return nil, ErrInvalidInvite
		}
		return toOrg(o, existing.Role), nil
	}
	if !strings.EqualFold(u.Email, inv.Email) {
		return nil, ErrInviteEmailMismatch
	}
	if existing, err := s.repo.GetMembership(o.ID, u.ID); err == nil {
		_ = s.repo.ConsumeInvite(nil, inv.ID, true, now)
		return toOrg(o, existing.Role), nil
	}
	if n, err := s.repo.CountMembers(o.ID); err != nil {
		return nil, err
	} else if n >= int64(s.entitlement(o.ID).Limits.MaxMembers) {
		return nil, subservice.ErrUpgradeRequired
	}
	m := &orgmodel.Membership{OrganizationID: o.ID, UserID: u.ID, Role: inv.Role}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateMembership(tx, m); err != nil {
			if orgutils.IsUniqueViolation(err) {
				return ErrAlreadyMember
			}
			return err
		}
		if err := s.repo.ConsumeInvite(tx, inv.ID, true, now); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &o.ID, ActorID: &u.ID,
			Action: "member.added", ObjectType: auditmodel.ObjectMember, ObjectID: &u.ID,
			After: map[string]any{"email": u.Email, "role": string(inv.Role), "via": "invite"},
		})
	}); err != nil {
		return nil, err
	}
	s.sync(u.ID, o.ID, &inv.Role)
	return toOrg(o, inv.Role), nil
}

// DeclineInvite stamps an invite declined ("reject" path). Idempotent;
// accepted invites cannot be declined.
func (s *OrgService) DeclineInvite(userID uuid.UUID, raw string) error {
	now := time.Now()
	inv, err := s.repo.GetInviteByHash(token.HashToken(raw))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidInvite
		}
		return err
	}
	if inv.AcceptedAt != nil {
		return ErrInviteConsumed
	}
	if inv.DeclinedAt != nil {
		return nil
	}
	if now.After(inv.ExpiresAt) {
		return ErrInvalidInvite
	}
	u, err := s.users.GetUserByID(userID)
	if err != nil {
		return ErrInvalidInvite
	}
	if !strings.EqualFold(u.Email, inv.Email) {
		return ErrInviteEmailMismatch
	}
	return s.repo.ConsumeInvite(nil, inv.ID, false, now)
}
