package payment

import (
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/payment/handler"
)

// RegisterRoutes wires payment endpoints. Checkout requires auth (owner);
// the webhook is public and authenticates via Stripe signature instead.
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
) {
	orders := api.Group("/orders/:id/checkout")
	orders.Use(authhandler.RequireAuth(authRepo))
	{
		orders.POST("", h.CreateCheckout)
	}

	webhooks := api.Group("/webhooks")
	{
		webhooks.POST("/stripe", h.StripeWebhook)
	}
}
