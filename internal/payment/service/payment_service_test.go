package service_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	orderdto "github.com/nikhea/rallya/internal/order/dto"
	"github.com/nikhea/rallya/internal/order/model"
	orderservice "github.com/nikhea/rallya/internal/order/service"
	"github.com/nikhea/rallya/internal/payment/service"
)

const testWebhookSecret = "whsec_test_1234567890abcdef"

// fakeOrders implements the payment OrderStore in-memory.
type fakeOrders struct {
	orders map[uuid.UUID]*model.Order
}

func newFakeOrders() *fakeOrders {
	return &fakeOrders{orders: map[uuid.UUID]*model.Order{}}
}

func (f *fakeOrders) addOrder(o *model.Order) {
	f.orders[o.ID] = o
}

func (f *fakeOrders) CheckoutDetail(userID, orderID uuid.UUID) (*orderservice.CheckoutDetail, error) {
	o, ok := f.orders[orderID]
	if !ok || o.UserID != userID {
		return nil, errNoOrder
	}
	return &orderservice.CheckoutDetail{Order: o, TicketName: "GA", EventTitle: "Fest"}, nil
}

func (f *fakeOrders) MarkPaid(orderID uuid.UUID, sessionID, pi string) (*orderdto.Order, error) {
	o, ok := f.orders[orderID]
	if !ok {
		return nil, errNoOrder
	}
	o.Status = model.OrderStatusConfirmed
	o.StripeSessionID = &sessionID
	if pi != "" {
		o.StripePaymentIntentID = &pi
	}
	now := time.Now()
	o.PaidAt = &now
	return &orderdto.Order{ID: o.ID.String(), Status: string(o.Status)}, nil
}

func (f *fakeOrders) SetStripeSession(orderID uuid.UUID, sessionID string) error {
	o, ok := f.orders[orderID]
	if !ok {
		return errNoOrder
	}
	o.StripeSessionID = &sessionID
	return nil
}

var errNoOrder = errors.New("order not found")

// fakeProvider records checkout creations.
type fakeProvider struct {
	lastParams service.CheckoutParams
	sessions   int
	fail       error
}

func (f *fakeProvider) CreateSession(_ context.Context, p service.CheckoutParams) (*service.CheckoutSession, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	f.lastParams = p
	f.sessions++
	return &service.CheckoutSession{
		ID:  fmt.Sprintf("cs_test_%d", f.sessions),
		URL: fmt.Sprintf("https://checkout.stripe.com/pay/cs_test_%d", f.sessions),
	}, nil
}

