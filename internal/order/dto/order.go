package orderdto

// CreateOrder is POST /api/v1/events/:id/orders.
type CreateOrder struct {
	TicketTypeID   string  `json:"ticketTypeId" binding:"required,uuid" example:"6f708192-0314-2b3c-4d5e-6f708192a3b4"`
	Quantity       int     `json:"quantity" binding:"required,min=1" example:"2"`
	IdempotencyKey *string `json:"idempotencyKey" binding:"omitempty,max=100" example:"order-req-001"`
}

// Order is the public order shape.
type Order struct {
	ID           string  `json:"id" example:"7a8192a3-1425-3c4d-5e6f-708192a3b4c5"`
	EventID      string  `json:"eventId" example:"4d5e6f70-8192-0a1b-2c3d-4e5f6a7b8c9d"`
	TicketTypeID string  `json:"ticketTypeId" example:"6f708192-0314-2b3c-4d5e-6f708192a3b4"`
	Quantity     int     `json:"quantity" example:"2"`
	PriceCents   int     `json:"priceCents" example:"5000"`
	Currency     string  `json:"currency" example:"USD"`
	Status       string  `json:"status" example:"CONFIRMED"`
	ExpiresAt    *string `json:"expiresAt,omitempty" example:"2026-09-19T12:15:00Z"`
	CreatedAt    string  `json:"createdAt" example:"2026-09-19T12:00:00Z"`
}

// OrdersPage lists a user's orders.
type OrdersPage struct {
	Items []Order `json:"items"`
	Total int64   `json:"total" example:"3"`
}

// Message is a generic ack response.
type Message struct {
	Message string `json:"message" example:"Order cancelled"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"sold out"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse

// MessageAlias names Message for swagger annotations.
type MessageAlias = Message
