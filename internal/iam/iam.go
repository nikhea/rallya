// Package iam is the enforcement reader for authorization.
//
// Casbin RBAC with domains: the domain is the organization UUID, roles are
// the fixed OWNER/ADMIN/MEMBER ladder plus a domain-less superadmin.
// Organization is the sole writer (memberships -> groupings via GroupSyncer,
// policy seeds on org create); iam only reads at request time through
// RequirePermission / RequireSuperAdmin middleware.
package iam

import (
	_ "embed"
)

//go:embed model.conf
var modelText string

// ModelText returns the embedded Casbin model.
func ModelText() string { return modelText }
