package handler_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
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
	eventdto "github.com/nikhea/rallya/internal/event/dto"
	"github.com/nikhea/rallya/internal/event/handler"
	eventmodel "github.com/nikhea/rallya/internal/event/model"
	"github.com/nikhea/rallya/internal/event/repository"
	"github.com/nikhea/rallya/internal/event/service"
	"github.com/nikhea/rallya/internal/iam"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
)

func init() { gin.SetMode(gin.TestMode) }

type eventFixture struct {
	router   *gin.Engine
	eventSvc *service.EventService
	orgSvc   *orgservice.OrgService
	fake     *jobs.FakeEnqueuer
	tokens   map[string]string
	userIDs  map[string]uuid.UUID
}

func newEventFixture(t *testing.T) *eventFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate org: %v", err)
	}
	if err := db.AutoMigrate(eventmodel.AllModels()...); err != nil {
		t.Fatalf("migrate events: %v", err)
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

	eventRepo := repository.NewEventRepository(db)
	eventSvc := service.NewEventService(eventRepo, orgSvc)
	fake := &jobs.FakeEnqueuer{}
	eventSvc.SetEnqueuer(fake)
	uploads := t.TempDir()
	eventSvc.SetCoverStorage(cover.NewLocal(uploads))
	eventHandler := handler.NewHandler(eventSvc)

	r := gin.New()
	r.Static("/uploads", uploads)
	event.RegisterRoutes(r.Group("/api/v1"), eventHandler, authRepo, orgRepo, e)

	f := &eventFixture{
		router: r, eventSvc: eventSvc, orgSvc: orgSvc,
		fake: fake, tokens: map[string]string{}, userIDs: map[string]uuid.UUID{},
	}
	for _, u := range []struct{ email, first string }{
		{"eowner@test.com", "Owner"},
		{"emember@test.com", "Member"},
		{"estranger@test.com", "Stranger"},
	} {
		f.mustUser(t, authRepo, authSvc, u.email, u.first)
	}
	if _, err := orgSvc.CreateOrg(f.userIDs["eowner@test.com"], "Acme", "acme", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := orgSvc.AddMember(f.userIDs["eowner@test.com"], "acme", "emember@test.com", "MEMBER"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return f
}

func (f *eventFixture) mustUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email, first string) {
	t.Helper()
	name := first
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
	pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	f.tokens[email] = pair.AccessToken
	f.userIDs[email] = u.ID
}

func evRequest(t *testing.T, f *eventFixture, method, path, body, email string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func evUpload(t *testing.T, f *eventFixture, path, email, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("form: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func TestHTTPEventCRUDGuards(t *testing.T) {
	f := newEventFixture(t)

	// Anonymous create -> 401.
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events", `{"title":"X"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anon: got %d", w.Code)
	}
	// Owner create -> 201 auto slug.
	w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events",
		`{"title":"Summer Fest","venue":"Park","startsAt":"2026-08-01T10:00:00Z","endsAt":"2026-08-03T22:00:00Z","capacity":500}`, "eowner@test.com")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d (%s)", w.Code, w.Body.String())
	}
	var created eventdto.Event
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Slug != "summer-fest" || created.Status != "DRAFT" || created.Capacity == nil || *created.Capacity != 500 {
		t.Fatalf("unexpected event: %+v", created)
	}
	// Member create -> 403 (event:create is ADMIN+).
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events", `{"title":"Spam"}`, "emember@test.com"); w.Code != http.StatusForbidden {
		t.Fatalf("member create: got %d", w.Code)
	}
	// Bad dates -> 400.
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events",
		`{"title":"Bad","startsAt":"2026-08-03T10:00:00Z","endsAt":"2026-08-01T10:00:00Z"}`, "eowner@test.com"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad dates: got %d", w.Code)
	}
	// Draft invisible publicly, stranger 404 in org.
	if w := evRequest(t, f, "GET", "/api/v1/events/"+created.ID, "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("public draft: got %d", w.Code)
	}
	if w := evRequest(t, f, "GET", "/api/v1/orgs/acme/events/"+created.Slug, "", "estranger@test.com"); w.Code != http.StatusNotFound {
		t.Fatalf("stranger draft: got %d", w.Code)
	}
	// Member reads draft by slug.
	if w := evRequest(t, f, "GET", "/api/v1/orgs/acme/events/"+created.Slug, "", "emember@test.com"); w.Code != http.StatusOK {
		t.Fatalf("member get: got %d (%s)", w.Code, w.Body.String())
	}
	// Member update -> 403; owner update -> 200.
	if w := evRequest(t, f, "PATCH", "/api/v1/orgs/acme/events/"+created.Slug, `{"title":"Nope"}`, "emember@test.com"); w.Code != http.StatusForbidden {
		t.Fatalf("member update: got %d", w.Code)
	}
	w = evRequest(t, f, "PATCH", "/api/v1/orgs/acme/events/"+created.Slug, `{"title":"Summer Fest 2026"}`, "eowner@test.com")
	if w.Code != http.StatusOK {
		t.Fatalf("update: got %d (%s)", w.Code, w.Body.String())
	}
	// Member delete -> 403; owner delete -> 200 then 404.
	if w := evRequest(t, f, "DELETE", "/api/v1/orgs/acme/events/"+created.Slug, "", "emember@test.com"); w.Code != http.StatusForbidden {
		t.Fatalf("member delete: got %d", w.Code)
	}
	if w := evRequest(t, f, "DELETE", "/api/v1/orgs/acme/events/"+created.Slug, "", "eowner@test.com"); w.Code != http.StatusOK {
		t.Fatalf("delete: got %d", w.Code)
	}
	if w := evRequest(t, f, "GET", "/api/v1/orgs/acme/events/"+created.Slug, "", "eowner@test.com"); w.Code != http.StatusNotFound {
		t.Fatalf("get deleted: got %d", w.Code)
	}
}

