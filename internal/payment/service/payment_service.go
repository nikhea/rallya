package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	stripewebhook "github.com/stripe/stripe-go/v86/webhook"

	"github.com/nikhea/rallya/cmd/config"
	orderdto "github.com/nikhea/rallya/internal/order/dto"
	orderservice "github.com/nikhea/rallya/internal/order/service"
	paymentdto "github.com/nikhea/rallya/internal/payment/dto"
)

// OrderStore is implemented by the orders domain (CheckoutDetail,
// MarkPaid, SetStripeSession). Payments never touches order tables;
// fulfillment goes through MarkPaid only.
type OrderStore interface {
	CheckoutDetail(userID, orderID uuid.UUID) (*orderservice.CheckoutDetail, error)
	MarkPaid(orderID uuid.UUID, sessionID, paymentIntentID string) (*orderdto.Order, error)
	SetStripeSession(orderID uuid.UUID, sessionID string) error
}

// PaymentService orchestrates checkout creation and webhook fulfillment.
type PaymentService struct {
	orders   OrderStore
	provider CheckoutProvider
	billing  BillingHandler
}

// BillingHandler routes subscription/invoice events (implemented by the
// subscription domain). Nil-safe: billing events ack silently when the
// subscription module is unwired.
type BillingHandler interface {
	HandleBillingEvent(evt *stripe.Event) (bool, error)
}

// SetBillingHandler wires subscription webhook dispatch.
func (s *PaymentService) SetBillingHandler(b BillingHandler) { s.billing = b }

// NewPaymentService builds the service. provider nil = misconfigured;
// checkout calls fail closed, webhook verification still enforced.
func NewPaymentService(orders OrderStore, provider CheckoutProvider) *PaymentService {
	return &PaymentService{orders: orders, provider: provider}
}

// CreateCheckout builds (or reuses) a hosted session for a PENDING_PAYMENT
// order owned by the caller. Free orders and wrong states are rejected;
// fulfillment never happens here — only the webhook fulfills.
func (s *PaymentService) CreateCheckout(ctx context.Context, userID, orderID uuid.UUID, appURL string) (*paymentdto.CheckoutResponse, error) {
	if s.provider == nil {
		return nil, ErrProviderDown
	}
	d, err := s.orders.CheckoutDetail(userID, orderID)
	if err != nil {
		return nil, err
	}
	if d.Order.PriceCents <= 0 {
		return nil, ErrNoPaymentRequired
	}
	if string(d.Order.Status) != "PENDING_PAYMENT" {
		return nil, ErrInvalidOrderState
	}
	if d.Order.StripeSessionID != nil && *d.Order.StripeSessionID != "" {
		// A session already exists: mint a fresh link (old ones expire)
		// but keep a single stored id.
		return s.createNew(ctx, d, appURL)
	}
	return s.createNew(ctx, d, appURL)
}

func (s *PaymentService) createNew(ctx context.Context, d *orderservice.CheckoutDetail, appURL string) (*paymentdto.CheckoutResponse, error) {
	sess, err := s.provider.CreateSession(ctx, CheckoutParams{
		OrderID: d.Order.ID, UserID: d.Order.UserID,
		Name:     fmt.Sprintf("%s — %s", d.EventTitle, d.TicketName),
		Amount:   int64(d.Order.PriceCents),
		Currency: d.Order.Currency, Quantity: int64(d.Order.Quantity),
		SuccessURL: appURL + "/payment/success?order=" + d.Order.ID.String(),
		CancelURL:  appURL + "/payment/cancel?order=" + d.Order.ID.String(),
	})
	if err != nil {
		return nil, err
	}
	if err := s.orders.SetStripeSession(d.Order.ID, sess.ID); err != nil {
		slog.Warn("checkout session not recorded", "order", d.Order.ID, "error", err)
		return nil, err
	}
	return &paymentdto.CheckoutResponse{URL: sess.URL, SessionID: sess.ID}, nil
}

// FulfillError classifies webhook outcomes for logging/metrics.
type FulfillError struct{ msg string }

func (e *FulfillError) Error() string { return e.msg }

// HandleWebhook verifies the Stripe signature and fulfills. Fulfill ONLY
// when payment_status is paid (async methods complete later via
// async_payment_succeeded). Unknown/irrelevant events ack silently.
// Signature failures and unknown orders are errors (Stripe retries 4xx/5xx
// appropriately; we return 400 for bad signatures, 200 for handled).
func (s *PaymentService) HandleWebhook(payload []byte, sigHeader string) error {
	secret := config.StripeConfigFromEnv().WebhookSecret
	if secret == "" {
		return &FulfillError{"webhook secret unconfigured"}
	}
	evt, err := stripewebhook.ConstructEvent(payload, sigHeader, secret)
	if err != nil {
		return &FulfillError{"bad signature: " + err.Error()}
	}
	switch evt.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		if s.billing != nil {
			handled, err := s.billing.HandleBillingEvent(&evt)
			if err != nil {
				return &FulfillError{"billing event failed: " + err.Error()}
			}
			if handled {
				return nil
			}
		}
		return s.fulfillSession(&evt)
	case "checkout.session.async_payment_failed":
		slog.Info("async payment failed", "event", evt.ID)
		return nil
	case "invoice.payment_succeeded", "invoice.payment_failed",
		"customer.subscription.updated", "customer.subscription.deleted":
		if s.billing == nil {
			slog.Debug("ignoring billing event (subscription module unwired)", "type", evt.Type)
			return nil
		}
		if _, err := s.billing.HandleBillingEvent(&evt); err != nil {
			return &FulfillError{"billing event failed: " + err.Error()}
		}
		return nil
	default:
		slog.Debug("ignoring stripe event", "type", evt.Type)
		return nil
	}
}

func (s *PaymentService) fulfillSession(evt *stripe.Event) error {
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(evt.Data.Raw, &sess); err != nil {
		return &FulfillError{"bad session payload: " + err.Error()}
	}
	if sess.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
		slog.Info("session not paid yet, deferring", "session", sess.ID, "status", sess.PaymentStatus)
		return nil
	}
	orderID, err := uuid.Parse(sess.Metadata["order_id"])
	if err != nil || sess.Metadata["order_id"] == "" {
		return &FulfillError{"missing order_id metadata"}
	}
	pi := ""
	if sess.PaymentIntent != nil {
		pi = sess.PaymentIntent.ID
	}
	if _, err := s.orders.MarkPaid(orderID, sess.ID, pi); err != nil {
		return &FulfillError{"mark paid failed: " + err.Error()}
	}
	slog.Info("order fulfilled", "order", orderID, "session", sess.ID)
	return nil
}

var (
	ErrProviderDown      = errors.New("payments unavailable")
	ErrNoPaymentRequired = errors.New("no payment required for this order")
	ErrInvalidOrderState = errors.New("order cannot be checked out")
)
