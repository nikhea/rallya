package audit

import (
	"github.com/casbin/casbin/v3"
	"github.com/gin-gonic/gin"

	"github.com/nikhea/rallya/internal/audit/handler"
	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/iam"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	orgrepo "github.com/nikhea/rallya/internal/organization/repository"
)

// RegisterRoutes wires audit reads: org-scoped for ADMIN+ (audit:read) and
// the first platform read for superadmins (accountability backbone for the
// future /admin/* surface).
func RegisterRoutes(
	api *gin.RouterGroup,
	h *handler.Handler,
	authRepo *authrepo.AuthRepository,
	orgRepo *orgrepo.OrgRepository,
	e *casbin.Enforcer,
) {
	org := api.Group("/orgs/:id/audit")
	org.Use(authhandler.RequireAuth(authRepo))
	org.Use(orghandler.RequireOrgContext(orgRepo))
	{
		org.GET("", iam.RequirePermission(e, iam.ObjAudit, iam.ActRead), h.ListOrg)
	}

	admin := api.Group("/admin/audit")
	admin.Use(authhandler.RequireAuth(authRepo))
	admin.Use(iam.RequireSuperAdmin(e))
	{
		admin.GET("", h.ListPlatform)
	}
}
