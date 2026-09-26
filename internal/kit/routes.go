package kit

import (
	"github.com/casbin/casbin/v3"
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/iam"
	"github.com/nikhea/rallya/internal/kit/handler"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	orgrepo "github.com/nikhea/rallya/internal/organization/repository"
)

// RegisterRoutes wires kit endpoints: auth -> org membership ->
// Casbin gates (kit:create/update/delete for writes, kit:read for reads).
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
	orgRepo *orgrepo.OrgRepository,
	e *casbin.Enforcer,
) {
	kits := api.Group("/orgs/:id/events/:eventId/kits")
	kits.Use(authhandler.RequireAuth(authRepo))
	kits.Use(orghandler.RequireOrgContext(orgRepo))
	{
		kits.POST("", iam.RequirePermission(e, iam.ObjKit, iam.ActCreate), h.CreateKit)
		kits.GET("", iam.RequirePermission(e, iam.ObjKit, iam.ActRead), h.ListKits)
		kits.PATCH("/:kitId", iam.RequirePermission(e, iam.ObjKit, iam.ActUpdate), h.UpdateKit)
		kits.DELETE("/:kitId", iam.RequirePermission(e, iam.ObjKit, iam.ActDelete), h.DeleteKit)
		kits.POST("/:kitId/collect", iam.RequirePermission(e, iam.ObjKit, iam.ActUpdate), h.Collect)
		kits.GET("/:kitId/collections", iam.RequirePermission(e, iam.ObjKit, iam.ActRead), h.ListKitCollections)
	}

	collections := api.Group("/orgs/:id/events/:eventId/kit-collections")
	collections.Use(authhandler.RequireAuth(authRepo))
	collections.Use(orghandler.RequireOrgContext(orgRepo))
	{
		collections.GET("", iam.RequirePermission(e, iam.ObjKit, iam.ActRead), h.ListCollections)
		collections.POST("/:collectionId/collect", iam.RequirePermission(e, iam.ObjKit, iam.ActUpdate), h.MarkCollected)
		collections.POST("/:collectionId/void", iam.RequirePermission(e, iam.ObjKit, iam.ActUpdate), h.Void)
	}
}
