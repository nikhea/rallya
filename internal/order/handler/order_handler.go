package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	orderdto "github.com/nikhea/rallya/internal/order/dto"
	"github.com/nikhea/rallya/internal/order/service"
)

// Handler adapts OrderService to Gin. No business logic here.
type Handler struct {
	svc *service.OrderService
}

// NewHandler builds the handler.
func NewHandler(svc *service.OrderService) *Handler { return &Handler{svc: svc} }

// CreateOrder POST /api/v1/events/:id/orders (auth; published events only).
//
// @Summary		Create order
// @Description	Claims inventory atomically; free orders confirm immediately, priced wait for payments. Idempotent via idempotencyKey.
// @Tags			orders
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Event UUID"	example(4d5e6f70-8192-0a1b-2c3d-4e5f6a7b8c9d)
// @Param			request	body		orderdto.CreateOrder	true	"Order payload"
// @Success		201		{object}	orderdto.Order
// @Failure		400		{object}	orderdto.ErrorAlias
// @Failure		401		{object}	orderdto.ErrorAlias
// @Failure		404		{object}	orderdto.ErrorAlias
// @Failure		409		{object}	orderdto.ErrorAlias	"Sold out"
// @Router			/events/{id}/orders [post]
func (h *Handler) CreateOrder(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	eventID, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}
	var in orderdto.CreateOrder
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	typeID, err := uuid.Parse(strings.TrimSpace(in.TicketTypeID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ticket type id"})
		return
	}
	// The event path param scopes the claim; the type must belong to it.
	o, err := h.svc.CreateOrder(c.Request.Context(), uid, eventID, service.CreateInput{
		TicketTypeID: typeID, Quantity: in.Quantity, IdempotencyKey: strVal(in.IdempotencyKey),
	})
	if err != nil {
		c.JSON(orderErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, o)
}

// ListMyOrders GET /api/v1/orders/mine (auth).
//
// @Summary		List my orders
// @Description	Caller history, newest first, paginated.
// @Tags			orders
// @Produce		json
// @Security		BearerAuth
// @Param			page	query		int		false	"Page (1-based)"	example(1)
// @Param			perPage	query		int		false	"Page size, max 100"	example(20)
// @Success		200		{object}	orderdto.OrdersPage
// @Failure		401		{object}	orderdto.ErrorAlias
// @Router			/orders/mine [get]
func (h *Handler) ListMyOrders(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	limit, offset := page(c)
	items, total, err := h.svc.ListMyOrders(uid, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	c.JSON(http.StatusOK, orderdto.OrdersPage{Items: items, Total: total})
}

// GetOrder GET /api/v1/orders/:id (owner or org ADMIN+).
//
// @Summary		Get order
// @Description	Owners see their own; org admins via support path. Others get 404.
// @Tags			orders
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Order UUID"
// @Success		200	{object}	orderdto.Order
// @Failure		401	{object}	orderdto.ErrorAlias
// @Failure		404	{object}	orderdto.ErrorAlias
// @Router			/orders/{id} [get]
func (h *Handler) GetOrder(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
		return
	}
	o, err := h.svc.GetOrder(uid, id)
	if err != nil {
		c.JSON(orderErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, o)
}

// CancelOrder POST /api/v1/orders/:id/cancel (owner or org ADMIN+).
//
// @Summary		Cancel order
// @Description	Releases held inventory. Terminal states rejected. Priced refunds arrive with payments.
// @Tags			orders
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Order UUID"
// @Success		200	{object}	orderdto.Order
// @Failure		400	{object}	orderdto.ErrorAlias
// @Failure		401	{object}	orderdto.ErrorAlias
// @Failure		404	{object}	orderdto.ErrorAlias
// @Router			/orders/{id}/cancel [post]
func (h *Handler) CancelOrder(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
		return
	}
	o, err := h.svc.CancelOrder(c.Request.Context(), uid, id)
	if err != nil {
		c.JSON(orderErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, o)
}

// ---------- helpers ----------

func orderErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrOrderNotFound),
		errors.Is(err, service.ErrTicketGone):
		return http.StatusNotFound
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, service.ErrSoldOut),
		errors.Is(err, service.ErrInvalidStatus):
		return http.StatusConflict
	case errors.Is(err, service.ErrInvalidQty),
		errors.Is(err, service.ErrTooMany):
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