// signPayload builds a Stripe-Signature header the verifier accepts.
func signPayload(t *testing.T, payload []byte, secret string) string {
	t.Helper()
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, payload)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func sessionPayload(t *testing.T, orderID, status string) []byte {
	t.Helper()
	evt := map[string]any{
		"id":          "evt_test_1",
		"object":      "event",
		"api_version": "2026-08-26.dahlia",
		"type":        "checkout.session.completed",
		"data": map[string]any{
			"object": map[string]any{
				"id":             "cs_test_1",
				"object":         "checkout.session",
				"payment_status": status,
				"metadata":       map[string]any{"order_id": orderID},
				"payment_intent": "pi_test_1",
			},
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newPaymentSvc() (*service.PaymentService, *fakeOrders, *fakeProvider) {
	orders := newFakeOrders()
	provider := &fakeProvider{}
	return service.NewPaymentService(orders, provider), orders, provider
}

func seedPricedOrder(orders *fakeOrders, price int) (uuid.UUID, uuid.UUID) {
	user, order := uuid.New(), uuid.New()
	orders.addOrder(&model.Order{
		ID: order, UserID: user, EventID: uuid.New(), TicketTypeID: uuid.New(),
		Quantity: 2, PriceCents: price, Currency: "USD",
		Status: model.OrderStatusPendingPayment,
	})
	return user, order
}

func TestCreateCheckoutFreeRejected(t *testing.T) {
	svc, orders, _ := newPaymentSvc()
	user, order := seedPricedOrder(orders, 0)

	// Free orders need no checkout.
	if _, err := svc.CreateCheckout(context.Background(), user, order, "http://localhost:8080"); !errors.Is(err, service.ErrNoPaymentRequired) {
		t.Fatalf("expected ErrNoPaymentRequired, got %v", err)
	}
	// Unknown order.
	if _, err := svc.CreateCheckout(context.Background(), user, uuid.New(), "http://localhost:8080"); err == nil {
		t.Fatal("expected error for unknown order")
	}
	// Stranger's order hidden.
	if _, err := svc.CreateCheckout(context.Background(), uuid.New(), order, "http://localhost:8080"); err == nil {
		t.Fatal("expected error for stranger")
	}
}

func TestCreateCheckoutBuildsSession(t *testing.T) {
	svc, orders, provider := newPaymentSvc()
	user, order := seedPricedOrder(orders, 2500)

	out, err := svc.CreateCheckout(context.Background(), user, order, "http://localhost:8080")
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if out.URL == "" || out.SessionID == "" {
		t.Fatalf("unexpected response: %+v", out)
	}
	p := provider.lastParams
	if p.Amount != 2500 || p.Currency != "USD" || p.Quantity != 2 {
		t.Fatalf("unexpected params: %+v", p)
	}
	if !containsStr(p.SuccessURL, order.String()) || !containsStr(p.CancelURL, order.String()) {
		t.Fatalf("urls missing order: %+v", p)
	}
}

func TestCreateCheckoutProviderDown(t *testing.T) {
	orders := newFakeOrders()
	svc := service.NewPaymentService(orders, nil)
	user, order := seedPricedOrder(orders, 100)
	if _, err := svc.CreateCheckout(context.Background(), user, order, "http://localhost:8080"); !errors.Is(err, service.ErrProviderDown) {
		t.Fatalf("expected ErrProviderDown, got %v", err)
	}
}

func TestWebhookFulfillment(t *testing.T) {
	t.Setenv("STRIPE_WEBHOOKS_SIGNING_SECRET", testWebhookSecret)
	svc, orders, _ := newPaymentSvc()
	_, order := seedPricedOrder(orders, 2500)

	// Bad signature rejected.
	if err := svc.HandleWebhook(sessionPayload(t, order.String(), "paid"), "t=1,v1=deadbeef"); err == nil {
		t.Fatal("expected signature error")
	}
	// Unpaid completed event deferred, not fulfilled.
	unpaid := sessionPayload(t, order.String(), "unpaid")
	if err := svc.HandleWebhook(unpaid, signPayload(t, unpaid, testWebhookSecret)); err != nil {
		t.Fatalf("unpaid should ack nil, got %v", err)
	}
	if orders.orders[order].Status != model.OrderStatusPendingPayment {
		t.Fatal("unpaid must not fulfill")
	}
	// Paid event fulfills.
	paid := sessionPayload(t, order.String(), "paid")
	if err := svc.HandleWebhook(paid, signPayload(t, paid, testWebhookSecret)); err != nil {
		t.Fatalf("fulfill: %v", err)
	}
	got := orders.orders[order]
	if got.Status != model.OrderStatusConfirmed || got.StripeSessionID == nil || got.PaidAt == nil {
		t.Fatalf("unexpected order: %+v", got)
	}
	// Redelivery idempotent.
	if err := svc.HandleWebhook(paid, signPayload(t, paid, testWebhookSecret)); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	// Unknown order acks (no retry storm) — still nil error path via FulfillError.
	other := sessionPayload(t, uuid.New().String(), "paid")
	if err := svc.HandleWebhook(other, signPayload(t, other, testWebhookSecret)); err == nil {
		t.Fatal("expected error for unknown order")
	}
	// Async failed event acks silently.
	failedEvt := sessionPayloadAsync(t, "checkout.session.async_payment_failed")
	if err := svc.HandleWebhook(failedEvt, signPayload(t, failedEvt, testWebhookSecret)); err != nil {
		t.Fatalf("async failed should ack nil, got %v", err)
	}
}

func sessionPayloadAsync(t *testing.T, typ string) []byte {
	t.Helper()
	evt := map[string]any{
		"id": "evt_test_2", "object": "event", "api_version": "2026-08-26.dahlia", "type": typ,
		"data": map[string]any{"object": map[string]any{"id": "cs_test_9"}},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func containsStr(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
