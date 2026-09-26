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
	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	auth "github.com/nikhea/rallya/internal/auth"
	authdto "github.com/nikhea/rallya/internal/auth/dto"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/repository"
	orgutils "github.com/nikhea/rallya/internal/organization/utils"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
)

const inviteTTL = 7 * 24 * time.Hour

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// OrgService orchestrates org flows over OrgRepository.
// Identity comes from auth.UserReader; mail via jobs.Enqueuer;
// IAM grouping sync and policy seeds via consumer-side seams
// (nil-safe when unwired: tests, pre-IAM).
type OrgService struct {
	repo     *repository.OrgRepository
	users    auth.UserReader
	enqueuer jobs.Enqueuer
	syncer   GroupSyncer
	seeder   PolicySeeder
	cleaner  AssetCleaner
	auditor  auditsvc.Emitter
	apiKeys  ApiKeyStore
	plans    EntitlementProvider
}

// EntitlementProvider resolves subscription entitlements (implemented by
// the subscription domain; nil resolves Free — fail-closed restrictive).
type EntitlementProvider interface {
	EntitlementFor(orgID uuid.UUID) (submodel.Entitlement, error)
}

// SetEntitlementProvider wires plan quota/feature enforcement.
func (s *OrgService) SetEntitlementProvider(p EntitlementProvider) { s.plans = p }

// entitlement resolves the org's tier (Free when unwired or on error:
// restrictive default, never open).
func (s *OrgService) entitlement(orgID uuid.UUID) submodel.Entitlement {
	if s.plans == nil {
		return submodel.FreeEntitlement()
	}
	ent, err := s.plans.EntitlementFor(orgID)
	if err != nil {
		return submodel.FreeEntitlement()
	}
	return ent
}

// NewOrgService builds the service. users is required; enqueuer/syncer/
// seeder are wired via setters and nil-safe when absent.
func NewOrgService(repo *repository.OrgRepository, users auth.UserReader) *OrgService {
	return &OrgService{repo: repo, users: users}
}

// SetEnqueuer wires River job insertion.
func (s *OrgService) SetEnqueuer(e jobs.Enqueuer) { s.enqueuer = e }

// SetGroupSyncer wires IAM grouping sync.
func (s *OrgService) SetGroupSyncer(g GroupSyncer) { s.syncer = g }

// SetPolicySeeder wires IAM per-org policy seeding.
func (s *OrgService) SetPolicySeeder(p PolicySeeder) { s.seeder = p }

// SetAssetCleaner wires event asset cleanup on org delete.
func (s *OrgService) SetAssetCleaner(a AssetCleaner) { s.cleaner = a }

// SetAuditEmitter wires audit-trail emission (nil-safe when absent).
func (s *OrgService) SetAuditEmitter(a auditsvc.Emitter) { s.auditor = a }

// emit records one audit entry (nil-safe when unwired). Call inside the
// action's tx so entry and mutation commit atomically; emit failure fails
// the action (loud beats gappy).
func (s *OrgService) emit(tx *gorm.DB, e auditsvc.Entry) error {
	if s.auditor == nil {
		return nil
	}
	return s.auditor.EmitTx(tx, e)
}

// ---------- organizations ----------

