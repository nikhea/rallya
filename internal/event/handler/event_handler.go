package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	eventdto "github.com/nikhea/rallya/internal/event/dto"
	"github.com/nikhea/rallya/internal/event/service"
)

// Handler adapts EventService to Gin. No business logic here.
type Handler struct {
	svc *service.EventService
}

// NewHandler builds the handler.
func NewHandler(svc *service.EventService) *Handler { return &Handler{svc: svc} }

// CreateEvent POST /api/v1/orgs/:id/events (ADMIN+: event:create).
//
// @Summary		Create event
// @Description	Creates a DRAFT event under the org; slug auto-uniquifies per org.
// @Tags			events
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			request	body		eventdto.CreateEvent	true	"Event payload"
// @Success		201		{object}	eventdto.Event
// @Failure		400		{object}	eventdto.ErrorAlias
// @Failure		401		{object}	eventdto.ErrorAlias
// @Failure		403		{object}	eventdto.ErrorAlias
// @Failure		404		{object}	eventdto.ErrorAlias
// @Router			/orgs/{id}/events [post]
func (h *Handler) CreateEvent(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in eventdto.CreateEvent
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	starts, err := parseTime(in.StartsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid startsAt"})
		return
	}
	ends, err := parseTime(in.EndsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid endsAt"})
		return
	}
	e, err := h.svc.CreateEvent(uid, orgRef(c), service.CreateInput{
		Title: in.Title, Slug: strVal(in.Slug),
		Description: in.Description, Venue: in.Venue, Location: in.Location,
		StartsAt: starts, EndsAt: ends, Capacity: in.Capacity,
	})
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, e)
}

// ListOrgEvents GET /api/v1/orgs/:id/events (MEMBER+: drafts included).
//
// @Summary		List org events
// @Description	Paginated, filterable; includes drafts for members.
// @Tags			events
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			status	query		string	false	"DRAFT/PUBLISHED/CANCELLED"	example(PUBLISHED)
// @Param			from	query		string	false	"RFC3339 start"	example(2026-01-01T00:00:00Z)
// @Param			to		query		string	false	"RFC3339 end"	example(2026-12-31T23:59:59Z)
// @Param			q		query		string	false	"Title search"	example(fest)
// @Param			sort	query		string	false	"starts|created"	example(starts)
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	eventdto.EventsPage
// @Failure		401		{object}	eventdto.ErrorAlias
// @Failure		403		{object}	eventdto.ErrorAlias
// @Failure		404		{object}	eventdto.ErrorAlias
// @Router			/orgs/{id}/events [get]
func (h *Handler) ListOrgEvents(c *gin.Context) {
	limit, offset := page(c)
	var f eventdto.EventFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, total, err := h.svc.ListOrgEvents(orgRef(c), f, limit, offset, f.Sort)
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, eventdto.EventsPage{Items: items, Total: total})
}

