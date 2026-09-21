package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	"github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/service"
)

// Handler adapts OrgService to Gin. No business logic here.
type Handler struct {
	svc *service.OrgService
}

// NewHandler builds the handler.
func NewHandler(svc *service.OrgService) *Handler { return &Handler{svc: svc} }

// CreateOrg POST /api/v1/orgs (caller becomes OWNER).
//
// @Summary		Create organization
// @Description	Creates a tenant org; the caller becomes OWNER.
// @Tags			orgs
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			request	body		orgdto.CreateOrg	true	"Org payload"
// @Success		201		{object}	orgdto.Org
// @Failure		400		{object}	orgdto.ErrorAlias	"Validation error"
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		409		{object}	orgdto.ErrorAlias	"Slug taken"
// @Router			/orgs [post]
func (h *Handler) CreateOrg(c *gin.Context) {
	uid, ok := authhandler.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var in orgdto.CreateOrg
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	detail, err := h.svc.CreateOrg(uid, in.Name, strVal(in.Slug), strVal(in.Logo))
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, detail)
}

// ListMyOrgs GET /api/v1/orgs.
//
// @Summary		List my organizations
// @Description	Orgs the caller belongs to, with roles. Used for org switching and GET /auth/me bootstrap.
// @Tags			orgs
// @Produce		json
// @Security		BearerAuth
// @Success		200	{array}		orgdto.Org
// @Failure		401		{object}	orgdto.ErrorAlias
// @Router			/orgs [get]
func (h *Handler) ListMyOrgs(c *gin.Context) {
	uid, ok := authhandler.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	memberships, err := h.svc.ListMemberships(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	out := make([]orgdto.Org, 0, len(memberships))
	for _, m := range memberships {
		out = append(out, orgdto.Org{ID: m.ID, Slug: m.Slug, Name: m.Name, Role: m.Role, CreatedAt: m.JoinedAt})
	}
	c.JSON(http.StatusOK, out)
}

// GetOrg GET /api/v1/orgs/:id (MEMBER+).
//
// @Summary		Get organization
// @Description	Org details; :id accepts UUID or slug. Non-members get 404.
// @Tags			orgs
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Org UUID or slug"	example(acme)
// @Success		200	{object}	orgdto.Org
// @Failure		401	{object}	orgdto.ErrorAlias
// @Failure		404	{object}	orgdto.ErrorAlias
// @Router			/orgs/{id} [get]
func (h *Handler) GetOrg(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	detail, err := h.svc.GetOrg(uid, OrgRef(c))
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// UpdateOrg PATCH /api/v1/orgs/:id (ADMIN+).
//
// @Summary		Update organization
// @Description	Rename / re-slug / logo. Slug conflicts return 409.
// @Tags			orgs
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string			true	"Org UUID or slug"
// @Param			request	body		orgdto.UpdateOrg	true	"Fields to update"
// @Success		200		{object}	orgdto.Org
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Failure		409		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id} [patch]
func (h *Handler) UpdateOrg(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in orgdto.UpdateOrg
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	detail, err := h.svc.UpdateOrg(uid, OrgRef(c), strVal(in.Name), strVal(in.Slug), strVal(in.Logo))
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// DeleteOrg DELETE /api/v1/orgs/:id (OWNER).
//
// @Summary		Delete organization
// @Description	Hard delete; memberships and invites cascade.
// @Tags			orgs
// @Produce		json
// @Security		BearerAuth
// @Param			id	path	string	true	"Org UUID or slug"
// @Success		200	{object}	orgdto.MessageAlias
// @Failure		401	{object}	orgdto.ErrorAlias
// @Failure		403	{object}	orgdto.ErrorAlias
// @Failure		404	{object}	orgdto.ErrorAlias
// @Router			/orgs/{id} [delete]
func (h *Handler) DeleteOrg(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	if err := h.svc.DeleteOrg(uid, OrgRef(c)); err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Organization deleted"})
}

// ListMembers GET /api/v1/orgs/:id/members (MEMBER+).
//
// @Summary		List members
// @Description	Paginated memberships with identity display data.
// @Tags			orgs
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	orgdto.MembersPage
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/members [get]
func (h *Handler) ListMembers(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	limit, offset := page(c)
	members, total, err := h.svc.ListMembers(uid, OrgRef(c), limit, offset)
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, orgdto.MembersPage{Items: members, Total: total})
}

// AddMember POST /api/v1/orgs/:id/members (ADMIN+, role within grantor's).
//
// @Summary		Add member directly
// @Description	Adds a registered user; use invites for non-registered emails.
// @Tags			orgs
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string			true	"Org UUID or slug"
// @Param			request	body		orgdto.AddMember	true	"Member payload"
// @Success		201		{object}	orgdto.Member
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Failure		409		{object}	orgdto.ErrorAlias	"Already a member"
// @Router			/orgs/{id}/members [post]
func (h *Handler) AddMember(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in orgdto.AddMember
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := h.svc.AddMember(uid, OrgRef(c), in.Email, model.MemberRole(orDefault(in.Role, "MEMBER")))
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, m)
}

