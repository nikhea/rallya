package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authdto "github.com/nikhea/rallya/internal/auth/dto"
	authmodel "github.com/nikhea/rallya/internal/auth/model"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	authservice "github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/event/cover"
	eventmodel "github.com/nikhea/rallya/internal/event/model"
	eventrepository "github.com/nikhea/rallya/internal/event/repository"
	eventservice "github.com/nikhea/rallya/internal/event/service"
	"github.com/nikhea/rallya/internal/iam"
	"github.com/nikhea/rallya/internal/notification/jobs"
	order "github.com/nikhea/rallya/internal/order"
	orderdto "github.com/nikhea/rallya/internal/order/dto"
	"github.com/nikhea/rallya/internal/order/handler"
	ordermodel "github.com/nikhea/rallya/internal/order/model"
	orderrepository "github.com/nikhea/rallya/internal/order/repository"
	orderservice "github.com/nikhea/rallya/internal/order/service"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
	ticketmodel "github.com/nikhea/rallya/internal/ticketing/model"
	ticketrepository "github.com/nikhea/rallya/internal/ticketing/repository"
	ticketservice "github.com/nikhea/rallya/internal/ticketing/service"
)

func init() { gin.SetMode(gin.TestMode) }

type orderFixture struct {
	router  *gin.Engine
	tokens  map[string]string
	eventID string
	freeID  string
	paidID  string
}

