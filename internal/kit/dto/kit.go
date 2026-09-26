// Package kitdto holds the Kit domain wire shapes.
package kitdto

// CreateKitRequest defines one named kit type for an event.
type CreateKitRequest struct {
	Name          string `json:"name" example:"VIP pack"`
	Description   string `json:"description,omitempty" example:"Lanyard, shirt, stickers"`
	QuantityTotal int    `json:"quantityTotal" example:"100"`
}

// UpdateKitRequest patches a kit type. QuantityTotal cannot drop below the
// already-collected count (422); omit fields to leave them unchanged.
type UpdateKitRequest struct {
	Name          *string `json:"name,omitempty" example:"VIP pack"`
	Description   *string `json:"description,omitempty"`
	QuantityTotal *int    `json:"quantityTotal,omitempty" example:"120"`
}

// KitResponse is one kit type with live handout counts.
type KitResponse struct {
	ID            string `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	EventID       string `json:"eventId" example:"550e8400-e29b-41d4-a716-446655440000"`
	Name          string `json:"name" example:"VIP pack"`
	Description   string `json:"description,omitempty"`
	QuantityTotal int    `json:"quantityTotal" example:"100"`
	Pending       int64  `json:"pending" example:"5"`
	Collected     int64  `json:"collected" example:"40"`
	Voided        int64  `json:"voided" example:"1"`
	Remaining     int64  `json:"remaining" example:"55"`
	CreatedAt     string `json:"createdAt" example:"2026-09-20T10:00:00Z"`
}

// CollectRequest hands a kit to an attendee. Default collects immediately;
// Reserve holds a PENDING unit for later pickup. IdempotencyKey makes
// tablet retries safe (same key returns the existing record).
type CollectRequest struct {
	AttendeeID     string `json:"attendeeId" example:"550e8400-e29b-41d4-a716-446655440000"`
	Reserve        bool   `json:"reserve,omitempty" example:"false"`
	IdempotencyKey string `json:"idempotencyKey,omitempty" example:"kit-req-001"`
}

// CollectionResponse is one handout record.
type CollectionResponse struct {
	ID          string  `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	KitID       string  `json:"kitId" example:"550e8400-e29b-41d4-a716-446655440000"`
	KitName     string  `json:"kitName,omitempty" example:"VIP pack"`
	EventID     string  `json:"eventId" example:"550e8400-e29b-41d4-a716-446655440000"`
	AttendeeID  string  `json:"attendeeId" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status      string  `json:"status" example:"COLLECTED"`
	CollectedAt *string `json:"collectedAt,omitempty" example:"2026-09-20T10:00:00Z"`
	CollectedBy *string `json:"collectedBy,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
	CreatedAt   string  `json:"createdAt" example:"2026-09-20T10:00:00Z"`
}

// CollectionListResponse pages handout records.
type CollectionListResponse struct {
	Items []CollectionResponse `json:"items"`
	Total int64                `json:"total" example:"40"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"kit not found"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse
