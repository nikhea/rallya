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
	event "github.com/nikhea/rallya/internal/event"
	"github.com/nikhea/rallya/internal/event/cover"
	eventhandler "github.com/nikhea/rallya/internal/event/handler"
	eventmodel "github.com/nikhea/rallya/internal/event/model"
	"github.com/nikhea/rallya/internal/event/repository"
	eventservice "github.com/nikhea/rallya/internal/event/service"
	"github.com/nikhea/rallya/internal/iam"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
	ticketing "github.com/nikhea/rallya/internal/ticketing"
	ticketdto "github.com/nikhea/rallya/internal/ticketing/dto"
	"github.com/nikhea/rallya/internal/ticketing/handler"
	ticketmodel "github.com/nikhea/rallya/internal/ticketing/model"
	ticketrepository "github.com/nikhea/rallya/internal/ticketing/repository"
	ticketservice "github.com/nikhea/rallya/internal/ticketing/service"
)

func init() { gin.SetMode(gin.TestMode) }

type ticketFixture struct {
	router *gin.Engine
	tokens map[string]string
}

func newTicketFixture(t *testing.T) *ticketFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	for _, migrate := range []func() error{
		func() error { return db.AutoMigrate(orgmodel.AllModels()...) },
		func() error { return db.AutoMigrate(eventmodel.AllModels()...) },
		func() error { return db.AutoMigrate(ticketmodel.AllModels()...) },
		func() error { return db.AutoMigrate(&gormadapter.CasbinRule{}) },
	} {
		if err := migrate(); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	e, err := iam.NewEnforcer(db)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}

	orgRepo := orgrepository.NewOrgRepository(db)
	orgSvc := orgservice.NewOrgService(orgRepo, authSvc)
	orgSvc.SetGroupSyncer(iam.NewMembershipSyncer(e))
	orgSvc.SetPolicySeeder(iam.NewOrgPolicySeeder(e))

	eventRepo := repository.NewEventRepository(db)
	eventSvc := eventservice.NewEventService(eventRepo, orgSvc)
	eventSvc.SetEnqueuer(&jobs.FakeEnqueuer{})
	eventSvc.SetCoverStorage(cover.NewLocal(t.TempDir()))
	eventHandler := eventhandler.NewHandler(eventSvc)

	ticketRepo := ticketrepository.NewTicketRepository(db)
	ticketSvc := ticketservice.NewTicketService(ticketRepo, ticketservice.NewEventAdapter(eventSvc))
	ticketHandler := handler.NewHandler(ticketSvc)

	r := gin.New()
	event.RegisterRoutes(r.Group("/api/v1"), eventHandler, authRepo, orgRepo, e)
	ticketing.RegisterRoutes(r.Group("/api/v1"), ticketHandler, authRepo, orgRepo, e)

	f := &ticketFixture{router: r, tokens: map[string]string{}}
	owner := mkTicketUser(t, authRepo, authSvc, "towner@test.com")
	mkTicketUser(t, authRepo, authSvc, "tmember@test.com")
	mkTicketUser(t, authRepo, authSvc, "tout@test.com")
	for _, email := range []string{"towner@test.com", "tmember@test.com", "tout@test.com"} {
		pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		f.tokens[email] = pair.AccessToken
	}
	if _, err := orgSvc.CreateOrg(owner, "Acme", "acme", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := orgSvc.AddMember(owner, "acme", "tmember@test.com", "MEMBER"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := eventSvc.CreateEvent(owner, "acme", eventservice.CreateInput{Title: "Fest", Capacity: intPtrT(100)}); err != nil {
		t.Fatalf("create event: %v", err)
	}
	// Publish the event so public ticket reads work.
	if _, err := eventSvc.Publish(owner, "acme", "fest"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return f
}

func intPtrT(n int) *int { return &n }

func mkTicketUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email string) uuid.UUID {
	t.Helper()
	name := "T"
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

func tkRequest(t *testing.T, f *ticketFixture, email, method, path, body string) (int, []byte) {
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

func TestHTTPTicketCRUDGuards(t *testing.T) {
	f := newTicketFixture(t)
	owner, member, stranger := "towner@test.com", "tmember@test.com", "tout@test.com"

	// Anonymous -> 401.
	if code, _ := tkRequest(t, f, "", "POST", "/api/v1/orgs/acme/events/fest/tickets", `{"name":"GA","quantityTotal":10}`); code != http.StatusUnauthorized {
		t.Fatalf("anon: got %d", code)
	}
	// Stranger (non-member) -> 404 stealth.
	if code, _ := tkRequest(t, f, stranger, "POST", "/api/v1/orgs/acme/events/fest/tickets", `{"name":"GA","quantityTotal":10}`); code != http.StatusNotFound {
		t.Fatalf("stranger: got %d", code)
	}
	// Member create -> 403 (ticket:create is ADMIN+).
	if code, _ := tkRequest(t, f, member, "POST", "/api/v1/orgs/acme/events/fest/tickets", `{"name":"GA","quantityTotal":10}`); code != http.StatusForbidden {
		t.Fatalf("member create: got %d", code)
	}
	// Owner create -> 201 (event starts DRAFT; fest fixture event has no capacity cap).
	code, body := tkRequest(t, f, owner, "POST", "/api/v1/orgs/acme/events/fest/tickets",
		`{"name":"General Admission","priceCents":2500,"currency":"USD","quantityTotal":50,"maxPerOrder":4}`)
	if code != http.StatusCreated {
		t.Fatalf("create: got %d (%s)", code, body)
	}
	var created ticketdto.TicketType
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Status != "DRAFT" || created.Currency != "USD" || created.Remaining != 50 || created.ForSale {
		t.Fatalf("unexpected type: %+v", created)
	}
	// Member list sees drafts with forSale=false.
	code, body = tkRequest(t, f, member, "GET", "/api/v1/orgs/acme/events/fest/tickets", "")
	var page ticketdto.TicketsPage
	if err := json.Unmarshal(body, &page); err != nil || code != http.StatusOK || page.Total != 1 {
		t.Fatalf("member list: %d %+v %v", code, page, err)
	}
	if page.Items[0].ForSale {
		t.Fatal("draft must not be for sale")
	}
	// Activate + public list shows it for sale.
	if code, _ := tkRequest(t, f, owner, "POST", "/api/v1/orgs/acme/events/fest/tickets/"+created.ID+"/activate", ""); code != http.StatusOK {
		t.Fatalf("activate: got %d", code)
	}
	code, body = tkRequest(t, f, "", "GET", "/api/v1/events/"+eventIDOf(t, f, owner)+"/tickets", "")
	if code != http.StatusOK {
		t.Fatalf("public list: got %d (%s)", code, body)
	}
	var pub ticketdto.TicketsPage
	if err := json.Unmarshal(body, &pub); err != nil || pub.Total != 1 || !pub.Items[0].ForSale {
		t.Fatalf("public page: %+v %v", pub, err)
	}
	// Delete with zero sales works.
	if code, _ := tkRequest(t, f, owner, "DELETE", "/api/v1/orgs/acme/events/fest/tickets/"+created.ID, ""); code != http.StatusOK {
		t.Fatalf("delete: got %d", code)
	}
	// Unknown event -> 404 (stealth via org event resolution).
	if code, _ := tkRequest(t, f, owner, "GET", "/api/v1/orgs/acme/events/ghost/tickets", ""); code != http.StatusNotFound {
		t.Fatalf("ghost event: got %d", code)
	}
}

func eventIDOf(t *testing.T, f *ticketFixture, owner string) string {
	t.Helper()
	code, body := tkRequest(t, f, owner, "GET", "/api/v1/orgs/acme/events", "")
	if code != http.StatusOK {
		t.Fatalf("list events: %d", code)
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil || len(page.Items) == 0 {
		t.Fatalf("decode events: %v", err)
	}
	return page.Items[0].ID
}
