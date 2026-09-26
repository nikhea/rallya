package service

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgutils "github.com/nikhea/rallya/internal/organization/utils"
)

// ---------- custom roles ----------

// MaxCustomRoles caps definitions per org (sanity bound, not a quota).
const MaxCustomRoles = 20

// RolePermission is one grant in a custom role (consumer-declared; IAM
// materializes it — org never imports IAM, so no cycle).
type RolePermission struct {
	Object string `json:"object"`
	Action string `json:"action"`
}

// roleRegistry is the grantable vocabulary: every matrix (object, action)
// pair, mirrored from IAM. Pinned by TestRoleRegistryMatchesIAM — drift
// breaks tests loudly. Grants only; denies are unrepresentable by design.
var roleRegistry = map[string]map[string]bool{
	"org":      {"read": true, "update": true},
	"member":   {"read": true, "create": true, "delete": true},
	"invite":   {"read": true, "create": true, "delete": true},
	"event":    {"read": true, "create": true, "update": true, "delete": true, "publish": true},
	"ticket":   {"read": true, "create": true, "update": true, "delete": true},
	"attendee": {"read": true, "create": true, "update": true},
	"checkin":  {"read": true, "create": true, "update": true},
	"kit":      {"read": true, "create": true, "update": true, "delete": true},
	"audit":    {"read": true},
	"role":     {"read": true, "create": true, "update": true, "delete": true},
	"apikey":   {"read": true, "create": true, "delete": true},
}

// reservedRoleNames can never be custom definitions (enforcement collision).
var reservedRoleNames = map[string]bool{
	"owner": true, "admin": true, "member": true, "superadmin": true,
}

// RoleDetail is a definition with its holder count.
type RoleDetail struct {
	Name        string
	Permissions []RolePermission
	Holders     int64
}

// normalizeRoleName lowercases + validates slug shape and reservations.
func normalizeRoleName(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" || !slugRe.MatchString(name) {
		return "", ErrInvalidRoleName
	}
	if reservedRoleNames[name] {
		return "", ErrRoleReserved
	}
	return name, nil
}

// normalizeRolePermissions dedupes + validates pairs against the registry.
// Empty sets are rejected (a grantless role is a no-op definition).
func normalizeRolePermissions(perms []RolePermission) ([]RolePermission, error) {
	if len(perms) == 0 {
		return nil, ErrInvalidRolePermissions
	}
	seen := map[RolePermission]bool{}
	out := make([]RolePermission, 0, len(perms))
	for _, p := range perms {
		obj, act := strings.ToLower(strings.TrimSpace(p.Object)), strings.ToLower(strings.TrimSpace(p.Action))
		if acts, ok := roleRegistry[obj]; !ok || !acts[act] {
			return nil, ErrInvalidRolePermissions
		}
		rp := RolePermission{Object: obj, Action: act}
		if seen[rp] {
			continue
		}
		seen[rp] = true
		out = append(out, rp)
	}
	return out, nil
}

func encodeRolePermissions(perms []RolePermission) string {
	raw, _ := json.Marshal(perms)
	return string(raw)
}

func decodeRolePermissions(stored string) []RolePermission {
	var out []RolePermission
	if err := json.Unmarshal([]byte(stored), &out); err != nil {
		return nil
	}
	return out
}

// DefineRole creates a custom role (OWNER-only). The definition + audit
// commit atomically; enforcement rows materialize post-commit (best-effort,
// house pattern — the adapter can't join the PG tx; reseed heals).
func (s *OrgService) DefineRole(grantorID uuid.UUID, ref, name string, perms []RolePermission) (*orgdto.CustomRole, error) {
	o, _, err := s.requireRole(grantorID, ref, orgmodel.MemberRoleOwner)
	if err != nil {
		return nil, err
	}
	role, err := normalizeRoleName(name)
	if err != nil {
		return nil, err
	}
	norm, err := normalizeRolePermissions(perms)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.GetRoleDef(o.ID, role); err == nil {
		return nil, ErrRoleExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if n, err := s.repo.CountRoleDefs(o.ID); err != nil {
		return nil, err
	} else if n >= MaxCustomRoles {
		return nil, ErrRoleCapReached
	}
	d := &orgmodel.RoleDefinition{OrganizationID: o.ID, Name: role, Permissions: encodeRolePermissions(norm)}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateRoleDef(tx, d); err != nil {
			if orgutils.IsUniqueViolation(err) {
				return ErrRoleExists
			}
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &o.ID, ActorID: &grantorID,
			Action: "role.created", ObjectType: auditmodel.ObjectRole, ObjectID: &d.ID,
			After: map[string]any{"name": role, "permissions": norm},
		})
	}); err != nil {
		return nil, err
	}
	s.syncCustomPolicies(o.ID, role, norm, false)
	return toCustomRole(d, norm, 0), nil
}

