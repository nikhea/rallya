package event

import (
	"github.com/casbin/casbin/v3"
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/event/handler"
	"github.com/nikhea/rallya/internal/iam"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	orgrepo "github.com/nikhea/rallya/internal/organization/repository"
)

// RegisterRoutes wires public discovery plus org-scoped management.
// Public routes need no auth. Org routes chain auth -> org membership
// (stealth 404s) -> Casbin gates; finer rules live in service.
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
	orgRepo *orgrepo.OrgRepository,
	e *casbin.Enforcer,
) {
	public := api.Group("/events")
	{
		public.GET("", h.ListPublicEvents)
		public.GET("/:id", h.GetPublicEvent)
	}

	orgs := api.Group("/orgs/:id/events")
	orgs.Use(authhandler.RequireAuth(authRepo))
	orgs.Use(orghandler.RequireOrgContext(orgRepo))
	{
		orgs.GET("", iam.RequirePermission(e, iam.ObjEvent, iam.ActRead), h.ListOrgEvents)
		orgs.POST("", iam.RequirePermission(e, iam.ObjEvent, iam.ActCreate), h.CreateEvent)
		orgs.GET("/:eventId", iam.RequirePermission(e, iam.ObjEvent, iam.ActRead), h.GetOrgEvent)
		orgs.PATCH("/:eventId", iam.RequirePermission(e, iam.ObjEvent, iam.ActUpdate), h.UpdateEvent)
		orgs.DELETE("/:eventId", iam.RequirePermission(e, iam.ObjEvent, iam.ActDelete), h.DeleteEvent)
		orgs.POST("/:eventId/publish", iam.RequirePermission(e, iam.ObjEvent, iam.ActPublish), h.PublishEvent)
		orgs.POST("/:eventId/unpublish", iam.RequirePermission(e, iam.ObjEvent, iam.ActPublish), h.UnpublishEvent)
		orgs.POST("/:eventId/cancel", iam.RequirePermission(e, iam.ObjEvent, iam.ActPublish), h.CancelEvent)
		orgs.POST("/:eventId/cover", iam.RequirePermission(e, iam.ObjEvent, iam.ActUpdate), h.UploadCover)
		orgs.POST("/:eventId/images", iam.RequirePermission(e, iam.ObjEvent, iam.ActUpdate), h.UploadGallery)
		orgs.GET("/:eventId/images", iam.RequirePermission(e, iam.ObjEvent, iam.ActRead), h.ListImages)
	}
}