func TestHTTPPublishLifecycleAndDiscovery(t *testing.T) {
	f := newEventFixture(t)

	mk := func(title string) eventdto.Event {
		w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events", `{"title":"`+title+`","startsAt":"2026-08-01T10:00:00Z"}`, "eowner@test.com")
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: got %d", title, w.Code)
		}
		var e eventdto.Event
		if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return e
	}
	a := mk("Alpha Fest")
	_ = mk("Beta Fest")
	// Member publish -> 403.
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events/"+a.Slug+"/publish", "", "emember@test.com"); w.Code != http.StatusForbidden {
		t.Fatalf("member publish: got %d", w.Code)
	}
	// Owner publish -> 200 + announcement jobs to opted-in members.
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events/"+a.Slug+"/publish", "", "eowner@test.com"); w.Code != http.StatusOK {
		t.Fatalf("publish: got %d (%s)", w.Code, w.Body.String())
	}
	announced := f.fake.OfKind("send_event_published_email")
	if len(announced) != 2 {
		t.Fatalf("expected 2 announcement jobs (owner+member), got %d", len(announced))
	}
	// Opt member out -> next publish only notifies owner.
	if err := f.orgSvc.UpdateMyPreferences(f.userIDs["emember@test.com"], "acme", false); err != nil {
		t.Fatalf("opt out: %v", err)
	}
	bSlug := "beta-fest"
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events/"+bSlug+"/publish", "", "eowner@test.com"); w.Code != http.StatusOK {
		t.Fatalf("publish b: got %d", w.Code)
	}
	if n := len(f.fake.OfKind("send_event_published_email")); n != 3 {
		t.Fatalf("expected 3 total announcement jobs, got %d", n)
	}
	// Double publish -> 409 (invalid transition).
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events/"+a.Slug+"/publish", "", "eowner@test.com"); w.Code != http.StatusConflict {
		t.Fatalf("republish: got %d", w.Code)
	}
	// Public discovery: 2 published.
	w := evRequest(t, f, "GET", "/api/v1/events", "", "")
	var page eventdto.EventsPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Total != 2 {
		t.Fatalf("public list: %+v %v", page, err)
	}
	// Search narrows.
	w = evRequest(t, f, "GET", "/api/v1/events?q=alpha", "", "")
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Total != 1 {
		t.Fatalf("search: %+v %v", page, err)
	}
	// Public detail.
	w = evRequest(t, f, "GET", "/api/v1/events/"+a.ID, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("public detail: got %d", w.Code)
	}
	// Unpublish -> invisible publicly; cancel flow on the other.
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events/"+a.Slug+"/unpublish", "", "eowner@test.com"); w.Code != http.StatusOK {
		t.Fatalf("unpublish: got %d", w.Code)
	}
	if w := evRequest(t, f, "GET", "/api/v1/events/"+a.ID, "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unpublished public: got %d", w.Code)
	}
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events/"+bSlug+"/cancel", "", "eowner@test.com"); w.Code != http.StatusOK {
		t.Fatalf("cancel: got %d", w.Code)
	}
	if w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events/"+bSlug+"/publish", "", "eowner@test.com"); w.Code != http.StatusConflict {
		t.Fatalf("publish cancelled: got %d", w.Code)
	}
}

