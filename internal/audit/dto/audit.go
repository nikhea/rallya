// Package auditdto holds the Audit domain wire shapes.
package auditdto

// AuditEvent is one recorded action.
type AuditEvent struct {
	ID         string  `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	OrgID      *string `json:"orgId,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
	ActorID    *string `json:"actorId,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
	Action     string  `json:"action" example:"order.confirmed"`
	ObjectType string  `json:"objectType" example:"order"`
	ObjectID   *string `json:"objectId,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
	Before     *string `json:"before,omitempty" example:"{\"status\":\"PENDING_PAYMENT\"}"`
	After      *string `json:"after,omitempty" example:"{\"status\":\"CONFIRMED\"}"`
	CreatedAt  string  `json:"createdAt" example:"2026-09-20T10:00:00Z"`
}

// AuditPage is the paginated envelope.
type AuditPage struct {
	Items   []AuditEvent `json:"items"`
	Total   int64        `json:"total" example:"42"`
	Page    int          `json:"page" example:"1"`
	PerPage int          `json:"perPage" example:"20"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"organization not found"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse
