package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	attendeedto "github.com/nikhea/rallya/internal/attendee/dto"
	"github.com/nikhea/rallya/internal/attendee/service"
	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
)

// Handler adapts AttendeeService to Gin. No business logic here.
type Handler struct {
	svc *service.AttendeeService
}

// NewHandler builds the handler.
func NewHandler(svc *service.AttendeeService) *Handler { return &Handler{svc: svc} }

// AddAttendee POST /api/v1/orgs/:id/events/:eventId/attendees (ADMIN+).
//
// @Summary		Add attendee manually
// @Description	Walk-ins, comps, staff. Email need not belong to an account.
// @Tags			attendees
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string						true	"Org UUID or slug"
// @Param			eventId	path		string						true	"Event UUID or slug"
// @Param			request	body		attendeedto.AddAttendee	true	"Attendee payload"
// @Success		201		{object}	attendeedto.Attendee
// @Failure		400		{object}	attendeedto.ErrorAlias
// @Failure		401		{object}	attendeedto.ErrorAlias
// @Failure		403		{object}	attendeedto.ErrorAlias
// @Failure		404		{object}	attendeedto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/attendees [post]
func (h *Handler) AddAttendee(c *gin.Context) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	var in attendeedto.AddAttendee
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a, err := h.svc.AddManual(orgID, c.Param("eventId"), in.Email, in.Name)
	if err != nil {
		c.JSON(attendeeErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, a)
}

// ListRoster GET /api/v1/orgs/:id/events/:eventId/attendees (ADMIN+).
//
// @Summary		List event roster
// @Description	Paginated with name/email search. No QR payloads (door use only).
// @Tags			attendees
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Param			q		query		string	false	"Name/email search"	example(jane)
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	attendeedto.AttendeesPage
// @Failure		401		{object}	attendeedto.ErrorAlias
// @Failure		403		{object}	attendeedto.ErrorAlias
// @Failure		404		{object}	attendeedto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/attendees [get]
func (h *Handler) ListRoster(c *gin.Context) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	eventID, err := h.svc.ResolveEvent(orgID, c.Param("eventId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return
	}
	limit, offset := page(c)
	items, total, err := h.svc.ListEventRoster(eventID, c.Query("q"), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	c.JSON(http.StatusOK, attendeedto.AttendeesPage{Items: items, Total: total})
}

// CorrectAttendee PATCH .../attendees/:attendeeId (ADMIN+).
//
// @Summary		Correct attendee name/email
// @Tags			attendees
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id			path		string							true	"Org UUID or slug"
// @Param			eventId		path		string							true	"Event UUID or slug"
// @Param			attendeeId	path		string							true	"Attendee UUID"
// @Param			request		body		attendeedto.CorrectAttendee	true	"Fields to correct"
// @Success		200			{object}	attendeedto.Attendee
// @Failure		400			{object}	attendeedto.ErrorAlias
// @Failure		401			{object}	attendeedto.ErrorAlias
// @Failure		403			{object}	attendeedto.ErrorAlias
// @Failure		404			{object}	attendeedto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/attendees/{attendeeId} [patch]
func (h *Handler) CorrectAttendee(c *gin.Context) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	attendeeID, err := uuid.Parse(strings.TrimSpace(c.Param("attendeeId")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid attendee id"})
		return
	}
	var in attendeedto.CorrectAttendee
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Scope the row to this event (no cross-event edits via URL juggling).
	eventID, err := h.svc.ResolveEvent(orgID, c.Param("eventId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return
	}
	a, err := h.svc.CorrectAttendeeInEvent(eventID, attendeeID, in.Name, in.Email)
	if err != nil {
		c.JSON(attendeeErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, a)
}

// ListMine GET /api/v1/attendees/mine (auth).
//
// @Summary		List my attendee records
// @Description	QR payloads arrive by confirmation email (raw tokens are hash-only at rest and cannot be re-issued here).
// @Tags			attendees
// @Produce		json
// @Security		BearerAuth
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	attendeedto.AttendeesPage
// @Failure		401		{object}	attendeedto.ErrorAlias
// @Router			/attendees/mine [get]
func (h *Handler) ListMine(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	limit, offset := page(c)
	items, total, err := h.svc.ListMine(uid, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	c.JSON(http.StatusOK, attendeedto.AttendeesPage{Items: items, Total: total})
}

// GetMine GET /api/v1/attendees/:id (owner).
//
// @Summary		Get my attendee record
// @Description	QR payloads arrive by confirmation email (hash-only storage cannot re-issue them).
// @Tags			attendees
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Attendee UUID"
// @Success		200	{object}	attendeedto.Attendee
// @Failure		401	{object}	attendeedto.ErrorAlias
// @Failure		404	{object}	attendeedto.ErrorAlias
// @Router			/attendees/{id} [get]
func (h *Handler) GetMine(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid attendee id"})
		return
	}
	a, err := h.svc.GetAttendee(uid, id)
	if err != nil {
		c.JSON(attendeeErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, a)
}

// CancelMine POST /api/v1/attendees/:id/cancel (owner).
//
// @Summary		Cancel my attendance
// @Description	Flips to CANCELLED (audit trail kept).
// @Tags			attendees
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Attendee UUID"
// @Success		200	{object}	attendeedto.Attendee
// @Failure		401	{object}	attendeedto.ErrorAlias
// @Failure		404	{object}	attendeedto.ErrorAlias
// @Failure		409	{object}	attendeedto.ErrorAlias
// @Router			/attendees/{id}/cancel [post]
func (h *Handler) CancelMine(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid attendee id"})
		return
	}
	a, err := h.svc.CancelMine(uid, id)
	if err != nil {
		c.JSON(attendeeErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, a)
}

// ---------- helpers ----------

func attendeeErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrAttendeeNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, service.ErrInvalidEmail),
		errors.Is(err, service.ErrInvalidStatus):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
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
