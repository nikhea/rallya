package paymentdto

// CheckoutResponse is POST /api/v1/orders/:id/checkout.
type CheckoutResponse struct {
	URL       string `json:"url" example:"https://checkout.stripe.com/pay/cs_test_123"`
	SessionID string `json:"sessionId" example:"cs_test_123"`
}

// Message is a generic ack response.
type Message struct {
	Message string `json:"message" example:"ok"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"payment required"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse

// MessageAlias names Message for swagger annotations.
type MessageAlias = Message
