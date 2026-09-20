package attendeedto

// AddAttendee is POST /api/v1/orgs/:id/events/:eventId/attendees.
type AddAttendee struct {
	Email string  `json:"email" binding:"required,email,max=255" example:"walkin@test.com"`
	Name  *string `json:"name" binding:"omitempty,max=255" example:"Walk In"`
}

// CorrectAttendee is PATCH .../attendees/:attendeeId.
type CorrectAttendee struct {
	Name  *string `json:"name" binding:"omitempty,max=255" example:"Jane Doe"`
	Email *string `json:"email" binding:"omitempty,email,max=255" example:"jane@test.com"`
}

// Attendee is the public attendee shape. QRPayload appears only where the
// caller is entitled to scan it (owner views, roster); never public.
type Attendee struct {
	ID          string  `json:"id" example:"8a8192a3-1425-3c4d-5e6f-708192a3b4c5"`
	OrderID     *string `json:"orderId,omitempty" example:"7a8192a3-1425-3c4d-5e6f-708192a3b4c5"`
	UnitIndex   int     `json:"unitIndex" example:"0"`
	EventID     string  `json:"eventId" example:"4d5e6f70-8192-0a1b-2c3d-4e5f6a7b8c9d"`
	UserID      *string `json:"userId,omitempty"`
	Email       string  `json:"email" example:"jane@test.com"`
	Name        *string `json:"name,omitempty" example:"Jane Doe"`
	Status      string  `json:"status" example:"REGISTERED"`
	QRPayload   *string `json:"qrPayload,omitempty" example:"8a8192a3….hmac"`
	CheckedInAt *string `json:"checkedInAt,omitempty"`
	CreatedAt   string  `json:"createdAt" example:"2026-09-19T12:00:00Z"`
}

// AttendeesPage lists rows.
type AttendeesPage struct {
	Items []Attendee `json:"items"`
	Total int64      `json:"total" example:"2"`
}

// Message is a generic ack response.
type Message struct {
	Message string `json:"message" example:"Attendee cancelled"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"attendee not found"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse

// MessageAlias names Message for swagger annotations.
type MessageAlias = Message
