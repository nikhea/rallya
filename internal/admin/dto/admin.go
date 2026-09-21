// Package admindto holds the Admin domain wire shapes (platform reads).
package admindto

// OrgSummary is one row of the platform org inventory.
type OrgSummary struct {
	ID        string `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Name      string `json:"name" example:"Acme"`
	Slug      string `json:"slug" example:"acme"`
	Members   int64  `json:"members" example:"12"`
	CreatedAt string `json:"createdAt" example:"2026-09-20T10:00:00Z"`
}

// OrgsPage lists orgs.
type OrgsPage struct {
	Items   []OrgSummary `json:"items"`
	Total   int64        `json:"total" example:"3"`
	Page    int          `json:"page" example:"1"`
	PerPage int          `json:"perPage" example:"20"`
}

// MemberRow is one roster row with identity (platform view).
type MemberRow struct {
	UserID        string `json:"userId" example:"550e8400-e29b-41d4-a716-446655440000"`
	Email         string `json:"email" example:"jane@test.com"`
	Name          string `json:"name" example:"Jane"`
	Role          string `json:"role" example:"ADMIN"`
	EmailVerified bool   `json:"emailVerified" example:"true"`
	JoinedAt      string `json:"joinedAt" example:"2026-09-20T10:00:00Z"`
}

// OrgDetail is the dispute-triage view of one tenant.
type OrgDetail struct {
	ID        string      `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Name      string      `json:"name" example:"Acme"`
	Slug      string      `json:"slug" example:"acme"`
	Members   []MemberRow `json:"members"`
	CreatedAt string      `json:"createdAt" example:"2026-09-20T10:00:00Z"`
}

// UserSummary is one row of platform account lookup.
type UserSummary struct {
	ID            string `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Email         string `json:"email" example:"jane@test.com"`
	Name          string `json:"name" example:"Jane"`
	EmailVerified bool   `json:"emailVerified" example:"true"`
	SuperAdmin    bool   `json:"superAdmin" example:"false"`
	CreatedAt     string `json:"createdAt" example:"2026-09-20T10:00:00Z"`
}

// UsersPage lists accounts.
type UsersPage struct {
	Items   []UserSummary `json:"items"`
	Total   int64         `json:"total" example:"7"`
	Page    int           `json:"page" example:"1"`
	PerPage int           `json:"perPage" example:"20"`
}

// MembershipRow is one tenant membership of a user.
type MembershipRow struct {
	OrgID    string `json:"orgId" example:"550e8400-e29b-41d4-a716-446655440000"`
	OrgName  string `json:"orgName" example:"Acme"`
	OrgSlug  string `json:"orgSlug" example:"acme"`
	Role     string `json:"role" example:"ADMIN"`
	JoinedAt string `json:"joinedAt" example:"2026-09-20T10:00:00Z"`
}

// UserDetail is the access-review view of one account.
type UserDetail struct {
	UserSummary
	Memberships []MembershipRow `json:"memberships"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"organization not found"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse
