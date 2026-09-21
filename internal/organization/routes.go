package organization

import (
	"github.com/casbin/casbin/v3"
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/iam"
	"github.com/nikhea/rallya/internal/organization/handler"
	"github.com/nikhea/rallya/internal/organization/repository"
)

// RegisterRoutes wires the Organization HTTP endpoints. All routes require
// auth; :id-scoped routes require org membership (stealth 404s) plus Casbin
// permission gates. Finer rules (last-owner, grant hierarchy) live in service.
// DELETE member stays at membership level: the service allows self-leave for
// any role while restricting removals of others by hierarchy.
//
//	POST /api/v1/orgs                                    (auth)
//	GET  /api/v1/orgs                                    (auth)
//	GET  /api/v1/orgs/:id               (org,read)
//	PATCH /api/v1/orgs/:id              (org,update)
//	DELETE /api/v1/orgs/:id             (org,delete)
//	GET  /api/v1/orgs/:id/members       (member,read)
//	POST /api/v1/orgs/:id/members       (member,create)
//	PATCH /api/v1/orgs/:id/members/:userId  (member,update)
//	DELETE /api/v1/orgs/:id/members/:userId (service rules)
//	PATCH /api/v1/orgs/:id/members/me (self preferences)
//	POST /api/v1/orgs/:id/invites       (invite,create)
//	GET  /api/v1/orgs/:id/invites       (invite,read)
//	DELETE /api/v1/orgs/:id/invites/:inviteId (invite,delete)
//	POST /api/v1/orgs/invites/accept    (auth)
//	POST /api/v1/orgs/invites/decline   (auth)
//	GET  /api/v1/orgs/:id/roles        (role,read)
//	POST /api/v1/orgs/:id/roles        (role,create — OWNER-only: no matrix
//	                                     row grants it, only the wildcard passes)
//	PATCH /api/v1/orgs/:id/roles/:role (role,update — OWNER-only, same trick)
//	DELETE /api/v1/orgs/:id/roles/:role (role,delete — OWNER-only, same trick)
//	POST /api/v1/orgs/:id/roles/:role/assign   (role,update — OWNER-only)
//	POST /api/v1/orgs/:id/roles/:role/unassign (role,update — OWNER-only)
//
// :id accepts an org UUID or slug.
func RegisterRoutes(g *gin.RouterGroup, h *handler.Handler, repo *repository.OrgRepository, authRepo *authrepo.AuthRepository, e *casbin.Enforcer) {
	g.Use(authhandler.RequireAuth(authRepo))

	g.POST("", h.CreateOrg)
	g.GET("", h.ListMyOrgs)
	g.POST("/invites/accept", h.AcceptInvite)
	g.POST("/invites/decline", h.DeclineInvite)

	scoped := g.Group("/:id")
	scoped.Use(handler.RequireOrgContext(repo))
	{
		scoped.GET("", iam.RequirePermission(e, iam.ObjOrg, iam.ActRead), h.GetOrg)
		scoped.GET("/members", iam.RequirePermission(e, iam.ObjMember, iam.ActRead), h.ListMembers)
		scoped.DELETE("/members/:userId", h.RemoveMember)
		scoped.PATCH("/members/me", h.UpdateMyPreferences)

		scoped.PATCH("", iam.RequirePermission(e, iam.ObjOrg, iam.ActUpdate), h.UpdateOrg)
		scoped.POST("/members", iam.RequirePermission(e, iam.ObjMember, iam.ActCreate), h.AddMember)
		scoped.PATCH("/members/:userId", iam.RequirePermission(e, iam.ObjMember, iam.ActUpdate), h.UpdateMemberRole)

		scoped.POST("/invites", iam.RequirePermission(e, iam.ObjInvite, iam.ActCreate), h.InviteMember)
		scoped.GET("/invites", iam.RequirePermission(e, iam.ObjInvite, iam.ActRead), h.ListInvites)
		scoped.DELETE("/invites/:inviteId", iam.RequirePermission(e, iam.ObjInvite, iam.ActDelete), h.RevokeInvite)

		scoped.GET("/roles", iam.RequirePermission(e, iam.ObjRole, iam.ActRead), h.ListRoles)
		scoped.POST("/roles", iam.RequirePermission(e, iam.ObjRole, iam.ActCreate), h.DefineRole)
		scoped.PATCH("/roles/:role", iam.RequirePermission(e, iam.ObjRole, iam.ActUpdate), h.UpdateRole)
		scoped.DELETE("/roles/:role", iam.RequirePermission(e, iam.ObjRole, iam.ActDelete), h.DeleteRole)
		scoped.POST("/roles/:role/assign", iam.RequirePermission(e, iam.ObjRole, iam.ActUpdate), h.AssignRole)
		scoped.POST("/roles/:role/unassign", iam.RequirePermission(e, iam.ObjRole, iam.ActUpdate), h.UnassignRole)

		scoped.DELETE("", iam.RequirePermission(e, iam.ObjOrg, iam.ActDelete), h.DeleteOrg)
	}
}
