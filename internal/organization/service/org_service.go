package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/cmd/config"
	auth "github.com/nikhea/rallya/internal/auth"
	authdto "github.com/nikhea/rallya/internal/auth/dto"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/repository"
	orgutils "github.com/nikhea/rallya/internal/organization/utils"
)

const inviteTTL = 7 * 24 * time.Hour

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// OrgDetail is an org with the caller's membership role.
type OrgDetail struct {
	Organization orgmodel.Organization `json:"organization"`
	Role         orgmodel.MemberRole   `json:"role"`
}

// OrgService orchestrates org flows over OrgRepository.
// Identity comes from auth.UserReader; mail via jobs.Enqueuer;
// IAM grouping sync via organization.GroupSyncer (nil-safe pre-IAM).
type OrgService struct {
	repo     *repository.OrgRepository
	users    auth.UserReader
	enqueuer jobs.Enqueuer
	syncer   GroupSyncer
}

// NewOrgService builds the service. users is required; enqueuer/syncer
// are wired via setters and nil-safe when absent (tests, pre-IAM).
func NewOrgService(repo *repository.OrgRepository, users auth.UserReader) *OrgService {
	return &OrgService{repo: repo, users: users}
}

// SetEnqueuer wires River job insertion.
func (s *OrgService) SetEnqueuer(e jobs.Enqueuer) { s.enqueuer = e }

// SetGroupSyncer wires IAM grouping sync.
func (s *OrgService) SetGroupSyncer(g GroupSyncer) { s.syncer = g }

// ---------- organizations ----------

// CreateOrg creates an org with the caller as OWNER.
// An empty slug is derived from the name and auto-uniquified
// (acme, acme-2, ...). An explicit slug that is taken returns ErrSlugTaken.
func (s *OrgService) CreateOrg(creatorID uuid.UUID, name, slug, logo string) (*OrgDetail, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	auto := strings.TrimSpace(slug) == ""
	var base string
	if auto {
		base = orgutils.Slugify(name)
		if base == "" {
			base = "org"
		}
	} else {
		slug = strings.ToLower(strings.TrimSpace(slug))
		if !slugRe.MatchString(slug) {
			return nil, ErrInvalidSlug
		}
	}
	o := &orgmodel.Organization{Name: name, CreatedBy: &creatorID}
	if strings.TrimSpace(logo) != "" {
		o.LogoURL = &logo
	}
	m := &orgmodel.Membership{Role: orgmodel.MemberRoleOwner}
	for attempt := 0; attempt < 4; attempt++ {
		if auto {
			// Probe from the original base every attempt so a lost
			// slug race continues the sequence (acme-3, not acme-2-2).
			next, err := s.uniqueSlug(base)
			if err != nil {
				return nil, err
			}
			slug = next
		}
		o.Slug = slug
		err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
			if err := s.repo.CreateOrg(tx, o); err != nil {
				if orgutils.IsUniqueViolation(err) {
					return ErrSlugTaken
				}
				return err
			}
			m.OrganizationID = o.ID
			m.UserID = creatorID
			return s.repo.CreateMembership(tx, m)
		})
		if err == nil {
			s.sync(creatorID, o.ID, orgutils.PtrRole(orgmodel.MemberRoleOwner))
			return &OrgDetail{Organization: *o, Role: orgmodel.MemberRoleOwner}, nil
		}
		if !errors.Is(err, ErrSlugTaken) || !auto {
			return nil, err
		}
		// Lost a slug race with an auto slug: re-probe next attempt.
	}
	return nil, ErrSlugTaken
}

// ListUserOrgs implements dto.MembershipLister (GET /auth/me embedding).
func (s *OrgService) ListUserOrgs(userID uuid.UUID) ([]authdto.OrgMembership, error) {
	return s.ListMemberships(userID)
}

// ListMemberships returns org context rows for a user.
func (s *OrgService) ListMemberships(userID uuid.UUID) ([]authdto.OrgMembership, error) {
	ms, err := s.repo.ListUserMemberships(userID)
	if err != nil {
		return nil, err
	}
	out := make([]authdto.OrgMembership, 0, len(ms))
	for _, m := range ms {
		o, err := s.repo.GetOrgByID(m.OrganizationID)
		if err != nil {
			continue // org deleted concurrently; skip
		}
		out = append(out, authdto.OrgMembership{
			ID: o.ID.String(), Slug: o.Slug, Name: o.Name, Role: string(m.Role),
			JoinedAt: m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	return out, nil
}

// GetOrg returns an org for members (non-members get not-found: stealth).
func (s *OrgService) GetOrg(userID uuid.UUID, ref string) (*OrgDetail, error) {
	o, m, err := s.membership(userID, ref)
	if err != nil {
		return nil, err
	}
	return &OrgDetail{Organization: *o, Role: m.Role}, nil
}

// UpdateOrg renames/re-slugs an org (ADMIN+).
func (s *OrgService) UpdateOrg(userID uuid.UUID, ref, name, slug, logo string) (*OrgDetail, error) {
	o, m, err := s.requireRole(userID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return nil, err
	}
	if name != "" {
		o.Name = strings.TrimSpace(name)
	}
	if slug != "" {
		slug = strings.ToLower(strings.TrimSpace(slug))
		if !slugRe.MatchString(slug) {
			return nil, ErrInvalidSlug
		}
		o.Slug = slug
	}
	o.LogoURL = nil
	if strings.TrimSpace(logo) != "" {
		o.LogoURL = &logo
	}
	if err := s.repo.UpdateOrg(nil, o); err != nil {
		if orgutils.IsUniqueViolation(err) {
			return nil, ErrSlugTaken
		}
		return nil, err
	}
	return &OrgDetail{Organization: *o, Role: m.Role}, nil
}

// DeleteOrg removes an org and cascades memberships/invites (OWNER).
func (s *OrgService) DeleteOrg(userID uuid.UUID, ref string) error {
	o, _, err := s.requireRole(userID, ref, orgmodel.MemberRoleOwner)
	if err != nil {
		return err
	}
	members, _, err := s.repo.ListMemberships(o.ID, 100000, 0)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteOrg(nil, o.ID); err != nil {
		return err
	}
	for _, m := range members {
		s.sync(m.UserID, o.ID, nil)
	}
	return nil
}

// ---------- helpers ----------

// resolveOrg accepts a UUID or slug.
func (s *OrgService) resolveOrg(ref string) (*orgmodel.Organization, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		o, err := s.repo.GetOrgByID(id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrOrgNotFound
			}
			return nil, err
		}
		return o, nil
	}
	o, err := s.repo.GetOrgBySlug(strings.ToLower(ref))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrgNotFound
		}
		return nil, err
	}
	return o, nil
}

