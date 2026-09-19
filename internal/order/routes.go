package order

import (
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/order/handler"
)

// RegisterRoutes wires order endpoints. Purchase routes are authenticated
// but need no org membership (public buyers with accounts); admin support
// paths resolve org rights in service.
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
) {
	events := api.Group("/events/:id/orders")
	events.Use(authhandler.RequireAuth(authRepo))
	{
		events.POST("", h.CreateOrder)
	}

	mine := api.Group("/orders")
	mine.Use(authhandler.RequireAuth(authRepo))
	{
		mine.GET("/mine", h.ListMyOrders)
		mine.GET("/:id", h.GetOrder)
		mine.POST("/:id/cancel", h.CancelOrder)
	}
}
