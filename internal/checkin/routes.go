package checkin

import (
	"github.com/casbin/casbin/v3"
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/checkin/handler"
	"github.com/nikhea/rallya/internal/iam"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	orgrepo "github.com/nikhea/rallya/internal/organization/repository"
)

// RegisterRoutes wires check-in endpoints: auth -> org membership ->
// Casbin gates (checkin:create for scans, checkin:read for stats).
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
	orgRepo *orgrepo.OrgRepository,
	e *casbin.Enforcer,
) {
	door := api.Group("/orgs/:id/events/:eventId/checkin")
	door.Use(authhandler.RequireAuth(authRepo))
	door.Use(orghandler.RequireOrgContext(orgRepo))
	{
		door.POST("", iam.RequirePermission(e, iam.ObjCheckin, iam.ActCreate), h.Scan)
		door.POST("/batch", iam.RequirePermission(e, iam.ObjCheckin, iam.ActCreate), h.ScanBatch)
		door.POST("/revert", iam.RequirePermission(e, iam.ObjCheckin, iam.ActUpdate), h.Revert)
		door.GET("/stats", iam.RequirePermission(e, iam.ObjCheckin, iam.ActRead), h.Stats)
	}
}
