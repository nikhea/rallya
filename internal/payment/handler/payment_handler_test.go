package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authdto "github.com/nikhea/rallya/internal/auth/dto"
	authmodel "github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
	orderdto "github.com/nikhea/rallya/internal/order/dto"
	"github.com/nikhea/rallya/internal/order/model"
	orderservice "github.com/nikhea/rallya/internal/order/service"
	payment "github.com/nikhea/rallya/internal/payment"
	paymentdto "github.com/nikhea/rallya/internal/payment/dto"
	"github.com/nikhea/rallya/internal/payment/handler"
	paymentservice "github.com/nikhea/rallya/internal/payment/service"
)

func init() { gin.SetMode(gin.TestMode) }

const hookSecret = "whsec_test_handler_123"

// stubOrders implements payment OrderStore over order service fakes.
type stubOrders struct {
	orders map[uuid.UUID]*stubOrder
}

type stubOrder struct {
	id      uuid.UUID
	user    uuid.UUID
	price   int
	status  string
	session string
	paid    bool
}

func (s *stubOrders) CheckoutDetail(userID, orderID uuid.UUID) (*orderservice.CheckoutDetail, error) {
	o, ok := s.orders[orderID]
	if !ok || o.user != userID {
		return nil, orderservice.ErrOrderNotFound
	}
	return &orderservice.CheckoutDetail{
		Order:      &model.Order{ID: o.id, UserID: o.user, PriceCents: o.price, Status: model.OrderStatus(o.status), StripeSessionID: optStr(o.session)},
		TicketName: "GA", EventTitle: "Fest",
	}, nil
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *stubOrders) MarkPaid(orderID uuid.UUID, sessionID, pi string) (*orderdto.Order, error) {
	o, ok := s.orders[orderID]
	if !ok {
		return nil, orderservice.ErrOrderNotFound
	}
	o.status = "CONFIRMED"
	o.paid = true
	return &orderdto.Order{ID: o.id.String(), Status: "CONFIRMED"}, nil
}

func (s *stubOrders) SetStripeSession(orderID uuid.UUID, sessionID string) error {
	o, ok := s.orders[orderID]
	if !ok {
		return orderservice.ErrOrderNotFound
	}
	o.session = sessionID
	return nil
}

// stubProvider mints fake sessions.
type stubProvider struct{ n int }

func (s *stubProvider) CreateSession(_ context.Context, p paymentservice.CheckoutParams) (*paymentservice.CheckoutSession, error) {
	s.n++
	_ = p
	return &paymentservice.CheckoutSession{ID: fmt.Sprintf("cs_test_%d", s.n), URL: "https://checkout.stripe.com/pay/x"}, nil
}

type payFixture struct {
	router *gin.Engine
	tokens map[string]string
	orders *stubOrders
}

func newPayFixture(t *testing.T) *payFixture {
	t.Helper()
	_, ar, asvc := testutil.Setup(t)
	r := gin.New()
	// Auth passthrough: real RequireAuth needs sessions; login for real.
	f := &payFixture{router: r, tokens: map[string]string{}, orders: &stubOrders{orders: map[uuid.UUID]*stubOrder{}}}
	for _, email := range []string{"pbuyer@test.com"} {
		name := "P"
		if err := asvc.Register(authdto.Register{Email: email, Password: "Str0ngP@ssw0rd!", FirstName: &name}); err != nil {
			t.Fatalf("register: %v", err)
		}
		u, _ := ar.GetUserByEmail(email)
		raw := "verify-" + email
		_ = ar.CreateEmailVerification(nil, &authmodel.EmailVerification{
			UserID: u.ID, TokenHash: token.HashToken(raw), ExpiresAt: time.Now().Add(time.Hour),
		})
		_ = asvc.VerifyEmail(raw)
		pair, err := asvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		f.tokens[email] = pair.AccessToken
		f.tokens[email+"/id"] = u.ID.String()
	}
	svc := paymentservice.NewPaymentService(f.orders, &stubProvider{})
	h := handler.NewHandler(svc, "http://localhost:8080")
	payment.RegisterRoutes(r.Group("/api/v1"), h, ar)
	return f
}

func payRequest(t *testing.T, f *payFixture, email, method, path, body string) (int, []byte) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestHTTPCheckoutFlow(t *testing.T) {
	f := newPayFixture(t)
	buyer, owner := "pbuyer@test.com", "powner@test.com"
	_ = owner
	oid := uuid.New()
	f.orders.orders[oid] = &stubOrder{id: oid, user: mustPayUID(t, f, buyer), price: 2500, status: "PENDING_PAYMENT"}

	// Anonymous -> 401.
	if code, _ := payRequest(t, f, "", "POST", "/api/v1/orders/"+oid.String()+"/checkout", ""); code != http.StatusUnauthorized {
		t.Fatalf("anon: got %d", code)
	}
	// Checkout -> 200 with URL.
	code, body := payRequest(t, f, buyer, "POST", "/api/v1/orders/"+oid.String()+"/checkout", "")
	if code != http.StatusOK {
		t.Fatalf("checkout: got %d (%s)", code, body)
	}
	var out paymentdto.CheckoutResponse
	if err := json.Unmarshal(body, &out); err != nil || out.URL == "" || out.SessionID == "" {
		t.Fatalf("response: %+v %v", out, err)
	}
	// Unknown order -> 404.
	if code, _ := payRequest(t, f, buyer, "POST", "/api/v1/orders/"+uuid.NewString()+"/checkout", ""); code != http.StatusNotFound {
		t.Fatalf("unknown: got %d", code)
	}
}

func TestHTTPWebhookVerifyAndFulfill(t *testing.T) {
	t.Setenv("STRIPE_WEBHOOKS_SIGNING_SECRET", hookSecret)
	f := newPayFixture(t)
	buyer := "pbuyer@test.com"
	oid := uuid.New()
	uid := mustPayUID(t, f, buyer)
	f.orders.orders[oid] = &stubOrder{id: oid, user: uid, price: 2500, status: "PENDING_PAYMENT"}

	payload := webhookPayload(t, oid.String(), "paid")
	sig := webhookSign(t, payload, hookSecret)

	// Bad signature -> 400.
	code, _ := payWebhook(t, f, payload, "t=1,v1=dead")
	if code != http.StatusBadRequest {
		t.Fatalf("bad sig: got %d", code)
	}
	// Good signature -> 200, order marked paid.
	code, body := payWebhook(t, f, payload, sig)
	if code != http.StatusOK {
		t.Fatalf("webhook: got %d (%s)", code, body)
	}
	if !f.orders.orders[oid].paid {
		t.Fatal("expected order paid")
	}
}

func webhookPayload(t *testing.T, orderID, status string) []byte {
	t.Helper()
	evt := map[string]any{
		"id": "evt_test_http", "object": "event", "api_version": "2026-08-26.dahlia",
		"type": "checkout.session.completed",
		"data": map[string]any{"object": map[string]any{
			"id": "cs_test_http", "object": "checkout.session",
			"payment_status": status,
			"metadata":       map[string]any{"order_id": orderID},
			"payment_intent": "pi_test_http",
		}},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func webhookSign(t *testing.T, payload []byte, secret string) string {
	t.Helper()
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, payload)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func payWebhook(t *testing.T, f *payFixture, payload []byte, sig string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func mustPayUID(t *testing.T, f *payFixture, email string) uuid.UUID {
	t.Helper()
	// userIDs stored alongside tokens.
	for k, v := range f.tokens {
		if k == email+"/id" {
			id, err := uuid.Parse(v)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
	}
	t.Fatalf("no id for %s", email)
	return uuid.Nil
}
