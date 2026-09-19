package service

import (
	"github.com/google/uuid"
)

// PolicySeeder installs/removes per-org Casbin policies. Declared here
// (consumer side); iam implements it. Nil-safe: skipped when unwired.
type PolicySeeder interface {
	SeedOrgPolicies(orgID uuid.UUID) error
	RemoveOrgPolicies(orgID uuid.UUID)
}
