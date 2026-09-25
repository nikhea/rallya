package orgdto

import "github.com/google/uuid"

// CreateOrg is POST /api/v1/orgs.
type CreateOrg struct {
	Name string  `json:"name" binding:"required,min=1,max=255" example:"Acme Inc"`
	Slug *string `json:"slug" binding:"omitempty,max=100" example:"acme"`
	Logo *string `json:"logo" binding:"omitempty,url,max=2048" example:"https://example.com/logo.png"`
}

// UpdateOrg is PATCH /api/v1/orgs/:id.
type UpdateOrg struct {
	Name *string `json:"name" binding:"omitempty,min=1,max=255" example:"Acme Corp"`
	Slug *string `json:"slug" binding:"omitempty,max=100" example:"acme"`
	Logo *string `json:"logo" binding:"omitempty,max=2048" example:"https://example.com/logo2.png"`
}

// AddMember is POST /api/v1/orgs/:id/members.
type AddMember struct {
	Email string `json:"email" binding:"required,email,max=255" example:"jane@test.com"`
	Role  string `json:"role" binding:"omitempty,oneof=OWNER ADMIN MEMBER" example:"MEMBER"`
}

// UpdateMemberRole is PATCH /api/v1/orgs/:id/members/:userId.
type UpdateMemberRole struct {
	Role string `json:"role" binding:"required,oneof=OWNER ADMIN MEMBER" example:"ADMIN"`
}

// InviteMember is POST /api/v1/orgs/:id/invites.
type InviteMember struct {
	Email string `json:"email" binding:"required,email,max=255" example:"jane@test.com"`
	Role  string `json:"role" binding:"omitempty,oneof=OWNER ADMIN MEMBER" example:"MEMBER"`
}

// AcceptInvite is POST /api/v1/orgs/invites/accept.
type AcceptInvite struct {
	Token string `json:"token" binding:"required,min=1" example:"d9e4f3a2b1c04852e5f6a708192b3c4d5e6f8091a2b3c4d5e6f708192b3"`
}

// DeclineInvite is POST /api/v1/orgs/invites/decline ("reject" path).
type DeclineInvite struct {
	Token string `json:"token" binding:"required,min=1" example:"d9e4f3a2b1c04852e5f6a708192b3c4d5e6f8091a2b3c4d5e6f708192b3"`
}

// Org is the public org shape.
type Org struct {
	ID        string  `json:"id" example:"2b3c4d5e-6f70-8a9b-0c1d-2e3f4a5b6c7d"`
	Name      string  `json:"name" example:"Acme Inc"`
	Slug      string  `json:"slug" example:"acme"`
	LogoURL   *string `json:"logoUrl,omitempty" example:"https://example.com/logo.png"`
	Role      string  `json:"role,omitempty" example:"OWNER"`
	CreatedAt string  `json:"createdAt" example:"2026-09-19T12:00:00Z"`
}

// Member is the public membership shape.
type Member struct {
	UserID        string `json:"userId" example:"1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"`
	Email         string `json:"email" example:"jane@test.com"`
	Name          string `json:"name" example:"Jane"`
	EmailVerified bool   `json:"emailVerified" example:"true"`
	Role          string `json:"role" example:"MEMBER"`
	JoinedAt      string `json:"joinedAt" example:"2026-09-19T12:00:00Z"`
}

// RolePermission is one grant in a custom role.
type RolePermission struct {
	Object string `json:"object" example:"checkin"`
	Action string `json:"action" example:"create"`
}

// CustomRole is the public custom-role shape.
type CustomRole struct {
	ID          string           `json:"id" example:"1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"`
	Name        string           `json:"name" example:"door"`
	Permissions []RolePermission `json:"permissions"`
	Holders     int64            `json:"holders" example:"3"`
	CreatedAt   string           `json:"createdAt" example:"2026-09-19T12:00:00Z"`
}