// GetOrgEvent GET /api/v1/orgs/:id/events/:eventId (MEMBER+; drafts ok).
//
// @Summary		Get org event
// @Description	Drafts require membership; :eventId accepts UUID or slug.
// @Tags			events
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Success		200		{object}	eventdto.Event
// @Failure		401		{object}	eventdto.ErrorAlias
// @Failure		403		{object}	eventdto.ErrorAlias
// @Failure		404		{object}	eventdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId} [get]
func (h *Handler) GetOrgEvent(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	e, err := h.svc.GetEvent(&uid, orgRef(c), c.Param("eventId"))
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// UpdateEvent PATCH /api/v1/orgs/:id/events/:eventId (ADMIN+: event:update).
//
// @Summary		Update event
// @Description	Field updates; status changes go through publish/unpublish/cancel.
// @Tags			events
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			eventId	path		string					true	"Event UUID or slug"
// @Param			request	body		eventdto.UpdateEvent	true	"Fields to update"
// @Success		200		{object}	eventdto.Event
// @Failure		400		{object}	eventdto.ErrorAlias
// @Failure		401		{object}	eventdto.ErrorAlias
// @Failure		403		{object}	eventdto.ErrorAlias
// @Failure		404		{object}	eventdto.ErrorAlias
// @Failure		409		{object}	eventdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId} [patch]
func (h *Handler) UpdateEvent(c *gin.Context) {
	var in eventdto.UpdateEvent
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	starts, err := parseTime(in.StartsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid startsAt"})
		return
	}
	ends, err := parseTime(in.EndsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid endsAt"})
		return
	}
	clearCover := in.ClearCover != nil && *in.ClearCover
	e, err := h.svc.UpdateEvent(orgRef(c), c.Param("eventId"), service.UpdateInput{
		Title: in.Title, Slug: in.Slug, Description: in.Description,
		Venue: in.Venue, Location: in.Location,
		StartsAt: starts, EndsAt: ends, Capacity: in.Capacity,
		ClearCover: clearCover,
	})
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// DeleteEvent DELETE /api/v1/orgs/:id/events/:eventId (OWNER: event:delete).
//
// @Summary		Delete event
// @Description	Hard delete with cover cleanup.
// @Tags			events
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Success		200		{object}	eventdto.MessageAlias
// @Failure		401		{object}	eventdto.ErrorAlias
// @Failure		403		{object}	eventdto.ErrorAlias
// @Failure		404		{object}	eventdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId} [delete]
func (h *Handler) DeleteEvent(c *gin.Context) {
	if err := h.svc.DeleteEvent(orgRef(c), c.Param("eventId")); err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Event deleted"})
}

// PublishEvent POST /api/v1/orgs/:id/events/:eventId/publish (ADMIN+: event:publish).
//
// @Summary		Publish event
// @Description	DRAFT → PUBLISHED; queues announcements to opted-in members.
// @Tags			events
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Success		200		{object}	eventdto.Event
// @Failure		400		{object}	eventdto.ErrorAlias
// @Failure		401		{object}	eventdto.ErrorAlias
// @Failure		403		{object}	eventdto.ErrorAlias
// @Failure		404		{object}	eventdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/publish [post]
func (h *Handler) PublishEvent(c *gin.Context) {
	e, err := h.svc.Publish(orgRef(c), c.Param("eventId"))
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// UnpublishEvent POST /api/v1/orgs/:id/events/:eventId/unpublish (ADMIN+: event:publish).
//
// @Summary		Unpublish event
// @Description	PUBLISHED → DRAFT.
// @Tags			events
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Success		200		{object}	eventdto.Event
// @Failure		400		{object}	eventdto.ErrorAlias
// @Failure		401		{object}	eventdto.ErrorAlias
// @Failure		403		{object}	eventdto.ErrorAlias
// @Failure		404		{object}	eventdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/unpublish [post]
func (h *Handler) UnpublishEvent(c *gin.Context) {
	e, err := h.svc.Unpublish(orgRef(c), c.Param("eventId"))
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// CancelEvent POST /api/v1/orgs/:id/events/:eventId/cancel (ADMIN+: event:publish).
//
// @Summary		Cancel event
// @Description	PUBLISHED → CANCELLED (terminal).
// @Tags			events
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Success		200		{object}	eventdto.Event
// @Failure		400		{object}	eventdto.ErrorAlias
// @Failure		401		{object}	eventdto.ErrorAlias
// @Failure		403		{object}	eventdto.ErrorAlias
// @Failure		404		{object}	eventdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/cancel [post]
func (h *Handler) CancelEvent(c *gin.Context) {
	e, err := h.svc.Cancel(orgRef(c), c.Param("eventId"))
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// UploadCover POST /api/v1/orgs/:id/events/:eventId/cover (ADMIN+: event:update).
//
// @Summary		Upload event cover
// @Description	Multipart file (jpeg/png/webp, 5MB max, content-sniffed). Replaces existing cover.
// @Tags			events
// @Accept			multipart/form-data
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Param			file	formData	file	true	"Image file"
// @Success		200		{object}	eventdto.Event
// @Failure		400		{object}	eventdto.ErrorAlias
// @Failure		401		{object}	eventdto.ErrorAlias
// @Failure		403		{object}	eventdto.ErrorAlias
// @Failure		404		{object}	eventdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/cover [post]
func (h *Handler) UploadCover(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (5<<20)+1024)
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read file"})
		return
	}
	defer src.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	mime := http.DetectContentType(head[:n])
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read file"})
		return
	}
	e, err := h.svc.SetCover(orgRef(c), c.Param("eventId"), src, file.Size, mime)
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// ListPublicEvents GET /api/v1/events (published only, public).
//
// @Summary		List published events
// @Description	Public discovery across orgs, filterable and paginated.
// @Tags			events
// @Produce		json
// @Param			org		query		string	false	"Org slug filter"	example(acme)
// @Param			status	query		string	false	"Status filter"	example(PUBLISHED)
// @Param			from	query		string	false	"RFC3339 start"	example(2026-01-01T00:00:00Z)
// @Param			to		query		string	false	"RFC3339 end"	example(2026-12-31T23:59:59Z)
// @Param			q		query		string	false	"Title search"	example(fest)
// @Param			sort	query		string	false	"starts|created"	example(starts)
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	eventdto.EventsPage
// @Failure		400		{object}	eventdto.ErrorAlias
// @Router			/events [get]
func (h *Handler) ListPublicEvents(c *gin.Context) {
	limit, offset := page(c)
	var f eventdto.EventFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, total, err := h.svc.ListPublic(f, limit, offset, f.Sort)
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, eventdto.EventsPage{Items: items, Total: total})
}

// GetPublicEvent GET /api/v1/events/:id (published only, public).
//
// @Summary		Get published event
// @Description	Public detail by UUID. Drafts/cancelled return 404.
// @Tags			events
// @Produce		json
// @Param			id	path		string	true	"Event UUID"	example(4d5e6f70-8192-0a1b-2c3d-4e5f6a7b8c9d)
// @Success		200	{object}	eventdto.Event
// @Failure		400	{object}	eventdto.ErrorAlias
// @Failure		404	{object}	eventdto.ErrorAlias
// @Router			/events/{id} [get]
func (h *Handler) GetPublicEvent(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}
	e, err := h.svc.GetPublicEvent(id)
	if err != nil {
		c.JSON(eventErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// ---------- helpers ----------

func eventErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrEventNotFound),
		errors.Is(err, service.ErrOrgUnresolved):
		return http.StatusNotFound
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, service.ErrSlugTaken),
		errors.Is(err, service.ErrInvalidStatus):
		return http.StatusConflict
	case errors.Is(err, service.ErrInvalidSlug),
		errors.Is(err, service.ErrInvalidTitle),
		errors.Is(err, service.ErrInvalidDates),
		errors.Is(err, service.ErrInvalidCap),
		errors.Is(err, service.ErrCoverTooLarge),
		errors.Is(err, service.ErrCoverType),
		errors.Is(err, service.ErrCoverRequired):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func orgRef(c *gin.Context) string {
	if ref := strings.TrimSpace(c.Param("id")); ref != "" {
		return ref
	}
	return strings.TrimSpace(c.Param("orgId"))
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

func parseTime(s *string) (*time.Time, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*s))
	if err != nil {
		return nil, err
	}
	return &t, nil
}
