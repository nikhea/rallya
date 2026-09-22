package iam

import (
	"fmt"
	"log/slog"

	"github.com/casbin/casbin/v3"
	"github.com/google/uuid"

	auth "github.com/nikhea/rallya/internal/auth"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
)

// Objects and actions gated by RequirePermission.
const (
	ObjOrg      = "org"
	ObjMember   = "member"
	ObjInvite   = "invite"
	ObjEvent    = "event"
	ObjTicket   = "ticket"
	ObjAttendee = "attendee"
	ObjCheckin  = "checkin"
	ObjAudit    = "audit"
	ObjRole     = "role"

	ActRead    = "read"
	ActCreate  = "create"
	ActUpdate  = "update"
	ActDelete  = "delete"
	ActManage  = "manage"
	ActPublish = "publish"
)

// SuperAdminRole is the domain-less platform role (g2 grouping).
const SuperAdminRole = "superadmin"

// Permission is one (object, action) grant.
type Permission struct {
	Obj string
	Act string
}

// AdminPermissions is the explicit ADMIN matrix (OWNER uses the wildcard).
func AdminPermissions() []Permission {
	return []Permission{
		{ObjOrg, ActRead},
		{ObjOrg, ActUpdate},
		{ObjMember, ActRead},
		{ObjMember, ActCreate},
		{ObjMember, ActDelete},
		{ObjInvite, ActRead},
		{ObjInvite, ActCreate},
		{ObjInvite, ActDelete},
		{ObjEvent, ActRead},
		{ObjEvent, ActCreate},
		{ObjEvent, ActUpdate},
		{ObjEvent, ActDelete},
		{ObjEvent, ActPublish},
		{ObjTicket, ActRead},
		{ObjTicket, ActCreate},
		{ObjTicket, ActUpdate},
		{ObjTicket, ActDelete},
		{ObjAttendee, ActRead},
		{ObjAttendee, ActCreate},
		{ObjAttendee, ActUpdate},
		{ObjCheckin, ActRead},
		{ObjCheckin, ActCreate},
		{ObjCheckin, ActUpdate},
		{ObjAudit, ActRead},
		{ObjRole, ActRead},
	}
}

// MemberPermissions is the explicit MEMBER matrix (read-only).
func MemberPermissions() []Permission {
	return []Permission{
		{ObjOrg, ActRead},
		{ObjMember, ActRead},
		{ObjEvent, ActRead},
		{ObjTicket, ActRead},
	}
}

// SeedOrgPolicies installs the default per-org role policies. Per-row
// additive: existing rows are left alone AND missing rows are healed, so
// it doubles as the repair path (Casbin's AddPolicies is all-or-nothing
// and cannot heal partial drift — hence the loop).
func SeedOrgPolicies(e *casbin.Enforcer, orgID uuid.UUID) error {
	dom := orgID.String()
	rules := [][]string{{"OWNER", dom, "*", "*"}}
	for _, p := range AdminPermissions() {
		rules = append(rules, []string{"ADMIN", dom, p.Obj, p.Act})
	}
	for _, p := range MemberPermissions() {
		rules = append(rules, []string{"MEMBER", dom, p.Obj, p.Act})
	}
	var added int
	err := LockWrite(func() error {
		for _, r := range rules {
			ok, err := e.HasPolicy(r)
			if err != nil {
				return fmt.Errorf("check org policy: %w", err)
			}
			if ok {
				continue
			}
			if _, err := e.AddPolicy(r); err != nil {
				return fmt.Errorf("add org policy: %w", err)
			}
			added++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("seed org policies: %w", err)
	}
	if added == 0 {
		slog.Debug("org policies already present", "org", dom)
	}
	return nil
}

// RemoveOrgPolicies drops every policy row for an org domain (p + g).
// Best-effort: call after org delete; logs, never fails the caller.
func RemoveOrgPolicies(e *casbin.Enforcer, orgID uuid.UUID) {
	dom := orgID.String()
	_ = LockWrite(func() error {
		if _, err := e.RemoveFilteredPolicy(1, dom); err != nil {
			slog.Warn("remove org policies failed", "org", dom, "error", err)
		}
		if _, err := e.RemoveFilteredGroupingPolicy(2, dom); err != nil {
			slog.Warn("remove org groupings failed", "org", dom, "error", err)
		}
		return nil
	})
}

// OrgPolicySeeder implements the organization service's PolicySeeder
// (org declares, iam implements — dependency stays org -> iam).
type OrgPolicySeeder struct {
	E *casbin.Enforcer
}

// NewOrgPolicySeeder wraps the enforcer for org wiring.
func NewOrgPolicySeeder(e *casbin.Enforcer) OrgPolicySeeder {
	return OrgPolicySeeder{E: e}
}

// SeedOrgPolicies implements PolicySeeder.
func (s OrgPolicySeeder) SeedOrgPolicies(orgID uuid.UUID) error {
	return SeedOrgPolicies(s.E, orgID)
}

// RemoveOrgPolicies implements PolicySeeder.
func (s OrgPolicySeeder) RemoveOrgPolicies(orgID uuid.UUID) {
	RemoveOrgPolicies(s.E, orgID)
}

var _ orgservice.PolicySeeder = OrgPolicySeeder{}

// SeedSuperAdmins syncs platform superadmins from account emails:
// missing groupings are added, stale ones removed. Pure no-op when the
// list is empty (but never removes existing on empty input — rotation
// requires an explicit non-empty list).
func SeedSuperAdmins(e *casbin.Enforcer, users auth.UserReader, emails []string) error {
	if len(emails) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, email := range emails {
		u, err := users.GetUserByEmail(email)
		if err != nil {
			slog.Warn("superadmin seed: unknown email, skipping", "email", email)
			continue
		}
		want[u.ID.String()] = true
		if err := LockWrite(func() error {
			ok, err := e.AddNamedGroupingPolicy("g2", u.ID.String(), SuperAdminRole)
			if err != nil {
				return fmt.Errorf("seed superadmin %s: %w", email, err)
			}
			if ok {
				slog.Info("superadmin granted", "email", email)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	var existing [][]string
	if err := func() error {
		mu.RLock()
		defer mu.RUnlock()
		var err error
		existing, err = e.GetFilteredNamedGroupingPolicy("g2", 1, SuperAdminRole)
		return err
	}(); err != nil {
		return fmt.Errorf("list superadmins: %w", err)
	}
	for _, row := range existing {
		if len(row) == 0 || want[row[0]] {
			continue
		}
		uid := row[0]
		if err := LockWrite(func() error {
			_, err := e.RemoveNamedGroupingPolicy("g2", uid, SuperAdminRole)
			return err
		}); err != nil {
			return fmt.Errorf("remove stale superadmin %s: %w", uid, err)
		}
		slog.Info("superadmin revoked", "user", uid)
	}
	return nil
}

// IsSuperAdmin reports platform superadmin status (for middleware + tests).
func IsSuperAdmin(e *casbin.Enforcer, userID uuid.UUID) bool {
	mu.RLock()
	defer mu.RUnlock()
	ok, err := e.HasNamedGroupingPolicy("g2", userID.String(), SuperAdminRole)
	if err != nil {
		return false
	}
	return ok
}