// UpdateRole replaces a role's permission set (OWNER-only). Assignee
// groupings stand — the grants underneath change, enforced on next check.
func (s *OrgService) UpdateRole(grantorID uuid.UUID, ref, name string, perms []RolePermission) (*orgdto.CustomRole, error) {
	o, _, err := s.requireRole(grantorID, ref, orgmodel.MemberRoleOwner)
	if err != nil {
		return nil, err
	}
	role, err := normalizeRoleName(name)
	if err != nil {
		return nil, err
	}
	norm, err := normalizeRolePermissions(perms)
	if err != nil {
		return nil, err
	}
	d, err := s.repo.GetRoleDef(o.ID, role)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}
	before := decodeRolePermissions(d.Permissions)
	d.Permissions = encodeRolePermissions(norm)
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpdateRoleDef(tx, d); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &o.ID, ActorID: &grantorID,
			Action: "role.updated", ObjectType: auditmodel.ObjectRole, ObjectID: &d.ID,
			Before: map[string]any{"permissions": before},
			After:  map[string]any{"permissions": norm},
		})
	}); err != nil {
		return nil, err
	}
	s.syncCustomPolicies(o.ID, role, before, true)
	s.syncCustomPolicies(o.ID, role, norm, false)
	holders, _ := s.repo.CountRoleAssignments(o.ID, role)
	return toCustomRole(d, norm, holders), nil
}

// DeleteRole removes a definition (OWNER-only). Blocked while assigned
// (409) — privilege changes stay explicit, never silent.
func (s *OrgService) DeleteRole(grantorID uuid.UUID, ref, name string) error {
	o, _, err := s.requireRole(grantorID, ref, orgmodel.MemberRoleOwner)
	if err != nil {
		return err
	}
	role, err := normalizeRoleName(name)
	if err != nil {
		return err
	}
	d, err := s.repo.GetRoleDef(o.ID, role)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRoleNotFound
		}
		return err
	}
	if n, err := s.repo.CountRoleAssignments(o.ID, role); err != nil {
		return err
	} else if n > 0 {
		return ErrRoleAssigned
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.DeleteRoleDef(tx, o.ID, role); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &o.ID, ActorID: &grantorID,
			Action: "role.deleted", ObjectType: auditmodel.ObjectRole, ObjectID: &d.ID,
			Before: map[string]any{"name": role},
		})
	}); err != nil {
		return err
	}
	s.syncCustomPolicies(o.ID, role, decodeRolePermissions(d.Permissions), true)
	return nil
}

// ListRoles returns definitions with holder counts (ADMIN+ read).
func (s *OrgService) ListRoles(userID uuid.UUID, ref string) ([]orgdto.CustomRole, error) {
	o, _, err := s.requireRole(userID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return nil, err
	}
	defs, err := s.repo.ListRoleDefs(o.ID)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountRoleAssignees(o.ID)
	if err != nil {
		return nil, err
	}
	out := make([]orgdto.CustomRole, 0, len(defs))
	for i := range defs {
		out = append(out, *toCustomRole(&defs[i], decodeRolePermissions(defs[i].Permissions), counts[defs[i].Name]))
	}
	return out, nil
}

// AssignRole grants a custom role to a member (OWNER-only). Idempotent.
func (s *OrgService) AssignRole(grantorID uuid.UUID, ref string, targetID uuid.UUID, name string) error {
	o, _, err := s.requireRole(grantorID, ref, orgmodel.MemberRoleOwner)
	if err != nil {
		return err
	}
	role, err := normalizeRoleName(name)
	if err != nil {
		return err
	}
	d, err := s.repo.GetRoleDef(o.ID, role)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRoleNotFound
		}
		return err
	}
	if _, err := s.repo.GetMembership(o.ID, targetID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrOrgNotFound
		}
		return err
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.AssignCustomRole(tx, o.ID, targetID, role); err != nil {
			if orgutils.IsUniqueViolation(err) {
				return nil // already held: converge below, succeed idempotently
			}
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &o.ID, ActorID: &grantorID,
			Action: "role.assigned", ObjectType: auditmodel.ObjectRole, ObjectID: &d.ID,
			After: map[string]any{"role": role, "user": targetID.String()},
		})
	}); err != nil {
		return err
	}
	s.syncCustomGrouping(targetID, o.ID, role, true)
	return nil
}

// UnassignRole revokes a custom role (OWNER-only). Idempotent.
func (s *OrgService) UnassignRole(grantorID uuid.UUID, ref string, targetID uuid.UUID, name string) error {
	o, _, err := s.requireRole(grantorID, ref, orgmodel.MemberRoleOwner)
	if err != nil {
		return err
	}
	role, err := normalizeRoleName(name)
	if err != nil {
		return err
	}
	d, err := s.repo.GetRoleDef(o.ID, role)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRoleNotFound
		}
		return err
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UnassignCustomRole(tx, o.ID, targetID, role); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &o.ID, ActorID: &grantorID,
			Action: "role.unassigned", ObjectType: auditmodel.ObjectRole, ObjectID: &d.ID,
			Before: map[string]any{"role": role, "user": targetID.String()},
		})
	}); err != nil {
		return err
	}
	s.syncCustomGrouping(targetID, o.ID, role, false)
	return nil
}

// ListMemberCustomRoles returns a member's custom grants (internal use:
// roster enrichment, admin convergence).
func (s *OrgService) ListMemberCustomRoles(orgID, userID uuid.UUID) ([]string, error) {
	return s.repo.ListUserCustomRoles(nil, orgID, userID)
}

func toCustomRole(d *orgmodel.RoleDefinition, perms []RolePermission, holders int64) *orgdto.CustomRole {
	wire := make([]orgdto.RolePermission, 0, len(perms))
	for _, p := range perms {
		wire = append(wire, orgdto.RolePermission{Object: p.Object, Action: p.Action})
	}
	return &orgdto.CustomRole{
		ID: d.ID.String(), Name: d.Name, Permissions: wire, Holders: holders,
		CreatedAt: d.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}
