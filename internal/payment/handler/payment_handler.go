package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	orderservice "github.com/nikhea/rallya/internal/order/service"
	"github.com/nikhea/rallya/internal/payment/service"
)

// Handler adapts PaymentService to Gin. No business logic here.
type Handler struct {
	svc    *service.PaymentService
	appURL string
}

// NewHandler builds the handler.
func NewHandler(svc *service.PaymentService, appURL string) *Handler {
	return &Handler{svc: svc, appURL: appURL}
}

// CreateCheckout POST /api/v1/orders/:id/checkout (owner, priced PENDING_PAYMENT).
//
// @Summary		Create Stripe checkout
// @Description	Returns a hosted Checkout Session URL for a priced order awaiting payment. Free orders 400 (nothing to pay).
// @Tags			payments
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Order UUID"
// @Success		200	{object}	paymentdto.CheckoutResponse
// @Failure		400	{object}	paymentdto.ErrorAlias
// @Failure		401	{object}	paymentdto.ErrorAlias
// @Failure		404	{object}	paymentdto.ErrorAlias
// @Failure		503	{object}	paymentdto.ErrorAlias	"Stripe unconfigured"
// @Router			/orders/{id}/checkout [post]
func (h *Handler) CreateCheckout(c *gin.Context) {
	uid, _ := authhandler.UserFromContext(c)
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
		return
	}
	out, err := h.svc.CreateCheckout(c.Request.Context(), uid, id, h.appURL)
	if err != nil {
		c.JSON(paymentErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

// StripeWebhook POST /api/v1/webhooks/stripe (public; signature-verified).
//
// Fulfillment runs ONLY here — never on the success page. Handles
// checkout.session.completed and async_payment_succeeded (gated on paid
// status); async failures are logged. Bad signatures 400; handled and
// irrelevant events 200 (so Stripe stops retrying successes).
//
// @Summary		Stripe webhook
// @Description	Signature-verified fulfillment endpoint. Send raw event JSON with a valid Stripe-Signature header.
// @Tags			payments
// @Accept			json
// @Produce		json
// @Param			Stripe-Signature	header		string	true	"HMAC signature"	example(t=1492774577,v1=5257a869e7ecebeda32affa9cfc0f4)
// @Success		200					{object}	paymentdto.MessageAlias	"ok / ignored"
// @Failure		400					{object}	paymentdto.ErrorAlias	"Bad signature / empty payload"
// @Router			/webhooks/stripe [post]
func (h *Handler) StripeWebhook(c *gin.Context) {
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil || len(payload) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty payload"})
		return
	}
	if err := h.svc.HandleWebhook(payload, c.GetHeader("Stripe-Signature")); err != nil {
		var fe *service.FulfillError
		if errors.As(err, &fe) && isSignatureError(fe) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad signature"})
			return
		}
		// Handler-known fulfillment failures still ack: Stripe must not
		// retry what we deliberately reject (unknown order, wrong state).
		slog.Warn("stripe webhook ignored", "error", err)
		c.JSON(http.StatusOK, gin.H{"message": "ignored"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func paymentErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidOrderState),
		errors.Is(err, service.ErrNoPaymentRequired):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrProviderDown):
		return http.StatusServiceUnavailable
	case errors.Is(err, orderservice.ErrOrderNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func isSignatureError(fe *service.FulfillError) bool {
	return strings.HasPrefix(fe.Error(), "bad signature")
}
