package attendee

import (
	"github.com/casbin/casbin/v3"
	"github.com/gin-gonic/gin"

	"github.com/nikhea/rallya/internal/attendee/handler"
	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/iam"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	orgrepo "github.com/nikhea/rallya/internal/organization/repository"
)

// RegisterRoutes wires attendee endpoints. Owner reads are auth-only;
// roster management chains auth -> org membership -> Casbin gates.
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
	orgRepo *orgrepo.OrgRepository,
	e *casbin.Enforcer,
) {
	mine := api.Group("/attendees")
	mine.Use(authhandler.RequireAuth(authRepo))
	{
		mine.GET("/mine", h.ListMine)
		mine.GET("/:id", h.GetMine)
		mine.POST("/:id/cancel", h.CancelMine)
	}

	roster := api.Group("/orgs/:id/events/:eventId/attendees")
	roster.Use(authhandler.RequireAuth(authRepo))
	roster.Use(orghandler.RequireOrgContext(orgRepo))
	{
		roster.GET("", iam.RequirePermission(e, iam.ObjAttendee, iam.ActRead), h.ListRoster)
		roster.POST("", iam.RequirePermission(e, iam.ObjAttendee, iam.ActCreate), h.AddAttendee)
		roster.PATCH("/:attendeeId", iam.RequirePermission(e, iam.ObjAttendee, iam.ActUpdate), h.CorrectAttendee)
	}
}
