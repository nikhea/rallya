package eventdto

// CreateEvent is POST /api/v1/orgs/:id/events.
type CreateEvent struct {
	Title       string  `json:"title" binding:"required,min=1,max=255" example:"Summer Fest 2026"`
	Slug        *string `json:"slug" binding:"omitempty,max=150" example:"summer-fest-2026"`
	Description *string `json:"description" binding:"omitempty,max=10000" example:"Three days of music downtown."`
	Venue       *string `json:"venue" binding:"omitempty,max=255" example:"Central Park Amphitheatre"`
	Location    *string `json:"location" binding:"omitempty,max=255" example:"Lagos, Nigeria"`
	StartsAt    *string `json:"startsAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00" example:"2026-08-01T10:00:00Z"`
	EndsAt      *string `json:"endsAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00" example:"2026-08-03T22:00:00Z"`
	Capacity    *int    `json:"capacity" binding:"omitempty,min=1" example:"500"`
}

// UpdateEvent is PATCH /api/v1/orgs/:id/events/:eventId.
type UpdateEvent struct {
	Title       *string `json:"title" binding:"omitempty,min=1,max=255" example:"Summer Fest 2026"`
	Slug        *string `json:"slug" binding:"omitempty,max=150" example:"summer-fest"`
	Description *string `json:"description" binding:"omitempty,max=10000"`
	Venue       *string `json:"venue" binding:"omitempty,max=255"`
	Location    *string `json:"location" binding:"omitempty,max=255"`
	StartsAt    *string `json:"startsAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	EndsAt      *string `json:"endsAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	Capacity    *int    `json:"capacity" binding:"omitempty,min=1"`
	ClearCover  *bool   `json:"clearCover" example:"false"`
}

// EventFilter scopes listings (query params).
type EventFilter struct {
	OrganizationID *string `form:"-"`
	Status         *string `form:"status" example:"PUBLISHED"`
	From           *string `form:"from" example:"2026-01-01T00:00:00Z"`
	To             *string `form:"to" example:"2026-12-31T23:59:59Z"`
	Query          string  `form:"q" example:"fest"`
	Sort           string  `form:"sort" example:"starts"`
}

// Event is the public event shape.
type Event struct {
	ID          string  `json:"id" example:"4d5e6f70-8192-0a1b-2c3d-4e5f6a7b8c9d"`
	Title       string  `json:"title" example:"Summer Fest 2026"`
	Slug        string  `json:"slug" example:"summer-fest-2026"`
	Description *string `json:"description,omitempty"`
	Venue       *string `json:"venue,omitempty"`
	Location    *string `json:"location,omitempty"`
	StartsAt    *string `json:"startsAt,omitempty" example:"2026-08-01T10:00:00Z"`
	EndsAt      *string `json:"endsAt,omitempty" example:"2026-08-03T22:00:00Z"`
	Capacity    *int    `json:"capacity,omitempty" example:"500"`
	CoverURL    *string `json:"coverUrl,omitempty" example:"/uploads/events/acme/x7f2k9.jpg"`
	Status      string  `json:"status" example:"PUBLISHED"`
	CreatedAt   string  `json:"createdAt" example:"2026-09-19T12:00:00Z"`
	UpdatedAt   string  `json:"updatedAt" example:"2026-09-19T12:00:00Z"`
}

// EventsPage is a paginated envelope.
type EventsPage struct {
	Items []Event `json:"items"`
	Total int64   `json:"total" example:"2"`
}

// Message is a generic ack response.
type Message struct {
	Message string `json:"message" example:"Event published"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"event not found"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse

// MessageAlias names Message for swagger annotations.
type MessageAlias = Message