// DefineRole is POST /api/v1/orgs/:id/roles.
type DefineRole struct {
	Name        string           `json:"name" binding:"required" example:"door"`
	Permissions []RolePermission `json:"permissions" binding:"required,min=1"`
}

// UpdateRolePermissions is PATCH /api/v1/orgs/:id/roles/:role.
type UpdateRolePermissions struct {
	Permissions []RolePermission `json:"permissions" binding:"required,min=1"`
}

// RoleAssignment carries the target member for assign/unassign.
type RoleAssignment struct {
	UserID string `json:"userId" binding:"required,uuid" example:"1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"`
}

// Invite is the public pending-invite shape (no token hash).
type Invite struct {
	ID        string `json:"id" example:"3c4d5e6f-7081-9a0b-1c2d-3e4f5a6b7c8d"`
	Email     string `json:"email" example:"jane@test.com"`
	Role      string `json:"role" example:"MEMBER"`
	ExpiresAt string `json:"expiresAt" example:"2026-09-26T12:00:00Z"`
	CreatedAt string `json:"createdAt" example:"2026-09-19T12:00:00Z"`
}

// CreateApiKey is POST /api/v1/orgs/:id/api-keys.
type CreateApiKey struct {
	Name      string   `json:"name" binding:"required,min=1,max=100" example:"door-tablet-1"`
	Scopes    []string `json:"scopes" binding:"omitempty,dive,min=1,max=64" example:"checkin:create"`
	ExpiresAt *string  `json:"expiresAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00" example:"2027-01-01T00:00:00Z"`
}

// ApiKey is the public key shape (hash/raw never exposed).
type ApiKey struct {
	ID         string   `json:"id" example:"3c4d5e6f-7081-9a0b-1c2d-3e4f5a6b7c8d"`
	Name       string   `json:"name" example:"door-tablet-1"`
	Prefix     string   `json:"prefix" example:"rk_live_ab12cd34"`
	Scopes     []string `json:"scopes" example:"checkin:create"`
	ExpiresAt  *string  `json:"expiresAt,omitempty" example:"2027-01-01T00:00:00Z"`
	LastUsedAt *string  `json:"lastUsedAt,omitempty" example:"2026-09-20T12:00:00Z"`
	RevokedAt  *string  `json:"revokedAt,omitempty" example:"2026-09-21T12:00:00Z"`
	CreatedBy  string   `json:"createdBy" example:"1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"`
	CreatedAt  string   `json:"createdAt" example:"2026-09-19T12:00:00Z"`
}

// ApiKeyCreated returns the raw secret once (never stored, never re-read).
type ApiKeyCreated struct {
	ApiKey
	Key string `json:"key" example:"rk_live_abc123..."`
}

// ApiKeysPage lists keys with a total.
type ApiKeysPage struct {
	Items []ApiKey `json:"items"`
	Total int64    `json:"total" example:"2"`
}

// MemberNotify is one announcement recipient (internal read shape).
type MemberNotify struct {
	UserID uuid.UUID
	Email  string
	Name   string
}

// UpdateMyPreferences is PATCH /api/v1/orgs/:id/members/me.
type UpdateMyPreferences struct {
	NotifyEvents *bool `json:"notifyEvents" example:"true"`
}

// Page is a paginated envelope.
type Page struct {
	Items any   `json:"items"`
	Total int64 `json:"total" example:"2"`
}

// MembersPage lists members with a total.
type MembersPage struct {
	Items []Member `json:"items"`
	Total int64    `json:"total" example:"2"`
}

// InvitesPage lists pending invites with a total.
type InvitesPage struct {
	Items []Invite `json:"items"`
	Total int64    `json:"total" example:"1"`
}

// Message is a generic ack response.
type Message struct {
	Message string `json:"message" example:"Invite sent"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"insufficient role"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse

// MessageAlias names Message for swagger annotations.
type MessageAlias = Message
