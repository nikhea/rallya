package iam

import (
	"github.com/casbin/casbin/v3"
	"github.com/google/uuid"
)

// Custom-role policy materialization: role definitions live in the org
// domain's role_definitions table; their enforcement rows land here,
// per-row additive like SeedOrgPolicies (bulk AddPolicies cannot heal
// partial drift).

// EnsureCustomRolePolicies adds an org's p-rows for one custom role,
// skipping rows already present. Returns the added count.
func EnsureCustomRolePolicies(e *casbin.Enforcer, orgID uuid.UUID, role string, perms []Permission) (int, error) {
	dom := orgID.String()
	var added int
	err := LockWrite(func() error {
		for _, p := range perms {
			row := []string{role, dom, p.Obj, p.Act}
			ok, err := e.HasPolicy(row)
			if err != nil {
				return err
			}
			if ok {
				continue
			}
			if _, err := e.AddPolicy(row); err != nil {
				return err
			}
			added++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return added, nil
}

// RemoveCustomRolePolicies drops every p-row for one custom role in an org.
// Returns the removed count.
func RemoveCustomRolePolicies(e *casbin.Enforcer, orgID uuid.UUID, role string) (int, error) {
	var removed int
	err := LockWrite(func() error {
		rows, err := e.GetFilteredPolicy(0, role, orgID.String())
		if err != nil {
			return err
		}
		for _, row := range rows {
			if ok, err := e.RemovePolicy(row); err != nil {
				return err
			} else if ok {
				removed++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// ListCustomRolePolicies returns an org's non-fixed p-rows grouped by role
// name (fixed roles are OWNER/ADMIN/MEMBER; customs never collide).
func ListCustomRolePolicies(e *casbin.Enforcer, orgID uuid.UUID) (map[string][]Permission, error) {
	var rows [][]string
	var err error
	func() {
		mu.RLock()
		defer mu.RUnlock()
		rows, err = e.GetFilteredPolicy(1, orgID.String())
	}()
	if err != nil {
		return nil, err
	}
	out := map[string][]Permission{}
	for _, row := range rows {
		if len(row) != 4 || isFixedRole(row[0]) {
			continue
		}
		out[row[0]] = append(out[row[0]], Permission{Obj: row[2], Act: row[3]})
	}
	return out, nil
}
