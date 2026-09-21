package iam

import (
	"github.com/casbin/casbin/v3"
	"github.com/google/uuid"
)

// Repair reads for the admin surface: locked snapshots behind the package
// RWMutex (Casbin management reads don't self-synchronize; see enforcer.go).

// CountOrgPolicies returns the number of p-rows in an org domain.
func CountOrgPolicies(e *casbin.Enforcer, orgID uuid.UUID) (int, error) {
	var rows [][]string
	var err error
	func() {
		mu.RLock()
		defer mu.RUnlock()
		rows, err = e.GetFilteredPolicy(1, orgID.String())
	}()
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// UserGroupings returns a user's g-rows ([user, role, org]; superadmin
// g2 rows are 2-field and never touched by membership sync).
func UserGroupings(e *casbin.Enforcer, userID uuid.UUID) ([][]string, error) {
	var rows [][]string
	var err error
	func() {
		mu.RLock()
		defer mu.RUnlock()
		rows, err = e.GetFilteredGroupingPolicy(0, userID.String())
	}()
	if err != nil {
		return nil, err
	}
	return rows, nil
}