// CreateOrg creates an org with the caller as OWNER.
// An empty slug is derived from the name and auto-uniquified
// (acme, acme-2, ...). An explicit slug that is taken returns ErrSlugTaken.
func (s *OrgService) CreateOrg(creatorID uuid.UUID, name, slug, logo string) (*orgdto.Org, error) {
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
			if err := s.repo.CreateMembership(tx, m); err != nil {
				return err
			}
			return s.emit(tx, auditsvc.Entry{
				OrgID: &o.ID, ActorID: &creatorID,
				Action: "org.created", ObjectType: auditmodel.ObjectOrg, ObjectID: &o.ID,
				After: map[string]any{"name": name, "slug": slug},
			})
		})
		if err == nil {
			s.seed(o.ID)
			s.sync(creatorID, o.ID, orgutils.PtrRole(orgmodel.MemberRoleOwner))
			return toOrg(o, orgmodel.MemberRoleOwner), nil
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
func (s *OrgService) GetOrg(userID uuid.UUID, ref string) (*orgdto.Org, error) {
	o, m, err := s.membership(userID, ref)
	if err != nil {
		return nil, err
	}
	return toOrg(o, m.Role), nil
}

// UpdateOrg renames/re-slugs an org (ADMIN+).
func (s *OrgService) UpdateOrg(userID uuid.UUID, ref, name, slug, logo string) (*orgdto.Org, error) {
	o, m, err := s.requireRole(userID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return nil, err
	}
	before := map[string]any{"name": o.Name, "slug": o.Slug}
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
	after := map[string]any{"name": o.Name, "slug": o.Slug}
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpdateOrg(tx, o); err != nil {
			if orgutils.IsUniqueViolation(err) {
				return ErrSlugTaken
			}
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &o.ID, ActorID: &userID,
			Action: "org.updated", ObjectType: auditmodel.ObjectOrg, ObjectID: &o.ID,
			Before: before, After: after,
		})
	})
	if err != nil {
		return nil, err
	}
	return toOrg(o, m.Role), nil
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
	// Assets first (orphan rows self-heal; orphan files cost money),
	// then rows cascade, then derived IAM state.
	s.cleanAssets(o.ID)
	orgID, orgName := o.ID, o.Name
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: &userID,
			Action: "org.deleted", ObjectType: auditmodel.ObjectOrg, ObjectID: &orgID,
			Before: map[string]any{"name": orgName},
		}); err != nil {
			return err
		}
		return s.repo.DeleteOrg(tx, o.ID)
	}); err != nil {
		return err
	}
	for _, m := range members {
		s.sync(m.UserID, o.ID, nil)
	}
	s.seedRemove(o.ID)
	return nil
}

// toOrg maps an org row + caller role onto the wire shape.
func toOrg(o *orgmodel.Organization, role orgmodel.MemberRole) *orgdto.Org {
	return &orgdto.Org{
		ID: o.ID.String(), Name: o.Name, Slug: o.Slug, LogoURL: o.LogoURL,
		Role: string(role), CreatedAt: orgutils.FormatTime(o.CreatedAt),
	}
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

// IsOwner reports org ownership (subscription billing gate).
// Outsiders and missing rows read false (never leaks membership).
// Implements the subscription domain's OwnerChecker contract.
func (s *OrgService) IsOwner(userID, orgID uuid.UUID) (bool, error) {
	m, err := s.repo.GetMembership(orgID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return m.Role == orgmodel.MemberRoleOwner, nil
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

// seed installs per-org Casbin policies (best-effort post-commit).
func (s *OrgService) seed(orgID uuid.UUID) {
	if s.seeder == nil {
		return
	}
	if err := s.seeder.SeedOrgPolicies(orgID); err != nil {
		slog.Error("iam policy seed failed", "org", orgID, "error", err)
	}
}

// seedRemove drops per-org Casbin policies (best-effort post-delete).
func (s *OrgService) seedRemove(orgID uuid.UUID) {
	if s.seeder == nil {
		return
	}
	s.seeder.RemoveOrgPolicies(orgID)
}

// cleanAssets removes an org's stored event assets (best-effort pre-delete).
func (s *OrgService) cleanAssets(orgID uuid.UUID) {
	if s.cleaner == nil {
		return
	}
	if err := s.cleaner.DeleteOrgAssets(orgID); err != nil {
		slog.Error("org asset cleanup failed", "org", orgID, "error", err)
	}
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

// syncCustomGrouping propagates one custom grant/revoke. Best-effort
// post-commit like sync (the adapter can't join the PG tx).
func (s *OrgService) syncCustomGrouping(userID, orgID uuid.UUID, role string, grant bool) {
	if s.syncer == nil {
		return
	}
	if err := s.syncer.SyncCustomGrouping(userID, orgID, role, grant); err != nil {
		slog.Error("iam custom grouping sync failed", "user", userID, "org", orgID, "role", role, "error", err)
	}
}

// syncCustomPolicies materializes/revokes one role's p-rows. Best-effort
// post-commit; the admin reseed heals gaps.
func (s *OrgService) syncCustomPolicies(orgID uuid.UUID, role string, perms []RolePermission, remove bool) {
	if s.syncer == nil {
		return
	}
	if err := s.syncer.SyncCustomPolicies(orgID, role, perms, remove); err != nil {
		slog.Error("iam custom policy sync failed", "org", orgID, "role", role, "error", err)
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