func TestHTTPUploadCover(t *testing.T) {
	f := newEventFixture(t)

	w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events", `{"title":"Cover Fest"}`, "eowner@test.com")
	var created eventdto.Event
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 100)...)
	// Member upload -> 403.
	if w := evUpload(t, f, "/api/v1/orgs/acme/events/"+created.Slug+"/cover", "emember@test.com", "cover.png", png); w.Code != http.StatusForbidden {
		t.Fatalf("member upload: got %d", w.Code)
	}
	// Spoofed content (text named .png) -> 400.
	if w := evUpload(t, f, "/api/v1/orgs/acme/events/"+created.Slug+"/cover", "eowner@test.com", "evil.png", []byte("not an image at all")); w.Code != http.StatusBadRequest {
		t.Fatalf("spoofed: got %d", w.Code)
	}
	// Valid upload -> 200 with URL.
	w = evUpload(t, f, "/api/v1/orgs/acme/events/"+created.Slug+"/cover", "eowner@test.com", "cover.png", png)
	if w.Code != http.StatusOK {
		t.Fatalf("upload: got %d (%s)", w.Code, w.Body.String())
	}
	var withCover eventdto.Event
	if err := json.Unmarshal(w.Body.Bytes(), &withCover); err != nil || withCover.CoverURL == nil {
		t.Fatalf("cover url: %+v %v", withCover, err)
	}
	// Served statically.
	w = evRequest(t, f, "GET", *withCover.CoverURL, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("serve cover: got %d", w.Code)
	}
	// Oversize -> 400.
	big := bytes.Repeat([]byte{0}, 6<<20)
	big[0], big[1], big[2], big[3] = 0x89, 'P', 'N', 'G'
	if w := evUpload(t, f, "/api/v1/orgs/acme/events/"+created.Slug+"/cover", "eowner@test.com", "big.png", big); w.Code != http.StatusBadRequest {
		t.Fatalf("oversize: got %d", w.Code)
	}
}

func evUploadMulti(t *testing.T, f *eventFixture, path, email string, files map[string][]byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, content := range files {
		part, err := mw.CreateFormFile("files", name)
		if err != nil {
			t.Fatalf("form: %v", err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func TestHTTPGalleryFlow(t *testing.T) {
	f := newEventFixture(t)

	w := evRequest(t, f, "POST", "/api/v1/orgs/acme/events", `{"title":"Gallery Fest"}`, "eowner@test.com")
	var created eventdto.Event
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 100)...)
	// Member multi-upload -> 403.
	if w := evUploadMulti(t, f, "/api/v1/orgs/acme/events/"+created.Slug+"/images", "emember@test.com",
		map[string][]byte{"a.png": png}); w.Code != http.StatusForbidden {
		t.Fatalf("member upload: got %d", w.Code)
	}
	// Owner multi-upload -> 201 with metadata.
	w = evUploadMulti(t, f, "/api/v1/orgs/acme/events/"+created.Slug+"/images", "eowner@test.com",
		map[string][]byte{"a.png": png, "b.png": png})
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: got %d (%s)", w.Code, w.Body.String())
	}
	var page eventdto.ImagesPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Total != 2 {
		t.Fatalf("page: %+v %v", page, err)
	}
	for _, img := range page.Items {
		if img.URL == "" || img.PublicID == "" {
			t.Fatalf("missing fields: %+v", img)
		}
	}
	// Auto-cover: event had none, first image cloned.
	w = evRequest(t, f, "GET", "/api/v1/orgs/acme/events/"+created.Slug, "", "eowner@test.com")
	var withCover eventdto.Event
	if err := json.Unmarshal(w.Body.Bytes(), &withCover); err != nil || withCover.CoverURL == nil {
		t.Fatalf("expected cloned cover: %+v %v", withCover, err)
	}
	// List as member -> 200.
	w = evRequest(t, f, "GET", "/api/v1/orgs/acme/events/"+created.Slug+"/images", "", "emember@test.com")
	var listed eventdto.ImagesPage
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || listed.Total != 2 {
		t.Fatalf("list: %+v %v", listed, err)
	}
	// Stranger list -> 404.
	if w := evRequest(t, f, "GET", "/api/v1/orgs/acme/events/"+created.Slug+"/images", "", "estranger@test.com"); w.Code != http.StatusNotFound {
		t.Fatalf("stranger list: got %d", w.Code)
	}
	// Empty upload -> 400.
	if w := evUploadMulti(t, f, "/api/v1/orgs/acme/events/"+created.Slug+"/images", "eowner@test.com", map[string][]byte{}); w.Code != http.StatusBadRequest {
		t.Fatalf("empty: got %d", w.Code)
	}
}
