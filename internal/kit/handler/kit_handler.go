package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	kitdto "github.com/nikhea/rallya/internal/kit/dto"
	"github.com/nikhea/rallya/internal/kit/service"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
)

// Handler adapts KitService to Gin. No business logic here.
type Handler struct {
	svc *service.KitService
}

// NewHandler builds the handler.
func NewHandler(svc *service.KitService) *Handler { return &Handler{svc: svc} }

// CreateKit POST /api/v1/orgs/:id/events/:eventId/kits (ADMIN+).
//
// @Summary		Define a kit type
// @Description	Named kit (merch/welcome pack) with a total quantity for one event.
// @Tags			kits
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			eventId	path		string					true	"Event UUID or slug"
// @Param			request	body		kitdto.CreateKitRequest	true	"Kit definition"
// @Success		201		{object}	kitdto.KitResponse
// @Failure		400		{object}	kitdto.ErrorAlias
// @Failure		401		{object}	kitdto.ErrorAlias
// @Failure		403		{object}	kitdto.ErrorAlias
// @Failure		404		{object}	kitdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/kits [post]
func (h *Handler) CreateKit(c *gin.Context) {
	orgID, staffID, ok := staffFromContext(c)
	if !ok {
		return
	}
	var in kitdto.CreateKitRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	k, err := h.svc.CreateKit(orgID, c.Param("eventId"), in.Name, in.Description, in.QuantityTotal, staffID)
	if err != nil {
		c.JSON(kitErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toKitResponse(k))
}

// ListKits GET /api/v1/orgs/:id/events/:eventId/kits (ADMIN+).
//
// @Summary		List kit types
// @Description	Every kit type for one event with live pending/collected/voided/remaining tallies.
// @Tags			kits
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Success		200		{array}		kitdto.KitResponse
// @Failure		401		{object}	kitdto.ErrorAlias
// @Failure		403		{object}	kitdto.ErrorAlias
// @Failure		404		{object}	kitdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/kits [get]
func (h *Handler) ListKits(c *gin.Context) {
	orgID, _, ok := staffFromContext(c)
	if !ok {
		return
	}
	kits, err := h.svc.ListKits(orgID, c.Param("eventId"))
	if err != nil {
		c.JSON(kitErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	out := make([]kitdto.KitResponse, 0, len(kits))
	for i := range kits {
		out = append(out, toKitResponse(&kits[i]))
	}
	c.JSON(http.StatusOK, out)
}

// UpdateKit PATCH /api/v1/orgs/:id/events/:eventId/kits/:kitId (ADMIN+).
//
// @Summary		Update a kit type
// @Description	QuantityTotal cannot drop below the collected count (422).
// @Tags			kits
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			eventId	path		string					true	"Event UUID or slug"
// @Param			kitId	path		string					true	"Kit UUID"
// @Param			request	body		kitdto.UpdateKitRequest	true	"Kit patch"
// @Success		200		{object}	kitdto.KitResponse
// @Failure		400		{object}	kitdto.ErrorAlias
// @Failure		401		{object}	kitdto.ErrorAlias
// @Failure		403		{object}	kitdto.ErrorAlias
// @Failure		404		{object}	kitdto.ErrorAlias
// @Failure		422		{object}	kitdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/kits/{kitId} [patch]
func (h *Handler) UpdateKit(c *gin.Context) {
	orgID, staffID, ok := staffFromContext(c)
	if !ok {
		return
	}
	var in kitdto.UpdateKitRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	k, err := h.svc.UpdateKit(orgID, c.Param("eventId"), c.Param("kitId"), in.Name, in.Description, in.QuantityTotal, staffID)
	if err != nil {
		c.JSON(kitErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toKitResponse(k))
}

// DeleteKit DELETE /api/v1/orgs/:id/events/:eventId/kits/:kitId (ADMIN+).
//
// @Summary		Delete a kit type
// @Description	Refused while active (non-voided) collections exist — void them first.
// @Tags			kits
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Param			kitId	path		string	true	"Kit UUID"
// @Success		204		"Deleted"
// @Failure		401		{object}	kitdto.ErrorAlias
// @Failure		403		{object}	kitdto.ErrorAlias
// @Failure		404		{object}	kitdto.ErrorAlias
// @Failure		422		{object}	kitdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/kits/{kitId} [delete]
func (h *Handler) DeleteKit(c *gin.Context) {
	orgID, staffID, ok := staffFromContext(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteKit(orgID, c.Param("eventId"), c.Param("kitId"), staffID); err != nil {
		c.JSON(kitErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// Collect POST /api/v1/orgs/:id/events/:eventId/kits/:kitId/collect (ADMIN+).
//
// @Summary		Hand a kit to an attendee
// @Description	Attendee must be CHECKED_IN (422 otherwise). Collects immediately unless reserve=true holds a PENDING unit. IdempotencyKey makes tablet retries safe.
// @Tags			kits
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string				true	"Org UUID or slug"
// @Param			eventId	path		string				true	"Event UUID or slug"
// @Param			kitId	path		string				true	"Kit UUID"
// @Param			request	body		kitdto.CollectRequest	true	"Attendee handout"
// @Success		201		{object}	kitdto.CollectionResponse
// @Failure		400		{object}	kitdto.ErrorAlias
// @Failure		401		{object}	kitdto.ErrorAlias
// @Failure		403		{object}	kitdto.ErrorAlias
// @Failure		404		{object}	kitdto.ErrorAlias
// @Failure		409		{object}	kitdto.ErrorAlias
// @Failure		422		{object}	kitdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/kits/{kitId}/collect [post]
func (h *Handler) Collect(c *gin.Context) {
	orgID, staffID, ok := staffFromContext(c)
	if !ok {
		return
	}
	var in kitdto.CollectRequest
	if err := c.ShouldBindJSON(&in); err != nil || in.AttendeeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attendeeId is required"})
		return
	}
	v, err := h.svc.Collect(orgID, c.Param("eventId"), c.Param("kitId"), in.AttendeeID, in.Reserve, in.IdempotencyKey, staffID)
	if err != nil {
		c.JSON(kitErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toCollectionResponse(v))
}

// MarkCollected POST /api/v1/orgs/:id/events/:eventId/kit-collections/:collectionId/collect (ADMIN+).
//
// @Summary		Mark a reserved handout collected
// @Description	PENDING -> COLLECTED with staff stamps. Anything else is 422.
// @Tags			kits
// @Produce		json
// @Security		BearerAuth
// @Param			id				path		string	true	"Org UUID or slug"
// @Param			eventId			path		string	true	"Event UUID or slug"
// @Param			collectionId	path		string	true	"Collection UUID"
// @Success		200		{object}	kitdto.CollectionResponse
// @Failure		401		{object}	kitdto.ErrorAlias
// @Failure		403		{object}	kitdto.ErrorAlias
// @Failure		404		{object}	kitdto.ErrorAlias
// @Failure		422		{object}	kitdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/kit-collections/{collectionId}/collect [post]
func (h *Handler) MarkCollected(c *gin.Context) {
	orgID, staffID, ok := staffFromContext(c)
	if !ok {
		return
	}
	v, err := h.svc.MarkCollected(orgID, c.Param("eventId"), c.Param("collectionId"), staffID)
	if err != nil {
		c.JSON(kitErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toCollectionResponse(v))
}

// Void POST /api/v1/orgs/:id/events/:eventId/kit-collections/:collectionId/void (ADMIN+).
//
// @Summary		Void a handout
// @Description	PENDING or COLLECTED -> VOIDED (frees re-issue). Anything else is 422.
// @Tags			kits
// @Produce		json
// @Security		BearerAuth
// @Param			id				path		string	true	"Org UUID or slug"
// @Param			eventId			path		string	true	"Event UUID or slug"
// @Param			collectionId	path		string	true	"Collection UUID"
// @Success		200		{object}	kitdto.CollectionResponse
// @Failure		401		{object}	kitdto.ErrorAlias
// @Failure		403		{object}	kitdto.ErrorAlias
// @Failure		404		{object}	kitdto.ErrorAlias
// @Failure		422		{object}	kitdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/kit-collections/{collectionId}/void [post]
func (h *Handler) Void(c *gin.Context) {
	orgID, staffID, ok := staffFromContext(c)
	if !ok {
		return
	}
	v, err := h.svc.Void(orgID, c.Param("eventId"), c.Param("collectionId"), staffID)
	if err != nil {
		c.JSON(kitErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toCollectionResponse(v))
}

// ListCollections GET /api/v1/orgs/:id/events/:eventId/kit-collections (ADMIN+).
//
// @Summary		List handouts
// @Description	Filter by kitId, status (PENDING|COLLECTED|VOIDED), or attendeeId.
// @Tags			kits
// @Produce		json
// @Security		BearerAuth
// @Param			id			path		string	true	"Org UUID or slug"
// @Param			eventId		path		string	true	"Event UUID or slug"
// @Param			kitId		query		string	false	"Kit UUID"
// @Param			status		query		string	false	"Collection status"
// @Param			attendeeId	query		string	false	"Attendee UUID"
// @Success		200		{object}	kitdto.CollectionListResponse
// @Failure		400		{object}	kitdto.ErrorAlias
// @Failure		401		{object}	kitdto.ErrorAlias
// @Failure		403		{object}	kitdto.ErrorAlias
// @Failure		404		{object}	kitdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/kit-collections [get]
func (h *Handler) ListCollections(c *gin.Context) {
	orgID, _, ok := staffFromContext(c)
	if !ok {
		return
	}
	var kitID, attendeeID *uuid.UUID
	if raw := c.Query("kitId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid kit id"})
			return
		}
		kitID = &id
	}
	if raw := c.Query("attendeeId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid attendee id"})
			return
		}
		attendeeID = &id
	}
	views, total, err := h.svc.ListCollections(orgID, c.Param("eventId"), kitID, attendeeID, c.Query("status"))
	if err != nil {
		c.JSON(kitErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	items := make([]kitdto.CollectionResponse, 0, len(views))
	for i := range views {
		items = append(items, toCollectionResponse(&views[i]))
	}
	c.JSON(http.StatusOK, kitdto.CollectionListResponse{Items: items, Total: total})
}

// ListKitCollections GET /api/v1/orgs/:id/events/:eventId/kits/:kitId/collections (ADMIN+).
//
// @Summary		List handouts for one kit
// @Description	Optional ?status= filter (PENDING|COLLECTED|VOIDED).
// @Tags			kits
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Param			kitId	path		string	true	"Kit UUID"
// @Param			status	query		string	false	"Collection status"
// @Success		200		{object}	kitdto.CollectionListResponse
// @Failure		400		{object}	kitdto.ErrorAlias
// @Failure		401		{object}	kitdto.ErrorAlias
// @Failure		403		{object}	kitdto.ErrorAlias
// @Failure		404		{object}	kitdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/kits/{kitId}/collections [get]
func (h *Handler) ListKitCollections(c *gin.Context) {
	orgID, _, ok := staffFromContext(c)
	if !ok {
		return
	}
	kitID, err := uuid.Parse(c.Param("kitId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "kit not found"})
		return
	}
	views, total, err := h.svc.ListCollections(orgID, c.Param("eventId"), &kitID, nil, c.Query("status"))
	if err != nil {
		c.JSON(kitErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	items := make([]kitdto.CollectionResponse, 0, len(views))
	for i := range views {
		items = append(items, toCollectionResponse(&views[i]))
	}
	c.JSON(http.StatusOK, kitdto.CollectionListResponse{Items: items, Total: total})
}

// staffFromContext extracts org + staff identity (stealth org 404 first).
func staffFromContext(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return uuid.Nil, uuid.Nil, false
	}
	staffID, ok := authhandler.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return uuid.Nil, uuid.Nil, false
	}
	return orgID, staffID, true
}

func toKitResponse(k *service.KitWithCounts) kitdto.KitResponse {
	return kitdto.KitResponse{
		ID: k.Kit.ID.String(), EventID: k.Kit.EventID.String(),
		Name: k.Kit.Name, Description: k.Kit.Description, QuantityTotal: k.Kit.QuantityTotal,
		Pending: k.Pending, Collected: k.Collected, Voided: k.Voided, Remaining: k.Remaining,
		CreatedAt: k.Kit.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toCollectionResponse(v *service.CollectionView) kitdto.CollectionResponse {
	out := kitdto.CollectionResponse{
		ID: v.Collection.ID.String(), KitID: v.Collection.KitID.String(), KitName: v.KitName,
		EventID: v.Collection.EventID.String(), AttendeeID: v.Collection.AttendeeID.String(),
		Status:    string(v.Collection.Status),
		CreatedAt: v.Collection.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if v.Collection.CollectedAt != nil {
		s := v.Collection.CollectedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.CollectedAt = &s
	}
	if v.Collection.CollectedBy != nil {
		s := v.Collection.CollectedBy.String()
		out.CollectedBy = &s
	}
	return out
}

func kitErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrKitNotFound),
		errors.Is(err, service.ErrCollectionNotFound),
		errors.Is(err, service.ErrAttendeeNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrQuantityExhausted),
		errors.Is(err, service.ErrAlreadyCollected):
		return http.StatusConflict
	case errors.Is(err, service.ErrNotCheckedIn),
		errors.Is(err, service.ErrQuantityBelowCollected),
		errors.Is(err, service.ErrKitHasCollections),
		errors.Is(err, service.ErrInvalidStatus):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusBadRequest
	}
}