// UpdateMemberRole PATCH /api/v1/orgs/:id/members/:userId (OWNER, last-owner guard).
//
// @Summary		Change member role
// @Description	Demoting the last OWNER is rejected.
// @Tags			orgs
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			userId	path		string					true	"Member user UUID"
// @Param			request	body		orgdto.UpdateMemberRole	true	"Role payload"
// @Success		200		{object}	orgdto.Member
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Failure		409		{object}	orgdto.ErrorAlias	"Last owner"
// @Router			/orgs/{id}/members/{userId} [patch]
func (h *Handler) UpdateMemberRole(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	target, err := uuid.Parse(strings.TrimSpace(c.Param("userId")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var in orgdto.UpdateMemberRole
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := h.svc.UpdateMemberRole(uid, OrgRef(c), target, model.MemberRole(in.Role))
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

// RemoveMember DELETE /api/v1/orgs/:id/members/:userId (MEMBER+ route,
// service rules: ADMIN+ may remove MEMBER/ADMIN; only OWNER removes OWNER;
// self-leave allowed except last OWNER).
//
// @Summary		Remove member or leave
// @Description	ADMIN+ may remove MEMBER/ADMIN; only OWNER removes OWNER. Self-leave allowed except last OWNER.
// @Tags			orgs
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			userId	path		string	true	"Member user UUID"
// @Success		200		{object}	orgdto.MessageAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Failure		409		{object}	orgdto.ErrorAlias	"Last owner"
// @Router			/orgs/{id}/members/{userId} [delete]
func (h *Handler) RemoveMember(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	target, err := uuid.Parse(strings.TrimSpace(c.Param("userId")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	if err := h.svc.RemoveMember(uid, OrgRef(c), target); err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Member removed"})
}

// InviteMember POST /api/v1/orgs/:id/invites (ADMIN+).
//
// @Summary		Invite by email
// @Description	Creates a 7d invite and queues the invite email. Supersedes pending invites for the email.
// @Tags			orgs
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string				true	"Org UUID or slug"
// @Param			request	body		orgdto.InviteMember	true	"Invite payload"
// @Success		201		{object}	orgdto.MessageAlias	"Invite sent"
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Failure		409		{object}	orgdto.ErrorAlias	"Already a member"
// @Router			/orgs/{id}/invites [post]
func (h *Handler) InviteMember(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in orgdto.InviteMember
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.InviteMember(uid, OrgRef(c), in.Email, model.MemberRole(orDefault(in.Role, "MEMBER"))); err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Invite sent"})
}

// ListInvites GET /api/v1/orgs/:id/invites (ADMIN+).
//
// @Summary		List pending invites
// @Tags			orgs
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	orgdto.InvitesPage
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/invites [get]
func (h *Handler) ListInvites(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	limit, offset := page(c)
	invites, total, err := h.svc.ListInvites(uid, OrgRef(c), limit, offset)
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, orgdto.InvitesPage{Items: invites, Total: total})
}

// RevokeInvite DELETE /api/v1/orgs/:id/invites/:inviteId (ADMIN+).
//
// @Summary		Revoke invite
// @Tags			orgs
// @Produce		json
// @Security		BearerAuth
// @Param			id			path		string	true	"Org UUID or slug"
// @Param			inviteId	path		string	true	"Invite UUID"
// @Success		200			{object}	orgdto.MessageAlias
// @Failure		401			{object}	orgdto.ErrorAlias
// @Failure		403			{object}	orgdto.ErrorAlias
// @Failure		404			{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/invites/{inviteId} [delete]
func (h *Handler) RevokeInvite(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	inviteID, err := uuid.Parse(strings.TrimSpace(c.Param("inviteId")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid invite id"})
		return
	}
	if err := h.svc.RevokeInvite(uid, OrgRef(c), inviteID); err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Invite revoked"})
}

// AcceptInvite POST /api/v1/orgs/invites/accept.
//
// @Summary		Accept invite
// @Description	Caller account email must match the invite. Creates the membership; idempotent for members.
// @Tags			orgs
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			request	body		orgdto.AcceptInvite	true	"Invite token"
// @Success		200		{object}	orgdto.Org
// @Failure		400		{object}	orgdto.ErrorAlias	"Invalid/expired or email mismatch"
// @Failure		401		{object}	orgdto.ErrorAlias
// @Router			/orgs/invites/accept [post]
func (h *Handler) AcceptInvite(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in orgdto.AcceptInvite
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	detail, err := h.svc.AcceptInvite(uid, in.Token)
	if err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// DeclineInvite POST /api/v1/orgs/invites/decline ("reject" path).
//
// @Summary		Decline invite
// @Description	Rejects a pending invite addressed to the caller. Idempotent; accepted invites cannot be declined.
// @Tags			orgs
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			request	body		orgdto.DeclineInvite	true	"Invite token"
// @Success		200		{object}	orgdto.MessageAlias	"Invite declined"
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		409		{object}	orgdto.ErrorAlias	"Already accepted"
// @Router			/orgs/invites/decline [post]
func (h *Handler) DeclineInvite(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in orgdto.DeclineInvite
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.DeclineInvite(uid, in.Token); err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Invite declined"})
}

// UpdateMyPreferences PATCH /api/v1/orgs/:id/members/me (self, MEMBER+).
//
// @Summary		Update my announcement preference
// @Description	Opt in or out of publish announcement emails for this org.
// @Tags			orgs
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string							true	"Org UUID or slug"
// @Param			request	body		orgdto.UpdateMyPreferences	true	"Preference payload"
// @Success		200		{object}	orgdto.MessageAlias	"Preferences updated"
// @Failure		400		{object}	orgdto.ErrorAlias
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		404		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/members/me [patch]
func (h *Handler) UpdateMyPreferences(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in orgdto.UpdateMyPreferences
	if err := c.ShouldBindJSON(&in); err != nil || in.NotifyEvents == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "notifyEvents is required"})
		return
	}
	if err := h.svc.UpdateMyPreferences(uid, OrgRef(c), *in.NotifyEvents); err != nil {
		c.JSON(orgErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Preferences updated"})
}

// ---------- mapping + status ----------

func orgErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrOrgNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrForbidden),
		errors.Is(err, service.ErrInviteEmailMismatch):
		return http.StatusForbidden
	case errors.Is(err, service.ErrSlugTaken),
		errors.Is(err, service.ErrAlreadyMember),
		errors.Is(err, service.ErrLastOwner),
		errors.Is(err, service.ErrInviteConsumed),
		errors.Is(err, service.ErrRoleExists),
		errors.Is(err, service.ErrRoleAssigned),
		errors.Is(err, service.ErrRoleCapReached):
		return http.StatusConflict
	case errors.Is(err, service.ErrInvalidSlug),
		errors.Is(err, service.ErrInvalidName),
		errors.Is(err, service.ErrUserNotFound),
		errors.Is(err, service.ErrInvalidInvite),
		errors.Is(err, service.ErrInvalidRoleName),
		errors.Is(err, service.ErrRoleReserved),
		errors.Is(err, service.ErrRoleNotFound),
		errors.Is(err, service.ErrInvalidRolePermissions):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func page(c *gin.Context) (limit, offset int) {
	limit = 20
	if n, err := strconv.Atoi(c.Query("perPage")); err == nil && n >= 1 && n <= 100 {
		limit = n
	}
	if n, err := strconv.Atoi(c.Query("page")); err == nil && n >= 1 {
		offset = (n - 1) * limit
	}
	return limit, offset
}
