package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	subdto "github.com/nikhea/rallya/internal/subscription/dto"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
	"github.com/nikhea/rallya/internal/subscription/service"
)

// Handler adapts SubscriptionService to Gin. No business logic here.
type Handler struct {
	svc    *service.SubscriptionService
	appURL string
}

// NewHandler builds the handler (appURL roots Stripe return URLs).
func NewHandler(svc *service.SubscriptionService, appURL string) *Handler {
	return &Handler{svc: svc, appURL: appURL}
}

// GetPlans GET /api/v1/subscription/plans (public).
//
// @Summary		Subscription catalog
// @Description	Three tiers with quotas, feature flags, and monthly prices.
// @Tags			subscriptions
// @Produce		json
// @Success		200	{array}		subdto.TierResponse
// @Router			/subscription/plans [get]
func (h *Handler) GetPlans(c *gin.Context) {
	tiers, err := h.svc.Catalog(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "catalog unavailable"})
		return
	}
	out := make([]subdto.TierResponse, 0, len(tiers))
	for _, t := range tiers {
		out = append(out, subdto.TierResponse{
			Plan: string(t.Plan), Name: t.Name, PriceID: t.PriceID,
			MonthlyCents: t.MonthlyCents, Currency: t.Currency,
			Limits: subdto.Limits{
				MaxEvents: t.Limits.MaxEvents, MaxMembers: t.Limits.MaxMembers,
				MaxAttendeesPerEvent: t.Limits.MaxAttendeesPerEvent, MaxKitsPerEvent: t.Limits.MaxKitsPerEvent,
			},
			Features: t.Features,
		})
	}
	c.JSON(http.StatusOK, out)
}

// GetSubscription GET /api/v1/orgs/:id/subscription (members).
//
// @Summary		Org billing state
// @Description	Plan, status, and period end. Absent subscription reads FREE.
// @Tags			subscriptions
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Org UUID or slug"
// @Success		200	{object}	subdto.SubscriptionResponse
// @Failure		401	{object}	subdto.ErrorAlias
// @Failure		404	{object}	subdto.ErrorAlias
// @Router			/orgs/{id}/subscription [get]
func (h *Handler) GetSubscription(c *gin.Context) {
	orgID, ok := orghandler.OrgIDFromContext(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	sub, err := h.svc.GetSubscription(orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "subscription unavailable"})
		return
	}
	if sub == nil {
		c.JSON(http.StatusOK, subdto.SubscriptionResponse{Plan: string(submodel.PlanFree), Status: string(submodel.StatusActive)})
		return
	}
	out := subdto.SubscriptionResponse{
		Plan: string(sub.Plan), Status: string(sub.Status), CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
	}
	if sub.CurrentPeriodEnd != nil {
		s := sub.CurrentPeriodEnd.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.CurrentPeriodEnd = &s
	}
	c.JSON(http.StatusOK, out)
}

// StartCheckout POST /api/v1/orgs/:id/subscription/checkout (OWNER).
//
// @Summary		Start a subscription Checkout
// @Description	Stripe subscription-mode Checkout for PRO/SCALE. Downgrades and cancels run through the Customer Portal.
// @Tags			subscriptions
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			request	body		subdto.CheckoutRequest	true	"Target plan"
// @Success		201		{object}	subdto.CheckoutResponse
// @Failure		400		{object}	subdto.ErrorAlias
// @Failure		401		{object}	subdto.ErrorAlias
// @Failure		403		{object}	subdto.ErrorAlias
// @Failure		404		{object}	subdto.ErrorAlias
// @Failure		409		{object}	subdto.ErrorAlias
// @Failure		503		{object}	subdto.ErrorAlias
// @Router			/orgs/{id}/subscription/checkout [post]
func (h *Handler) StartCheckout(c *gin.Context) {
	orgID, staffID, ok := staffFromContext(c)
	if !ok {
		return
	}
	var in subdto.CheckoutRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	plan := submodel.Plan(in.Plan)
	url, sessionID, err := h.svc.StartCheckout(c.Request.Context(), staffID, orgID, plan,
		h.appURL+"/billing/success?session_id={CHECKOUT_SESSION_ID}",
		h.appURL+"/billing/cancel")
	if err != nil {
		status, code := subWireError(err)
		c.JSON(status, gin.H{"error": code.Error(), "code": code.Code()})
		return
	}
	c.JSON(http.StatusCreated, subdto.CheckoutResponse{URL: url, SessionID: sessionID})
}

// CreatePortalSession POST /api/v1/orgs/:id/subscription/portal (OWNER).
//
// @Summary		Customer Portal session
// @Description	Self-serve manage/cancel (downgrades land at period end).
// @Tags			subscriptions
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Org UUID or slug"
// @Success		200	{object}	subdto.PortalResponse
// @Failure		401	{object}	subdto.ErrorAlias
// @Failure		403	{object}	subdto.ErrorAlias
// @Failure		404	{object}	subdto.ErrorAlias
// @Failure		503	{object}	subdto.ErrorAlias
// @Router			/orgs/{id}/subscription/portal [post]
func (h *Handler) CreatePortalSession(c *gin.Context) {
	orgID, staffID, ok := staffFromContext(c)
	if !ok {
		return
	}
	url, err := h.svc.CreatePortalSession(c.Request.Context(), staffID, orgID, h.appURL+"/billing")
	if err != nil {
		status, code := subWireError(err)
		c.JSON(status, gin.H{"error": code.Error(), "code": code.Code()})
		return
	}
	c.JSON(http.StatusOK, subdto.PortalResponse{URL: url})
}

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

// codedErr carries a machine code alongside the message.
type codedErr struct {
	msg  string
	code string
}

func (e *codedErr) Error() string { return e.msg }
func (e *codedErr) Code() string  { return e.code }

func subWireError(err error) (int, *codedErr) {
	switch {
	case errors.Is(err, service.ErrInvalidPlan):
		return http.StatusBadRequest, &codedErr{msg: err.Error(), code: "INVALID_PLAN"}
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden, &codedErr{msg: err.Error(), code: "FORBIDDEN"}
	case errors.Is(err, service.ErrAlreadySubscribed):
		return http.StatusConflict, &codedErr{msg: err.Error(), code: "ALREADY_SUBSCRIBED"}
	case errors.Is(err, service.ErrNoSubscription):
		return http.StatusNotFound, &codedErr{msg: err.Error(), code: "NO_SUBSCRIPTION"}
	case errors.Is(err, service.ErrBillingUnavailable):
		return http.StatusServiceUnavailable, &codedErr{msg: err.Error(), code: "BILLING_UNAVAILABLE"}
	default:
		return http.StatusBadRequest, &codedErr{msg: "subscription failed", code: "SUBSCRIPTION_FAILED"}
	}
}
