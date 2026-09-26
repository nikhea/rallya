package subscription

import (
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	orgrepo "github.com/nikhea/rallya/internal/organization/repository"
	"github.com/nikhea/rallya/internal/subscription/handler"
)

// RegisterRoutes wires subscription endpoints. The catalog is public;
// org billing reads need membership (stealth 404 for outsiders) and
// mutations additionally enforce OWNER inside the service (403).
// No Casbin object: billing is not a role grant.
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
	orgRepo *orgrepo.OrgRepository,
) {
	api.GET("/subscription/plans", h.GetPlans)

	org := api.Group("/orgs/:id/subscription")
	org.Use(authhandler.RequireAuth(authRepo))
	org.Use(orghandler.RequireOrgContext(orgRepo))
	{
		org.GET("", h.GetSubscription)
		org.POST("/checkout", h.StartCheckout)
		org.POST("/portal", h.CreatePortalSession)
	}
}