// membership loads org + caller membership (stealth not-found either way).
func (s *OrgService) membership(userID uuid.UUID, ref string) (*orgmodel.Organization, *orgmodel.Membership, error) {
	o, err := s.resolveOrg(ref)
	if err != nil {
		return nil, nil, err
	}
	m, err := s.repo.GetMembership(o.ID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrOrgNotFound
		}
		return nil, nil, err
	}
	return o, m, nil
}

// requireRole enforces a minimum role (stealth not-found for outsiders).
func (s *OrgService) requireRole(userID uuid.UUID, ref string, min orgmodel.MemberRole) (*orgmodel.Organization, *orgmodel.Membership, error) {
	o, m, err := s.membership(userID, ref)
	if err != nil {
		return nil, nil, err
	}
	if m.Role.Level() < min.Level() {
		return nil, nil, ErrForbidden
	}
	return o, m, nil
}

// guardLastOwner blocks demoting/removing the final OWNER.
// Fail-closed: counting errors abort the mutation.
func (s *OrgService) guardLastOwner(orgID uuid.UUID) error {
	n, err := s.repo.CountOwners(orgID)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastOwner
	}
	return nil
}

// getInvite loads an invite by ID.
func (s *OrgService) getInvite(id uuid.UUID) (*orgmodel.Invite, error) {
	inv, err := s.repo.GetInviteByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidInvite
		}
		return nil, err
	}
	return inv, nil
}

// sync propagates membership state to IAM (nil role = removal).
// Best-effort post-commit: failures log loud, the repair path is
// SyncUserPolicies; the membership itself always stands.
func (s *OrgService) sync(userID, orgID uuid.UUID, role *orgmodel.MemberRole) {
	if s.syncer == nil {
		return
	}
	if err := s.syncer.SyncMembership(userID, orgID, role); err != nil {
		slog.Error("iam grouping sync failed", "user", userID, "org", orgID, "error", err)
	}
}

// SyncUserPolicies repairs derived IAM state for a user (admin escape hatch).
func (s *OrgService) SyncUserPolicies(userID uuid.UUID) error {
	if s.syncer == nil {
		return nil
	}
	ms, err := s.repo.ListUserMemberships(userID)
	if err != nil {
		return err
	}
	for _, m := range ms {
		role := m.Role
		if err := s.syncer.SyncMembership(userID, m.OrganizationID, &role); err != nil {
			return err
		}
	}
	return nil
}

// enqueueTx inserts a job inside tx; skips silently without an enqueuer.
func (s *OrgService) enqueueTx(tx *gorm.DB, args interface{ Kind() string }) error {
	if s.enqueuer == nil {
		return nil
	}
	return s.enqueuer.EnqueueTx(context.Background(), tx, args)
}

func PtrRole(r orgmodel.MemberRole) *orgmodel.MemberRole { return &r }

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func DisplayName(email string, firstName *string) string {
	if firstName != nil && strings.TrimSpace(*firstName) != "" {
		return strings.TrimSpace(*firstName)
	}
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return email
}

func DisplayNameOf(u *model.User) string {
	if u == nil {
		return ""
	}
	var first *string
	if u.Profile != nil {
		first = u.Profile.FirstName
	}
	return orgutils.DisplayName(u.Email, first)
}

func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == ' ' || r == '-' || r == '_':
			if !dash && b.Len() > 0 {
				b.WriteRune('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// slugBase strips a trailing -N suffix so retries continue the sequence
// (acme-2 taken -> probe acme-3, not acme-2-2).
func SlugBase(slug string) string {
	if i := strings.LastIndex(slug, "-"); i > 0 {
		tail := slug[i+1:]
		n := 0
		for _, r := range tail {
			if r < '0' || r > '9' {
				return slug
			}
			n = n*10 + int(r-'0')
		}
		if n >= 2 {
			return slug[:i]
		}
	}
	return slug
}

// uniqueSlug probes a free slug from a slugified base:
// base, base-2, base-3, ... with a random fallback after 100 tries.
func (s *OrgService) uniqueSlug(base string) (string, error) {
	if _, err := s.repo.GetOrgBySlug(base); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return base, nil
		}
		return "", err
	}
	for i := 2; i < 100; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if _, err := s.repo.GetOrgBySlug(cand); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return cand, nil
			}
			return "", err
		}
	}
	raw, err := token.GenerateRawToken(3)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", base, raw[:6]), nil
}

func InviteLink(raw string) string {
	return config.AppURL() + "/orgs/invites/accept?token=" + raw
}

func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Postgres: `duplicate key value violates unique constraint ...`.
	// SQLite: `UNIQUE constraint failed: ...`.
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}
