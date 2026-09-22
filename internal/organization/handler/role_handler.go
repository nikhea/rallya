package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	"github.com/nikhea/rallya/internal/organization/service"
)

// ListRoles GET /api/v1/orgs/:id/roles (ADMIN+).
//
// @Summary		List custom roles
// @Description	Definitions with permission sets and holder counts.
// @Tags			roles
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Org UUID or slug"
// @Success		200	{object}	[]orgdto.CustomRole
// @Failure		401	{object}	orgdto.ErrorAlias
// @Failure		403	{object}	orgdto.ErrorAlias
// @Failure		404	{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/roles [get]
func (h *Handler) ListRoles(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	roles, err := h.svc.ListRoles(uid, OrgRef(c))
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, roles)
}

// DefineRole POST /api/v1/orgs/:id/roles (OWNER).
//
// @Summary		Define a custom role
// @Description	Grants-only permission set, materialized post-commit. Audited.
// @Tags			roles
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string				true	"Org UUID or slug"
// @Param			request	body		orgdto.DefineRole	true	"Role payload"
// @Success		201		{object}	orgdto.CustomRole
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Failure		409		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/roles [post]
func (h *Handler) DefineRole(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in orgdto.DefineRole
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	perms := make([]service.RolePermission, 0, len(in.Permissions))
	for _, p := range in.Permissions {
		perms = append(perms, service.RolePermission{Object: p.Object, Action: p.Action})
	}
	r, err := h.svc.DefineRole(uid, OrgRef(c), in.Name, perms)
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, r)
}

// UpdateRole PATCH /api/v1/orgs/:id/roles/:role (OWNER).
//
// @Summary		Replace a role's permission set
// @Description	Assignee groupings stand; grants change underneath.
// @Tags			roles
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string							true	"Org UUID or slug"
// @Param			role	path		string							true	"Role name"
// @Param			request	body		orgdto.UpdateRolePermissions	true	"Permission set"
// @Success		200		{object}	orgdto.CustomRole
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/roles/{role} [patch]
func (h *Handler) UpdateRole(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in orgdto.UpdateRolePermissions
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	perms := make([]service.RolePermission, 0, len(in.Permissions))
	for _, p := range in.Permissions {
		perms = append(perms, service.RolePermission{Object: p.Object, Action: p.Action})
	}
	r, err := h.svc.UpdateRole(uid, OrgRef(c), c.Param("role"), perms)
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, r)
}

// DeleteRole DELETE /api/v1/orgs/:id/roles/:role (OWNER).
//
// @Summary		Delete a custom role
// @Description	409 while holders remain — unassign first.
// @Tags			roles
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			role	path		string	true	"Role name"
// @Success		200		{object}	orgdto.MessageAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Failure		409		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/roles/{role} [delete]
func (h *Handler) DeleteRole(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	if err := h.svc.DeleteRole(uid, OrgRef(c), c.Param("role")); err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Role deleted"})
}

// AssignRole POST /api/v1/orgs/:id/roles/:role/assign (OWNER).
//
// @Summary		Grant a custom role to a member
// @Description	Idempotent; member must exist.
// @Tags			roles
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			role	path		string					true	"Role name"
// @Param			request	body		orgdto.RoleAssignment	true	"Target member"
// @Success		200		{object}	orgdto.MessageAlias
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/roles/{role}/assign [post]
func (h *Handler) AssignRole(c *gin.Context) {
	h.assignFlow(c, true)
}

// UnassignRole POST /api/v1/orgs/:id/roles/:role/unassign (OWNER).
//
// @Summary		Revoke a custom role from a member
// @Description	Idempotent.
// @Tags			roles
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			role	path		string					true	"Role name"
// @Param			request	body		orgdto.RoleAssignment	true	"Target member"
// @Success		200		{object}	orgdto.MessageAlias
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/roles/{role}/unassign [post]
func (h *Handler) UnassignRole(c *gin.Context) {
	h.assignFlow(c, false)
}

func (h *Handler) assignFlow(c *gin.Context, grant bool) {
	uid, _ := authhandler.UserFromContext(c)
	var in orgdto.RoleAssignment
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	target, err := uuid.Parse(strings.TrimSpace(in.UserID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var opErr error
	if grant {
		opErr = h.svc.AssignRole(uid, OrgRef(c), target, c.Param("role"))
	} else {
		opErr = h.svc.UnassignRole(uid, OrgRef(c), target, c.Param("role"))
	}
	if opErr != nil {
		c.JSON(orgErrorStatus(opErr), gin.H{"error": opErr.Error()})
		return
	}
	msg := "Role revoked"
	if grant {
		msg = "Role granted"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
}
