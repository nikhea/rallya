package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	checkindto "github.com/nikhea/rallya/internal/checkin/dto"
	"github.com/nikhea/rallya/internal/checkin/service"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
)

// Handler adapts CheckinService to Gin. No business logic here.
type Handler struct {
	svc *service.CheckinService
}

// NewHandler builds the handler.
func NewHandler(svc *service.CheckinService) *Handler { return &Handler{svc: svc} }

// Scan POST /api/v1/orgs/:id/events/:eventId/checkin (ADMIN+).
//
// @Summary		Scan one code
// @Description	QR string or roster attendee ID. Refusals return 200 with their outcome.
// @Tags			checkin
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			eventId	path		string					true	"Event UUID or slug"
// @Param			request	body		checkindto.ScanRequest	true	"Code or attendee ID"
// @Success		200		{object}	checkindto.ScanResult
// @Failure		400		{object}	checkindto.ErrorAlias
// @Failure		401		{object}	checkindto.ErrorAlias
// @Failure		403		{object}	checkindto.ErrorAlias
// @Failure		404		{object}	checkindto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/checkin [post]
func (h *Handler) Scan(c *gin.Context) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	staffID, ok := authhandler.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var in checkindto.ScanRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.Code == "" && in.AttendeeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code or attendeeId is required"})
		return
	}
	r, err := h.svc.Scan(orgID, c.Param("eventId"), in.Code, in.AttendeeID, staffID)
	if err != nil {
		c.JSON(checkinErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toScanResult(r))
}

// ScanBatch POST /api/v1/orgs/:id/events/:eventId/checkin/batch (ADMIN+).
//
// @Summary		Scan a batch of codes
// @Description	QR-only, per-item outcomes, no fail-fast. Max 50 codes.
// @Tags			checkin
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			eventId	path		string					true	"Event UUID or slug"
// @Param			request	body		checkindto.BatchRequest	true	"QR codes"
// @Success		200		{object}	checkindto.BatchResult
// @Failure		400		{object}	checkindto.ErrorAlias
// @Failure		401		{object}	checkindto.ErrorAlias
// @Failure		403		{object}	checkindto.ErrorAlias
// @Failure		404		{object}	checkindto.ErrorAlias
// @Failure		413		{object}	checkindto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/checkin/batch [post]
func (h *Handler) ScanBatch(c *gin.Context) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	staffID, ok := authhandler.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var in checkindto.BatchRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(in.Codes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "codes must not be empty"})
		return
	}
	results, err := h.svc.ScanBatch(orgID, c.Param("eventId"), in.Codes, staffID)
	if err != nil {
		c.JSON(checkinErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	out := make([]checkindto.ScanResult, 0, len(results))
	for i := range results {
		out = append(out, toScanResult(&results[i]))
	}
	c.JSON(http.StatusOK, checkindto.BatchResult{Results: out})
}

// Stats GET /api/v1/orgs/:id/events/:eventId/checkin/stats (ADMIN+).
// @Summary		Door-dashboard counts
// @Description	Registered / checked-in / cancelled tallies for one event.
// @Tags			checkin
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Success		200		{object}	checkindto.StatsResponse
// @Failure		401		{object}	checkindto.ErrorAlias
// @Failure		403		{object}	checkindto.ErrorAlias
// @Failure		404		{object}	checkindto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/checkin/stats [get]
func (h *Handler) Stats(c *gin.Context) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	registered, checkedIn, cancelled, err := h.svc.Stats(orgID, c.Param("eventId"))
	if err != nil {
		c.JSON(checkinErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, checkindto.StatsResponse{
		Registered: registered, CheckedIn: checkedIn,
		Cancelled: cancelled, Total: registered + checkedIn + cancelled,
	})
}

// Revert POST /api/v1/orgs/:id/events/:eventId/checkin/revert (ADMIN+).
//
// @Summary		Undo a mis-scan
// @Description	CHECKED_IN back to REGISTERED, timestamp cleared, logged REVERTED.
// @Tags			checkin
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string						true	"Org UUID or slug"
// @Param			eventId	path		string						true	"Event UUID or slug"
// @Param			request	body		checkindto.RevertRequest	true	"Roster attendee ID"
// @Success		200		{object}	checkindto.ScanResult
// @Failure		400		{object}	checkindto.ErrorAlias
// @Failure		401		{object}	checkindto.ErrorAlias
// @Failure		403		{object}	checkindto.ErrorAlias
// @Failure		404		{object}	checkindto.ErrorAlias
// @Failure		422		{object}	checkindto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/checkin/revert [post]
func (h *Handler) Revert(c *gin.Context) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	staffID, ok := authhandler.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var in checkindto.RevertRequest
	if err := c.ShouldBindJSON(&in); err != nil || in.AttendeeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attendeeId is required"})
		return
	}
	r, err := h.svc.Revert(orgID, c.Param("eventId"), in.AttendeeID, staffID)
	if err != nil {
		c.JSON(checkinErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toScanResult(r))
}

func toScanResult(r *service.ScanResult) checkindto.ScanResult {
	out := checkindto.ScanResult{Outcome: string(r.Outcome), Method: string(r.Method)}
	if r.AttendeeID != nil {
		s := r.AttendeeID.String()
		out.AttendeeID = &s
	}
	if r.CheckedInAt != nil {
		s := r.CheckedInAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.CheckedInAt = &s
	}
	return out
}

func checkinErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrCheckinNotFound),
		errors.Is(err, service.ErrAttendeeNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrBatchTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, service.ErrQRSecretUnset):
		return http.StatusServiceUnavailable
	case errors.Is(err, service.ErrNotCheckedIn):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusBadRequest
	}
}
