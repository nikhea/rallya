package organization

import (
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/organization/handler"
	"github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/repository"
)

// RegisterRoutes wires the Organization HTTP endpoints. All routes require
// auth; :id-scoped routes additionally require org membership with role
// guards. Finer rules (last-owner, grant hierarchy) live in service.
//
//	POST /api/v1/orgs                                    (auth)
//	GET  /api/v1/orgs                                    (auth)
//	GET  /api/v1/orgs/:id               (MEMBER+)
//	PATCH /api/v1/orgs/:id              (ADMIN+)
//	DELETE /api/v1/orgs/:id             (OWNER)
//	GET  /api/v1/orgs/:id/members       (MEMBER+)
//	POST /api/v1/orgs/:id/members       (ADMIN+)
//	PATCH /api/v1/orgs/:id/members/:userId  (OWNER)
//	DELETE /api/v1/orgs/:id/members/:userId (MEMBER+; service enforces hierarchy)
//	POST /api/v1/orgs/:id/invites       (ADMIN+)
//	GET  /api/v1/orgs/:id/invites       (ADMIN+)
//	DELETE /api/v1/orgs/:id/invites/:inviteId (ADMIN+)
//	POST /api/v1/orgs/invites/accept    (auth)
//	POST /api/v1/orgs/invites/decline   (auth)
//
// :id accepts an org UUID or slug.
func RegisterRoutes(g *gin.RouterGroup, h *handler.Handler, repo *repository.OrgRepository, authRepo *authrepo.AuthRepository) {
	g.Use(authhandler.RequireAuth(authRepo))

	g.POST("", h.CreateOrg)
	g.GET("", h.ListMyOrgs)
	g.POST("/invites/accept", h.AcceptInvite)
	g.POST("/invites/decline", h.DeclineInvite)

	scoped := g.Group("/:id")
	scoped.Use(handler.RequireOrgContext(repo))
	{
		scoped.GET("", h.GetOrg)
		scoped.GET("/members", h.ListMembers)
		// RemoveMember lives at member level: the service allows self-leave
		// for any role and restricts removing others by hierarchy.
		scoped.DELETE("/members/:userId", h.RemoveMember)

		admin := scoped.Group("")
		admin.Use(handler.RequireRole(model.MemberRoleAdmin))
		{
			admin.PATCH("", h.UpdateOrg)
			admin.POST("/members", h.AddMember)
			admin.POST("/invites", h.InviteMember)
			admin.GET("/invites", h.ListInvites)
			admin.DELETE("/invites/:inviteId", h.RevokeInvite)
		}

		owner := scoped.Group("")
		owner.Use(handler.RequireRole(model.MemberRoleOwner))
		{
			owner.DELETE("", h.DeleteOrg)
			owner.PATCH("/members/:userId", h.UpdateMemberRole)
		}
	}
}
