package admin

import (
	"github.com/casbin/casbin/v3"
	"github.com/gin-gonic/gin"

	"github.com/nikhea/rallya/internal/admin/handler"
	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/iam"
)

// RegisterRoutes wires the platform reads. Everything here is superadmin
// only — tenant gates never apply; the audit trail on each read is the
// control. Repair triggers land as a follow-up slice.
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
	e *casbin.Enforcer,
) {
	admin := api.Group("/admin")
	admin.Use(authhandler.RequireAuth(authRepo))
	admin.Use(iam.RequireSuperAdmin(e))
	{
		admin.GET("/orgs", h.ListOrgs)
		admin.GET("/orgs/:id", h.GetOrg)
		admin.GET("/users", h.SearchUsers)
		admin.GET("/users/:id", h.GetUser)
		admin.GET("/users/:id/orders", h.ListUserOrders)
	}
}