func newOrderFixture(t *testing.T) *orderFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	for _, m := range [][]any{
		orgmodel.AllModels(), eventmodel.AllModels(), ticketmodel.AllModels(),
	} {
		_ = m
	}
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate org: %v", err)
	}
	if err := db.AutoMigrate(eventmodel.AllModels()...); err != nil {
		t.Fatalf("migrate events: %v", err)
	}
	if err := db.AutoMigrate(ticketmodel.AllModels()...); err != nil {
		t.Fatalf("migrate tickets: %v", err)
	}
	if err := db.AutoMigrate(ordermodel.AllModels()...); err != nil {
		t.Fatalf("migrate orders: %v", err)
	}
	if err := db.AutoMigrate(&gormadapter.CasbinRule{}); err != nil {
		t.Fatalf("migrate casbin: %v", err)
	}
	e, err := iam.NewEnforcer(db)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}

	orgRepo := orgrepository.NewOrgRepository(db)
	orgSvc := orgservice.NewOrgService(orgRepo, authSvc)
	orgSvc.SetGroupSyncer(iam.NewMembershipSyncer(e))
	orgSvc.SetPolicySeeder(iam.NewOrgPolicySeeder(e))

	eventRepo := eventrepository.NewEventRepository(db)
	eventSvc := eventservice.NewEventService(eventRepo, orgSvc)
	eventSvc.SetEnqueuer(&jobs.FakeEnqueuer{})
	eventSvc.SetCoverStorage(cover.NewLocal(t.TempDir()))

	ticketRepo := ticketrepository.NewTicketRepository(db)
	ticketSvc := ticketservice.NewTicketService(ticketRepo, ticketservice.NewEventAdapter(eventSvc))

	orderRepo := orderrepository.NewOrderRepository(db)
	orderSvc := orderservice.NewOrderService(orderRepo, ticketSvc, eventSvc, orgSvc)
	orderHandler := handler.NewHandler(orderSvc)

	r := gin.New()
	order.RegisterRoutes(r.Group("/api/v1"), orderHandler, authRepo)

	f := &orderFixture{router: r, tokens: map[string]string{}}
	owner := mkOrderUser(t, authRepo, authSvc, "oowner@test.com")
	mkOrderUser(t, authRepo, authSvc, "omember@test.com")
	if _, err := orgSvc.CreateOrg(owner, "Acme", "acme", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := orgSvc.AddMember(owner, "acme", "omember@test.com", "MEMBER"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ev, err := eventSvc.CreateEvent(owner, "acme", eventservice.CreateInput{Title: "Fest"})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := eventSvc.Publish("acme", ev.Slug); err != nil {
		t.Fatalf("publish: %v", err)
	}
	free, err := ticketSvc.CreateType(&owner, "acme", ev.Slug, ticketservice.CreateInput{Name: "Free", QuantityTotal: 10})
	if err != nil {
		t.Fatalf("create free: %v", err)
	}
	if _, err := ticketSvc.Activate(&owner, "acme", ev.Slug, free.ID); err != nil {
		t.Fatalf("activate free: %v", err)
	}
	paid, err := ticketSvc.CreateType(&owner, "acme", ev.Slug, ticketservice.CreateInput{Name: "VIP", PriceCents: 5000, QuantityTotal: 5})
	if err != nil {
		t.Fatalf("create paid: %v", err)
	}
	if _, err := ticketSvc.Activate(&owner, "acme", ev.Slug, paid.ID); err != nil {
		t.Fatalf("activate paid: %v", err)
	}
	for _, email := range []string{"oowner@test.com", "omember@test.com"} {
		pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		f.tokens[email] = pair.AccessToken
	}
	f.freeID = free.ID
	f.paidID = paid.ID
	f.eventID = ev.ID
	return f
}

func mkOrderUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email string) uuid.UUID {
	t.Helper()
	name := "O"
	if err := authSvc.Register(authdto.Register{Email: email, Password: "Str0ngP@ssw0rd!", FirstName: &name}); err != nil {
		t.Fatalf("register: %v", err)
	}
	u, err := authRepo.GetUserByEmail(email)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	raw := "verify-" + email
	if err := authRepo.CreateEmailVerification(nil, &authmodel.EmailVerification{
		UserID: u.ID, TokenHash: token.HashToken(raw), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := authSvc.VerifyEmail(raw); err != nil {
		t.Fatalf("verify: %v", err)
	}
	return u.ID
}

func ordRequest(t *testing.T, f *orderFixture, email, method, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if email != "" {
		tok, ok := f.tokens[email]
		if !ok {
			t.Fatalf("no token for %s", email)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestHTTPOrderFlows(t *testing.T) {
	f := newOrderFixture(t)
	owner, member := "oowner@test.com", "omember@test.com"

	// Anonymous -> 401.
	if code, _ := ordRequest(t, f, "", "POST", "/api/v1/events/"+f.eventID+"/orders", `{"ticketTypeId":"`+f.freeID+`","quantity":1}`); code != http.StatusUnauthorized {
		t.Fatalf("anon: got %d", code)
	}
	// Bad payloads.
	if code, _ := ordRequest(t, f, member, "POST", "/api/v1/events/"+f.eventID+"/orders", `{"ticketTypeId":"bogus","quantity":1}`); code != http.StatusBadRequest {
		t.Fatalf("bad type id: got %d", code)
	}
	if code, _ := ordRequest(t, f, member, "POST", "/api/v1/events/"+f.eventID+"/orders", `{"ticketTypeId":"`+f.freeID+`","quantity":0}`); code != http.StatusBadRequest {
		t.Fatalf("zero qty: got %d", code)
	}
	// Free order confirms immediately.
	code, body := ordRequest(t, f, member, "POST", "/api/v1/events/"+f.eventID+"/orders",
		`{"ticketTypeId":"`+f.freeID+`","quantity":2,"idempotencyKey":"m-1"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: got %d (%s)", code, body)
	}
	var created orderdto.Order
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Status != "CONFIRMED" || created.Quantity != 2 {
		t.Fatalf("unexpected order: %+v", created)
	}
	// Replay same key returns original.
	code, body = ordRequest(t, f, member, "POST", "/api/v1/events/"+f.eventID+"/orders",
		`{"ticketTypeId":"`+f.freeID+`","quantity":2,"idempotencyKey":"m-1"}`)
	var replayed orderdto.Order
	if err := json.Unmarshal(body, &replayed); err != nil || code != http.StatusCreated || replayed.ID != created.ID {
		t.Fatalf("replay: %d %+v %v", code, replayed, err)
	}
	// Priced order waits for payment.
	code, body = ordRequest(t, f, member, "POST", "/api/v1/events/"+f.eventID+"/orders",
		`{"ticketTypeId":"`+f.paidID+`","quantity":1}`)
	if code != http.StatusCreated {
		t.Fatalf("priced: got %d (%s)", code, body)
	}
	var paid orderdto.Order
	if err := json.Unmarshal(body, &paid); err != nil || paid.Status != "PENDING_PAYMENT" {
		t.Fatalf("priced order: %+v %v", paid, err)
	}
	// History shows both.
	code, body = ordRequest(t, f, member, "GET", "/api/v1/orders/mine", "")
	var page orderdto.OrdersPage
	if err := json.Unmarshal(body, &page); err != nil || code != http.StatusOK || page.Total != 2 {
		t.Fatalf("history: %d %+v %v", code, page, err)
	}
	// Get own order; owner sees via support path too.
	code, _ = ordRequest(t, f, member, "GET", "/api/v1/orders/"+created.ID, "")
	if code != http.StatusOK {
		t.Fatalf("get own: got %d", code)
	}
	code, _ = ordRequest(t, f, owner, "GET", "/api/v1/orders/"+created.ID, "")
	if code != http.StatusOK {
		t.Fatalf("admin get: got %d", code)
	}
	// Cancel own order.
	code, body = ordRequest(t, f, member, "POST", "/api/v1/orders/"+created.ID+"/cancel", "")
	if code != http.StatusOK {
		t.Fatalf("cancel: got %d (%s)", code, body)
	}
	var cancelled orderdto.Order
	if err := json.Unmarshal(body, &cancelled); err != nil || cancelled.Status != "CANCELLED" {
		t.Fatalf("cancelled: %+v %v", cancelled, err)
	}
	_ = owner
}
