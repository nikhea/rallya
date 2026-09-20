package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	ticketdto "github.com/nikhea/rallya/internal/ticketing/dto"
	"github.com/nikhea/rallya/internal/ticketing/service"
)

// Handler adapts TicketService to Gin. No business logic here.
type Handler struct {
	svc *service.TicketService
}

// NewHandler builds the handler.
func NewHandler(svc *service.TicketService) *Handler { return &Handler{svc: svc} }

// CreateTicket POST /api/v1/orgs/:id/events/:eventId/tickets (ADMIN+: ticket:create).
//
// @Summary		Create ticket type
// @Description	Prices one admission tier; starts DRAFT. Capacity coupling enforced both ways.
// @Tags			tickets
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			eventId	path		string					true	"Event UUID or slug"
// @Param			request	body		ticketdto.CreateTicket	true	"Ticket payload"
// @Success		201		{object}	ticketdto.TicketType
// @Failure		400		{object}	ticketdto.ErrorAlias
// @Failure		401		{object}	ticketdto.ErrorAlias
// @Failure		403		{object}	ticketdto.ErrorAlias
// @Failure		404		{object}	ticketdto.ErrorAlias
// @Failure		409		{object}	ticketdto.ErrorAlias	"Capacity exceeded"
// @Router			/orgs/{id}/events/{eventId}/tickets [post]
func (h *Handler) CreateTicket(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in ticketdto.CreateTicket
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	starts, err := parseTime(in.SaleStartsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid saleStartsAt"})
		return
	}
	ends, err := parseTime(in.SaleEndsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid saleEndsAt"})
		return
	}
	t, err := h.svc.CreateType(&uid, orgRef(c), c.Param("eventId"), service.CreateInput{
		Name: in.Name, Description: in.Description,
		PriceCents: in.PriceCents, Currency: strVal(in.Currency),
		QuantityTotal: in.QuantityTotal, MaxPerOrder: in.MaxPerOrder,
		SaleStartsAt: starts, SaleEndsAt: ends,
	})
	if err != nil {
		c.JSON(ticketErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, t)
}

// ListTickets GET /api/v1/orgs/:id/events/:eventId/tickets (MEMBER+: ticket:read).
//
// @Summary		List ticket types
// @Description	All types incl. drafts, with live availability.
// @Tags			tickets
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string	true	"Org UUID or slug"
// @Param			eventId	path		string	true	"Event UUID or slug"
// @Success		200		{object}	ticketdto.TicketsPage
// @Failure		401		{object}	ticketdto.ErrorAlias
// @Failure		403		{object}	ticketdto.ErrorAlias
// @Failure		404		{object}	ticketdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/tickets [get]
func (h *Handler) ListTickets(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	items, err := h.svc.ListTypes(&uid, orgRef(c), c.Param("eventId"))
	if err != nil {
		c.JSON(ticketErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ticketdto.TicketsPage{Items: items, Total: int64(len(items))})
}

// GetTicket GET .../tickets/:ticketId (MEMBER+: ticket:read).
//
// @Summary		Get ticket type
// @Tags			tickets
// @Produce		json
// @Security		BearerAuth
// @Param			id			path		string	true	"Org UUID or slug"
// @Param			eventId		path		string	true	"Event UUID or slug"
// @Param			ticketId	path		string	true	"Ticket UUID"
// @Success		200			{object}	ticketdto.TicketType
// @Failure		401			{object}	ticketdto.ErrorAlias
// @Failure		403			{object}	ticketdto.ErrorAlias
// @Failure		404			{object}	ticketdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/tickets/{ticketId} [get]
func (h *Handler) GetTicket(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	t, err := h.svc.GetType(&uid, orgRef(c), c.Param("eventId"), c.Param("ticketId"))
	if err != nil {
		c.JSON(ticketErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// UpdateTicket PATCH .../tickets/:ticketId (ADMIN+: ticket:update).
//
// @Summary		Update ticket type
// @Description	Quantity cuts below sold and capacity violations rejected.
// @Tags			tickets
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id			path		string					true	"Org UUID or slug"
// @Param			eventId		path		string					true	"Event UUID or slug"
// @Param			ticketId	path		string					true	"Ticket UUID"
// @Param			request		body		ticketdto.UpdateTicket	true	"Fields to update"
// @Success		200			{object}	ticketdto.TicketType
// @Failure		400			{object}	ticketdto.ErrorAlias
// @Failure		401			{object}	ticketdto.ErrorAlias
// @Failure		403			{object}	ticketdto.ErrorAlias
// @Failure		404			{object}	ticketdto.ErrorAlias
// @Failure		409			{object}	ticketdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/tickets/{ticketId} [patch]
func (h *Handler) UpdateTicket(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	var in ticketdto.UpdateTicket
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	starts, err := parseTime(in.SaleStartsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid saleStartsAt"})
		return
	}
	ends, err := parseTime(in.SaleEndsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid saleEndsAt"})
		return
	}
	t, err := h.svc.UpdateType(&uid, orgRef(c), c.Param("eventId"), c.Param("ticketId"), service.UpdateInput{
		Name: in.Name, Description: in.Description,
		PriceCents: in.PriceCents, Currency: in.Currency,
		QuantityTotal: in.QuantityTotal, MaxPerOrder: in.MaxPerOrder,
		SaleStartsAt: starts, SaleEndsAt: ends,
	})
	if err != nil {
		c.JSON(ticketErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// DeleteTicket DELETE .../tickets/:ticketId (ADMIN+: ticket:delete).
//
// @Summary		Delete ticket type
// @Description	Only with zero sales; sold history survives.
// @Tags			tickets
// @Produce		json
// @Security		BearerAuth
// @Param			id			path		string	true	"Org UUID or slug"
// @Param			eventId		path		string	true	"Event UUID or slug"
// @Param			ticketId	path		string	true	"Ticket UUID"
// @Success		200			{object}	ticketdto.MessageAlias
// @Failure		401			{object}	ticketdto.ErrorAlias
// @Failure		403			{object}	ticketdto.ErrorAlias
// @Failure		404			{object}	ticketdto.ErrorAlias
// @Failure		409			{object}	ticketdto.ErrorAlias	"Has sales"
// @Router			/orgs/{id}/events/{eventId}/tickets/{ticketId} [delete]
func (h *Handler) DeleteTicket(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	if err := h.svc.DeleteType(&uid, orgRef(c), c.Param("eventId"), c.Param("ticketId")); err != nil {
		c.JSON(ticketErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Ticket type deleted"})
}

// ActivateTicket POST .../tickets/:ticketId/activate (ADMIN+: ticket:update).
//
// @Summary		Activate ticket type
// @Description	DRAFT/PAUSED → ACTIVE.
// @Tags			tickets
// @Produce		json
// @Security		BearerAuth
// @Param			id			path		string	true	"Org UUID or slug"
// @Param			eventId		path		string	true	"Event UUID or slug"
// @Param			ticketId	path		string	true	"Ticket UUID"
// @Success		200			{object}	ticketdto.TicketType
// @Failure		400			{object}	ticketdto.ErrorAlias
// @Failure		401			{object}	ticketdto.ErrorAlias
// @Failure		403			{object}	ticketdto.ErrorAlias
// @Failure		404			{object}	ticketdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/tickets/{ticketId}/activate [post]
func (h *Handler) ActivateTicket(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	t, err := h.svc.Activate(&uid, orgRef(c), c.Param("eventId"), c.Param("ticketId"))
	if err != nil {
		c.JSON(ticketErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// PauseTicket POST .../tickets/:ticketId/pause (ADMIN+: ticket:update).
//
// @Summary		Pause ticket type
// @Description	ACTIVE → PAUSED (reversible; distinct from computed SOLD_OUT).
// @Tags			tickets
// @Produce		json
// @Security		BearerAuth
// @Param			id			path		string	true	"Org UUID or slug"
// @Param			eventId		path		string	true	"Event UUID or slug"
// @Param			ticketId	path		string	true	"Ticket UUID"
// @Success		200			{object}	ticketdto.TicketType
// @Failure		400			{object}	ticketdto.ErrorAlias
// @Failure		401			{object}	ticketdto.ErrorAlias
// @Failure		403			{object}	ticketdto.ErrorAlias
// @Failure		404			{object}	ticketdto.ErrorAlias
// @Router			/orgs/{id}/events/{eventId}/tickets/{ticketId}/pause [post]
func (h *Handler) PauseTicket(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	t, err := h.svc.Pause(&uid, orgRef(c), c.Param("eventId"), c.Param("ticketId"))
	if err != nil {
		c.JSON(ticketErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// ListPublicTickets GET /api/v1/events/:id/tickets (published only, public).
//
// @Summary		List tickets for sale
// @Description	Active types with live availability. No purchase flow yet (orders module).
// @Tags			tickets
// @Produce		json
// @Param			id	path		string	true	"Event UUID"	example(4d5e6f70-8192-0a1b-2c3d-4e5f6a7b8c9d)
// @Success		200	{object}	ticketdto.TicketsPage
// @Failure		400	{object}	ticketdto.ErrorAlias
// @Failure		404	{object}	ticketdto.ErrorAlias
// @Router			/events/{id}/tickets [get]
func (h *Handler) ListPublicTickets(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}
	items, err := h.svc.ListPublicTypes(id)
	if err != nil {
		c.JSON(ticketErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ticketdto.TicketsPage{Items: items, Total: int64(len(items))})
}

// ---------- helpers ----------

func ticketErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrTicketNotFound),
		errors.Is(err, service.ErrEventUnresolved):
		return http.StatusNotFound
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, service.ErrEventCapacity),
		errors.Is(err, service.ErrHasSales),
		errors.Is(err, service.ErrInvalidStatus),
		errors.Is(err, service.ErrSoldOut):
		return http.StatusConflict
	case errors.Is(err, service.ErrInvalidName),
		errors.Is(err, service.ErrInvalidPrice),
		errors.Is(err, service.ErrInvalidQty),
		errors.Is(err, service.ErrInvalidWindow),
		errors.Is(err, service.ErrNotOnSale),
		errors.Is(err, service.ErrTooMany):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func orgRef(c *gin.Context) string {
	return strings.TrimSpace(c.Param("id"))
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
