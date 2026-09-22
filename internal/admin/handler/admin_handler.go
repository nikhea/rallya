package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	admindto "github.com/nikhea/rallya/internal/admin/dto"
	"github.com/nikhea/rallya/internal/admin/service"
	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	authmodel "github.com/nikhea/rallya/internal/auth/model"
	orderdto "github.com/nikhea/rallya/internal/order/dto"
	orgutils "github.com/nikhea/rallya/internal/organization/utils"
)

// Handler adapts AdminService to Gin. No business logic here. Every call
// passes the superadmin's ID so the service can audit the read.
type Handler struct {
	svc *service.AdminService
}

// NewHandler builds the handler.
func NewHandler(svc *service.AdminService) *Handler { return &Handler{svc: svc} }

func actorOf(c *gin.Context) (uuid.UUID, bool) {
	return authhandler.UserFromContext(c)
}

func pageOf(c *gin.Context) (page, perPage int, ok bool) {
	page, perPage = 1, 20
	if raw := c.Query("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return 0, 0, false
		}
		page = n
	}
	if raw := c.Query("perPage"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return 0, 0, false
		}
		perPage = n
	}
	return page, perPage, true
}

// ListOrgs GET /api/v1/admin/orgs (superadmin).
//
// @Summary		Platform org inventory
// @Description	Newest first with member counts. Audited per call.
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			q		query		string	false	"Name/slug search"
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	admindto.OrgsPage
// @Failure		401		{object}	admindto.ErrorAlias
// @Failure		403		{object}	admindto.ErrorAlias
// @Router			/admin/orgs [get]
func (h *Handler) ListOrgs(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, perPage, ok := pageOf(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pagination"})
		return
	}
	rows, total, err := h.svc.ListOrgs(actor, c.Query("q"), perPage, (page-1)*perPage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items := make([]admindto.OrgSummary, 0, len(rows))
	for i := range rows {
		items = append(items, admindto.OrgSummary{
			ID: rows[i].Org.ID.String(), Name: rows[i].Org.Name, Slug: rows[i].Org.Slug,
			Members:   rows[i].Members,
			CreatedAt: rows[i].Org.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	c.JSON(http.StatusOK, admindto.OrgsPage{Items: items, Total: total, Page: page, PerPage: perPage})
}

// GetOrg GET /api/v1/admin/orgs/:id (superadmin).
//
// @Summary		Tenant dispute-triage view
// @Description	Org row with full roster (identity + roles).
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Org UUID"
// @Success		200	{object}	admindto.OrgDetail
// @Failure		400	{object}	admindto.ErrorAlias
// @Failure		401	{object}	admindto.ErrorAlias
// @Failure		403	{object}	admindto.ErrorAlias
// @Failure		404	{object}	admindto.ErrorAlias
// @Router			/admin/orgs/{id} [get]
func (h *Handler) GetOrg(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}
	d, err := h.svc.GetOrg(actor, orgID)
	if err != nil {
		c.JSON(adminErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	members := make([]admindto.MemberRow, 0, len(d.Members))
	for i := range d.Members {
		m := &d.Members[i]
		members = append(members, admindto.MemberRow{
			UserID: m.User.ID.String(), Email: m.User.Email, Name: orgutils.DisplayNameOf(&m.User),
			Role: string(m.Role), EmailVerified: m.User.EmailVerified,
			JoinedAt: m.JoinedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	c.JSON(http.StatusOK, admindto.OrgDetail{
		ID: d.Org.ID.String(), Name: d.Org.Name, Slug: d.Org.Slug, Members: members,
		CreatedAt: d.Org.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// SearchUsers GET /api/v1/admin/users (superadmin).
//
// @Summary		Platform account lookup
// @Description	Email-fragment search with superadmin flags.
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			q		query		string	false	"Email fragment"
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	admindto.UsersPage
// @Failure		401		{object}	admindto.ErrorAlias
// @Failure		403		{object}	admindto.ErrorAlias
// @Router			/admin/users [get]
func (h *Handler) SearchUsers(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, perPage, ok := pageOf(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pagination"})
		return
	}
	rows, total, err := h.svc.SearchUsers(actor, c.Query("q"), perPage, (page-1)*perPage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items := make([]admindto.UserSummary, 0, len(rows))
	for i := range rows {
		items = append(items, toUserSummary(&rows[i].User, rows[i].SuperAdmin))
	}
	c.JSON(http.StatusOK, admindto.UsersPage{Items: items, Total: total, Page: page, PerPage: perPage})
}

// GetUser GET /api/v1/admin/users/:id (superadmin).
//
// @Summary		Account access-review view
// @Description	Profile plus every tenant membership.
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"User UUID"
// @Success		200	{object}	admindto.UserDetail
// @Failure		400	{object}	admindto.ErrorAlias
// @Failure		401	{object}	admindto.ErrorAlias
// @Failure		403	{object}	admindto.ErrorAlias
// @Failure		404	{object}	admindto.ErrorAlias
// @Router			/admin/users/{id} [get]
func (h *Handler) GetUser(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	d, err := h.svc.GetUser(actor, userID)
	if err != nil {
		c.JSON(adminErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	memberships := make([]admindto.MembershipRow, 0, len(d.Memberships))
	for i := range d.Memberships {
		m := &d.Memberships[i]
		memberships = append(memberships, admindto.MembershipRow{
			OrgID: m.Org.ID.String(), OrgName: m.Org.Name, OrgSlug: m.Org.Slug,
			Role:     string(m.Role),
			JoinedAt: m.JoinedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	c.JSON(http.StatusOK, admindto.UserDetail{
		UserSummary: toUserSummary(&d.User, d.SuperAdmin), Memberships: memberships,
	})
}

// ListUserOrders GET /api/v1/admin/users/:id/orders (superadmin).
//
// @Summary		Cross-tenant order history
// @Description	orderdto shapes (already Stripe-free). Fraud/refund investigation.
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"User UUID"
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	orderdto.OrdersPage
// @Failure		400		{object}	admindto.ErrorAlias
// @Failure		401		{object}	admindto.ErrorAlias
// @Failure		403		{object}	admindto.ErrorAlias
// @Failure		404		{object}	admindto.ErrorAlias
// @Router			/admin/users/{id}/orders [get]
func (h *Handler) ListUserOrders(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	page, perPage, ok := pageOf(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pagination"})
		return
	}
	orders, total, err := h.svc.ListUserOrders(actor, userID, perPage, (page-1)*perPage)
	if err != nil {
		c.JSON(adminErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, orderdto.OrdersPage{Items: orders, Total: total})
}

// ReseedOrgPolicies POST /api/v1/admin/orgs/:id/policies/reseed (superadmin).
//
// @Summary		Repair org Casbin rows
// @Description	Idempotent re-seed; returns what changed. Audited.
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Org UUID"
// @Success		200	{object}	admindto.PolicyDiff
// @Failure		400	{object}	admindto.ErrorAlias
// @Failure		401	{object}	admindto.ErrorAlias
// @Failure		403	{object}	admindto.ErrorAlias
// @Failure		404	{object}	admindto.ErrorAlias
// @Router			/admin/orgs/{id}/policies/reseed [post]
func (h *Handler) ReseedOrgPolicies(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}
	diff, err := h.svc.ReseedOrgPolicies(actor, orgID)
	if err != nil {
		c.JSON(adminErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, admindto.PolicyDiff{Added: diff.Added, Removed: diff.Removed, Total: diff.Total})
}

// SyncUserPolicies POST /api/v1/admin/users/:id/policies/sync (superadmin).
//
// @Summary		Converge user groupings to memberships
// @Description	Sweep-then-add per org; stale orgs swept; g2 untouched. Audited.
// @Tags			admin
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"User UUID"
// @Success		200	{object}	admindto.PolicyDiff
// @Failure		400	{object}	admindto.ErrorAlias
// @Failure		401	{object}	admindto.ErrorAlias
// @Failure		403	{object}	admindto.ErrorAlias
// @Failure		404	{object}	admindto.ErrorAlias
// @Router			/admin/users/{id}/policies/sync [post]
func (h *Handler) SyncUserPolicies(c *gin.Context) {
	actor, ok := actorOf(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	diff, err := h.svc.SyncUserPolicies(actor, userID)
	if err != nil {
		c.JSON(adminErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, admindto.PolicyDiff{Added: diff.Added, Removed: diff.Removed, Total: diff.Total})
}

func toUserSummary(u *authmodel.User, superAdmin bool) admindto.UserSummary {
	return admindto.UserSummary{
		ID: u.ID.String(), Email: u.Email, Name: orgutils.DisplayNameOf(u),
		EmailVerified: u.EmailVerified, SuperAdmin: superAdmin,
		CreatedAt: u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func adminErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrOrgNotFound),
		errors.Is(err, service.ErrUserNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}
