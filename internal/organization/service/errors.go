package service

import "errors"

// Sentinel errors map to HTTP statuses in handler.
var (
	ErrOrgNotFound         = errors.New("organization not found")
	ErrForbidden           = errors.New("insufficient role")
	ErrSlugTaken           = errors.New("slug already taken")
	ErrInvalidSlug         = errors.New("invalid slug (lowercase letters, numbers, hyphens)")
	ErrInvalidName         = errors.New("name is required")
	ErrAlreadyMember       = errors.New("user is already a member")
	ErrUserNotFound        = errors.New("user not found (invite them by email instead)")
	ErrLastOwner           = errors.New("organization must keep at least one owner")
	ErrInvalidInvite       = errors.New("invalid or expired invite")
	ErrInviteConsumed      = errors.New("invite already used")
	ErrInviteEmailMismatch = errors.New("invite was sent to a different email")
)
