package ticketing

import (
	"github.com/casbin/casbin/v3"
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/iam"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	orgrepo "github.com/nikhea/rallya/internal/organization/repository"
	"github.com/nikhea/rallya/internal/ticketing/handler"
)

// RegisterRoutes wires public ticket discovery plus org-scoped management.
// Chain: auth -> org membership (stealth 404s) -> Casbin gates.
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
	orgRepo *orgrepo.OrgRepository,
	e *casbin.Enforcer,
) {
	public := api.Group("/events/:id/tickets")
	{
		public.GET("", h.ListPublicTickets)
	}

	tickets := api.Group("/orgs/:id/events/:eventId/tickets")
	tickets.Use(authhandler.RequireAuth(authRepo))
	tickets.Use(orghandler.RequireOrgContext(orgRepo))
	{
		tickets.GET("", iam.RequirePermission(e, iam.ObjTicket, iam.ActRead), h.ListTickets)
		tickets.POST("", iam.RequirePermission(e, iam.ObjTicket, iam.ActCreate), h.CreateTicket)
		tickets.GET("/:ticketId", iam.RequirePermission(e, iam.ObjTicket, iam.ActRead), h.GetTicket)
		tickets.PATCH("/:ticketId", iam.RequirePermission(e, iam.ObjTicket, iam.ActUpdate), h.UpdateTicket)
		tickets.DELETE("/:ticketId", iam.RequirePermission(e, iam.ObjTicket, iam.ActDelete), h.DeleteTicket)
		tickets.POST("/:ticketId/activate", iam.RequirePermission(e, iam.ObjTicket, iam.ActUpdate), h.ActivateTicket)
		tickets.POST("/:ticketId/pause", iam.RequirePermission(e, iam.ObjTicket, iam.ActUpdate), h.PauseTicket)
	}
}
