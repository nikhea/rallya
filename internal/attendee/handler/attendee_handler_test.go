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

	attendee "github.com/nikhea/rallya/internal/attendee"
	attendeedto "github.com/nikhea/rallya/internal/attendee/dto"
	"github.com/nikhea/rallya/internal/attendee/handler"
	attendeemodel "github.com/nikhea/rallya/internal/attendee/model"
	"github.com/nikhea/rallya/internal/attendee/repository"
	"github.com/nikhea/rallya/internal/attendee/service"
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
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
)

func init() { gin.SetMode(gin.TestMode) }

type attendeeFixture struct {
	router *gin.Engine
	tokens map[string]string
}

func newAttendeeFixture(t *testing.T) *attendeeFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	for _, m := range [][]any{
		orgmodel.AllModels(), eventmodel.AllModels(), attendeemodel.AllModels(),
	} {
		_ = m
	}
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate org: %v", err)
	}
	if err := db.AutoMigrate(eventmodel.AllModels()...); err != nil {
		t.Fatalf("migrate events: %v", err)
	}
	if err := db.AutoMigrate(attendeemodel.AllModels()...); err != nil {
		t.Fatalf("migrate attendees: %v", err)
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

	attRepo := repository.NewAttendeeRepository(db)
	attSvc := service.NewAttendeeService(attRepo, authSvc, eventSvc, orgSvc)
	attHandler := handler.NewHandler(attSvc)

	r := gin.New()
	attendee.RegisterRoutes(r.Group("/api/v1"), attHandler, authRepo, orgRepo, e)

	f := &attendeeFixture{router: r, tokens: map[string]string{}}
	owner := mkAttUser(t, authRepo, authSvc, "aowner@test.com")
	mkAttUser(t, authRepo, authSvc, "amember@test.com")
	mkAttUser(t, authRepo, authSvc, "astranger@test.com")
	for _, email := range []string{"aowner@test.com", "amember@test.com", "astranger@test.com"} {
		pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		f.tokens[email] = pair.AccessToken
	}
	if _, err := orgSvc.CreateOrg(owner, "Acme", "acme", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := orgSvc.AddMember(owner, "acme", "amember@test.com", orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ev, err := eventSvc.CreateEvent(owner, "acme", eventservice.CreateInput{Title: "Fest"})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	f.tokens["eventID"] = ev.ID
	return f
}

func mkAttUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email string) uuid.UUID {
	t.Helper()
	name := "A"
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

func attRequest(t *testing.T, f *attendeeFixture, email, method, path, body string) (int, []byte) {
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

func TestHTTPAttendeeRosterAndManual(t *testing.T) {
	f := newAttendeeFixture(t)
	owner, member, stranger := "aowner@test.com", "amember@test.com", "astranger@test.com"

	// Anonymous roster -> 401.
	if code, _ := attRequest(t, f, "", "GET", "/api/v1/orgs/acme/events/fest/attendees", ""); code != http.StatusUnauthorized {
		t.Fatalf("anon: got %d", code)
	}
	// Stranger roster -> 404 stealth.
	if code, _ := attRequest(t, f, stranger, "GET", "/api/v1/orgs/acme/events/fest/attendees", ""); code != http.StatusNotFound {
		t.Fatalf("stranger: got %d", code)
	}
	// Member roster read -> 403 (attendee:read is ADMIN+).
	if code, _ := attRequest(t, f, member, "GET", "/api/v1/orgs/acme/events/fest/attendees", ""); code != http.StatusForbidden {
		t.Fatalf("member roster: got %d", code)
	}
	// Member manual add -> 403.
	if code, _ := attRequest(t, f, member, "POST", "/api/v1/orgs/acme/events/fest/attendees", `{"email":"w@test.com"}`); code != http.StatusForbidden {
		t.Fatalf("member add: got %d", code)
	}
	// Owner manual add -> 201.
	code, body := attRequest(t, f, owner, "POST", "/api/v1/orgs/acme/events/fest/attendees", `{"email":"walkin@test.com","name":"Walk In"}`)
	if code != http.StatusCreated {
		t.Fatalf("add: got %d (%s)", code, body)
	}
	var added attendeedto.Attendee
	if err := json.Unmarshal(body, &added); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if added.Email != "walkin@test.com" || added.OrderID != nil {
		t.Fatalf("unexpected row: %+v", added)
	}
	// Bad email -> 400.
	if code, _ := attRequest(t, f, owner, "POST", "/api/v1/orgs/acme/events/fest/attendees", `{"email":"nope"}`); code != http.StatusBadRequest {
		t.Fatalf("bad email: got %d", code)
	}
	// Roster lists it.
	code, body = attRequest(t, f, owner, "GET", "/api/v1/orgs/acme/events/fest/attendees", "")
	var page attendeedto.AttendeesPage
	if err := json.Unmarshal(body, &page); err != nil || code != http.StatusOK || page.Total != 1 {
		t.Fatalf("roster: %d %+v %v", code, page, err)
	}
	// Correct name.
	code, body = attRequest(t, f, owner, "PATCH", "/api/v1/orgs/acme/events/fest/attendees/"+added.ID, `{"name":"W. In"}`)
	if code != http.StatusOK {
		t.Fatalf("correct: got %d (%s)", code, body)
	}
	var fixed attendeedto.Attendee
	if err := json.Unmarshal(body, &fixed); err != nil || fixed.Name == nil || *fixed.Name != "W. In" {
		t.Fatalf("fixed: %+v %v", fixed, err)
	}
}

func TestHTTPAttendeeMine(t *testing.T) {
	f := newAttendeeFixture(t)
	owner, stranger := "aowner@test.com", "astranger@test.com"

	// Owner has no rows yet (manual add used a non-account email).
	code, body := attRequest(t, f, owner, "GET", "/api/v1/attendees/mine", "")
	var page attendeedto.AttendeesPage
	if err := json.Unmarshal(body, &page); err != nil || code != http.StatusOK || page.Total != 0 {
		t.Fatalf("mine: %d %+v %v", code, page, err)
	}
	// Anonymous -> 401.
	if code, _ := attRequest(t, f, "", "GET", "/api/v1/attendees/mine", ""); code != http.StatusUnauthorized {
		t.Fatalf("anon: got %d", code)
	}
	// Stranger's row is invisible to owner (stealth 404 on direct get).
	_, body = attRequest(t, f, owner, "POST", "/api/v1/orgs/acme/events/fest/attendees", `{"email":"solo@test.com"}`)
	var added attendeedto.Attendee
	if err := json.Unmarshal(body, &added); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, _ := attRequest(t, f, stranger, "GET", "/api/v1/attendees/"+added.ID, ""); code != http.StatusNotFound {
		t.Fatalf("stranger get: got %d", code)
	}
	// Owner cancel of own row... owner has none; create via manual with owner email then cancel.
	code, body = attRequest(t, f, owner, "POST", "/api/v1/orgs/acme/events/fest/attendees", `{"email":"aowner@test.com"}`)
	if code != http.StatusCreated {
		t.Fatalf("add self: got %d (%s)", code, body)
	}
	// Manual rows carry no user link: owner cannot cancel via mine path (stealth 404).
	var selfrow attendeedto.Attendee
	if err := json.Unmarshal(body, &selfrow); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, _ := attRequest(t, f, owner, "POST", "/api/v1/attendees/"+selfrow.ID+"/cancel", ""); code != http.StatusNotFound {
		t.Fatalf("cancel unlinked: got %d", code)
	}
}
