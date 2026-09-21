package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	auditdto "github.com/nikhea/rallya/internal/audit/dto"
	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	"github.com/nikhea/rallya/internal/audit/service"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
)

// Handler adapts AuditService to Gin. No business logic here.
type Handler struct {
	svc *service.AuditService
}

// NewHandler builds the handler.
func NewHandler(svc *service.AuditService) *Handler { return &Handler{svc: svc} }

// ListOrg GET /api/v1/orgs/:id/audit (ADMIN+).
//
// @Summary		List org audit events
// @Description	Tenant-scoped paper trail, newest first. Filters narrow disputes.
// @Tags			audit
// @Produce		json
// @Security		BearerAuth
// @Param			id			path		string	true	"Org UUID or slug"
// @Param			action		query		string	false	"Action filter"		example(order.confirmed)
// @Param			actor		query		string	false	"Actor UUID filter"
// @Param			objectType	query		string	false	"Object type filter"	example(order)
// @Param			objectId	query		string	false	"Object UUID filter"
// @Param			since		query		string	false	"RFC3339 lower bound"
// @Param			until		query		string	false	"RFC3339 upper bound"
// @Param			page		query		int		false	"Page (1-based)"		example(1)
// @Param			perPage		query		int		false	"Page size, max 100"	example(20)
// @Success		200			{object}	auditdto.AuditPage
// @Failure		401			{object}	auditdto.ErrorAlias
// @Failure		403			{object}	auditdto.ErrorAlias
// @Failure		404			{object}	auditdto.ErrorAlias
// @Router			/orgs/{id}/audit [get]
func (h *Handler) ListOrg(c *gin.Context) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	f, code, errMsg := parseFilter(c)
	if errMsg != "" {
		c.JSON(code, gin.H{"error": errMsg})
		return
	}
	f.OrgID = &orgID
	h.list(c, f)
}

// ListPlatform GET /api/v1/admin/audit (superadmin).
//
// @Summary		List platform audit events
// @Description	Cross-tenant trail for disputes and abuse. Superadmin only.
// @Tags			audit
// @Produce		json
// @Security		BearerAuth
// @Param			org			query		string	false	"Org UUID filter"
// @Param			action		query		string	false	"Action filter"
// @Param			actor		query		string	false	"Actor UUID filter"
// @Param			objectType	query		string	false	"Object type filter"
// @Param			objectId	query		string	false	"Object UUID filter"
// @Param			since		query		string	false	"RFC3339 lower bound"
// @Param			until		query		string	false	"RFC3339 upper bound"
// @Param			page		query		int		false	"Page (1-based)"		example(1)
// @Param			perPage		query		int		false	"Page size, max 100"	example(20)
// @Success		200			{object}	auditdto.AuditPage
// @Failure		401			{object}	auditdto.ErrorAlias
// @Failure		403			{object}	auditdto.ErrorAlias
// @Router			/admin/audit [get]
func (h *Handler) ListPlatform(c *gin.Context) {
	f, code, errMsg := parseFilter(c)
	if errMsg != "" {
		c.JSON(code, gin.H{"error": errMsg})
		return
	}
	if raw := c.Query("org"); raw != "" {
		orgID, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org filter"})
			return
		}
		f.OrgID = &orgID
	}
	h.list(c, f)
}

func (h *Handler) list(c *gin.Context, f service.ListFilter) {
	res, err := h.svc.List(f)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items := make([]auditdto.AuditEvent, 0, len(res.Items))
	for i := range res.Items {
		items = append(items, toAuditEvent(&res.Items[i]))
	}
	page, perPage := f.Page, f.PerPage
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	c.JSON(http.StatusOK, auditdto.AuditPage{Items: items, Total: res.Total, Page: page, PerPage: perPage})
}

func parseFilter(c *gin.Context) (service.ListFilter, int, string) {
	var f service.ListFilter
	f.Action = c.Query("action")
	f.ObjectType = c.Query("objectType")
	if raw := c.Query("actor"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return f, http.StatusBadRequest, "invalid actor filter"
		}
		f.ActorID = &id
	}
	if raw := c.Query("objectId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return f, http.StatusBadRequest, "invalid objectId filter"
		}
		f.ObjectID = &id
	}
	if raw := c.Query("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return f, http.StatusBadRequest, "invalid since filter"
		}
		f.Since = &t
	}
	if raw := c.Query("until"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return f, http.StatusBadRequest, "invalid until filter"
		}
		f.Until = &t
	}
	if raw := c.Query("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return f, http.StatusBadRequest, "invalid page filter"
		}
		f.Page = n
	}
	if raw := c.Query("perPage"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return f, http.StatusBadRequest, "invalid perPage filter"
		}
		f.PerPage = n
	}
	return f, 0, ""
}

func toAuditEvent(e *auditmodel.AuditEvent) auditdto.AuditEvent {
	out := auditdto.AuditEvent{
		ID: e.ID.String(), Action: e.Action, ObjectType: e.ObjectType,
		Before: e.Before, After: e.After,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
	}
	if e.OrgID != nil {
		s := e.OrgID.String()
		out.OrgID = &s
	}
	if e.ActorID != nil {
		s := e.ActorID.String()
		out.ActorID = &s
	}
	if e.ObjectID != nil {
		s := e.ObjectID.String()
		out.ObjectID = &s
	}
	return out
}
