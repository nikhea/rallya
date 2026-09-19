package ticketdto

// CreateTicket is POST /api/v1/orgs/:id/events/:eventId/tickets.
type CreateTicket struct {
	Name          string  `json:"name" binding:"required,min=1,max=255" example:"General Admission"`
	Description   *string `json:"description" binding:"omitempty,max=2000" example:"Standing floor access."`
	PriceCents    int     `json:"priceCents" binding:"min=0" example:"2500"`
	Currency      *string `json:"currency" binding:"omitempty,len=3" example:"USD"`
	QuantityTotal int     `json:"quantityTotal" binding:"required,min=1" example:"200"`
	MaxPerOrder   *int    `json:"maxPerOrder" binding:"omitempty,min=1" example:"4"`
	SaleStartsAt  *string `json:"saleStartsAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00" example:"2026-07-01T00:00:00Z"`
	SaleEndsAt    *string `json:"saleEndsAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00" example:"2026-07-31T23:59:59Z"`
}

// UpdateTicket is PATCH .../tickets/:ticketId (nil = unchanged).
type UpdateTicket struct {
	Name          *string `json:"name" binding:"omitempty,min=1,max=255"`
	Description   *string `json:"description" binding:"omitempty,max=2000"`
	PriceCents    *int    `json:"priceCents" binding:"omitempty,min=0"`
	Currency      *string `json:"currency" binding:"omitempty,len=3"`
	QuantityTotal *int    `json:"quantityTotal" binding:"omitempty,min=1"`
	MaxPerOrder   *int    `json:"maxPerOrder" binding:"omitempty,min=1"`
	SaleStartsAt  *string `json:"saleStartsAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	SaleEndsAt    *string `json:"saleEndsAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
}

// TicketType is the public ticket shape with live availability.
type TicketType struct {
	ID                string  `json:"id" example:"6f708192-0314-2b3c-4d5e-6f708192a3b4"`
	Name              string  `json:"name" example:"General Admission"`
	Description       *string `json:"description,omitempty"`
	PriceCents        int     `json:"priceCents" example:"2500"`
	Currency          string  `json:"currency" example:"USD"`
	QuantityTotal     int     `json:"quantityTotal" example:"200"`
	QuantitySold      int     `json:"quantitySold" example:"37"`
	Remaining         int     `json:"remaining" example:"163"`
	ForSale           bool    `json:"forSale" example:"true"`
	UnavailableReason *string `json:"unavailableReason,omitempty" example:"sold_out"`
	MaxPerOrder       *int    `json:"maxPerOrder,omitempty" example:"4"`
	SaleStartsAt      *string `json:"saleStartsAt,omitempty" example:"2026-07-01T00:00:00Z"`
	SaleEndsAt        *string `json:"saleEndsAt,omitempty" example:"2026-07-31T23:59:59Z"`
	Status            string  `json:"status" example:"ACTIVE"`
	SoldOut           bool    `json:"soldOut" example:"false"`
	CreatedAt         string  `json:"createdAt" example:"2026-09-19T12:00:00Z"`
}

// TicketsPage lists types.
type TicketsPage struct {
	Items []TicketType `json:"items"`
	Total int64        `json:"total" example:"2"`
}

// Message is a generic ack response.
type Message struct {
	Message string `json:"message" example:"Ticket type deleted"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"sold out"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse

// MessageAlias names Message for swagger annotations.
type MessageAlias = Message
