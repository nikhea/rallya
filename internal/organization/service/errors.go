package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrOrgNotFound            = errors.New("organization not found")
	ErrForbidden              = errors.New("insufficient role")
	ErrSlugTaken              = errors.New("slug already taken")
	ErrInvalidSlug            = errors.New("invalid slug (lowercase letters, numbers, hyphens)")
	ErrInvalidName            = errors.New("name is required")
	ErrAlreadyMember          = errors.New("user is already a member")
	ErrUserNotFound           = errors.New("user not found (invite them by email instead)")
	ErrLastOwner              = errors.New("organization must keep at least one owner")
	ErrInvalidInvite          = errors.New("invalid or expired invite")
	ErrInviteConsumed         = errors.New("invite already used")
	ErrInviteEmailMismatch    = errors.New("invite was sent to a different email")
	ErrInvalidRoleName        = errors.New("invalid role name (lowercase letters, numbers, hyphens)")
	ErrRoleReserved           = errors.New("role name is reserved")
	ErrRoleExists             = errors.New("role already exists")
	ErrRoleNotFound           = errors.New("role not found")
	ErrRoleCapReached         = errors.New("too many custom roles (max 20)")
	ErrRoleAssigned           = errors.New("role is still assigned (unassign all holders first)")
	ErrInvalidRolePermissions = errors.New("permissions must be known matrix pairs (grants only)")
	ErrApiKeyNotFound         = errors.New("api key not found")
	ErrInvalidApiKeyName      = errors.New("name is required (max 100 chars)")
	ErrInvalidApiKeyScope     = errors.New("scopes must be known object:action pairs")
	ErrInvalidApiKeyExpiry    = errors.New("expiresAt must be in the future")
)
