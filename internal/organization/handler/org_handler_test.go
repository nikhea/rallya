package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	authdto "github.com/nikhea/rallya/internal/auth/dto"
	authmodel "github.com/nikhea/rallya/internal/auth/model"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	authservice "github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/notification/jobs"
	organization "github.com/nikhea/rallya/internal/organization"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	"github.com/nikhea/rallya/internal/organization/handler"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/repository"
	"github.com/nikhea/rallya/internal/organization/service"
)

func init() { gin.SetMode(gin.TestMode) }

type orgFixture struct {
	router  *gin.Engine
	orgSvc  *service.OrgService
	fake    *jobs.FakeEnqueuer
	tokens  map[string]string // email -> access token
	userIDs map[string]string // email -> user id
}

func newOrgFixture(t *testing.T) *orgFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate org: %v", err)
	}
	orgRepo := repository.NewOrgRepository(db)
	orgSvc := service.NewOrgService(orgRepo, authSvc)
	fake := &jobs.FakeEnqueuer{}
	orgSvc.SetEnqueuer(fake)
	orgHandler := handler.NewHandler(orgSvc)

	r := gin.New()
	organization.RegisterRoutes(r.Group("/api/v1/orgs"), orgHandler, orgRepo, authRepo)

	f := &orgFixture{
		router: r, orgSvc: orgSvc, fake: fake,
		tokens: map[string]string{}, userIDs: map[string]string{},
	}
	for _, u := range []struct{ email, first string }{
		{"owner@test.com", "Owner"},
		{"member@test.com", "Member"},
		{"stranger@test.com", "Stranger"},
	} {
		f.mustUser(t, db, authRepo, authSvc, u.email, u.first)
	}
	return f
}

// mustUser registers, verifies, and logs in a user via the real auth stack.
func (f *orgFixture) mustUser(t *testing.T, db *gorm.DB, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email, first string) {
	t.Helper()
	name := first
	if err := authSvc.Register(authdto.Register{
		Email: email, Password: "Str0ngP@ssw0rd!", FirstName: &name,
	}); err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	u, err := authRepo.GetUserByEmail(email)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	raw := "verify-" + email
	if err := authRepo.CreateEmailVerification(nil, &authmodel.EmailVerification{
		UserID: u.ID, TokenHash: token.HashToken(raw), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed verification: %v", err)
	}
	if err := authSvc.VerifyEmail(raw); err != nil {
		t.Fatalf("verify: %v", err)
	}
	pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authservice.LoginContext{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	f.tokens[email] = pair.AccessToken
	f.userIDs[email] = u.ID.String()
	_ = db
}

func orgRequest(t *testing.T, f *orgFixture, method, path, body, email string) *httptest.ResponseRecorder {
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

func TestHTTPOrgCRUDAndGuards(t *testing.T) {
	f := newOrgFixture(t)

	// Anonymous -> 401.
	if w := orgRequest(t, f, "POST", "/api/v1/orgs", `{"name":"Acme"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: got %d", w.Code)
	}
	// Create -> 201 owner.
	w := orgRequest(t, f, "POST", "/api/v1/orgs", `{"name":"Acme Inc"}`, "owner@test.com")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d (%s)", w.Code, w.Body.String())
	}
	var created orgdto.Org
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Slug != "acme-inc" || created.Role != "OWNER" {
		t.Fatalf("unexpected org: %+v", created)
	}
	// Duplicate slug -> 409.
	if w := orgRequest(t, f, "POST", "/api/v1/orgs", `{"name":"Other","slug":"acme-inc"}`, "owner@test.com"); w.Code != http.StatusConflict {
		t.Fatalf("dup slug: got %d", w.Code)
	}
	// Stranger get -> 404 stealth.
	if w := orgRequest(t, f, "GET", "/api/v1/orgs/acme-inc", "", "stranger@test.com"); w.Code != http.StatusNotFound {
		t.Fatalf("stranger get: got %d", w.Code)
	}
	// Owner get by slug + by id.
	if w := orgRequest(t, f, "GET", "/api/v1/orgs/acme-inc", "", "owner@test.com"); w.Code != http.StatusOK {
		t.Fatalf("get: got %d (%s)", w.Code, w.Body.String())
	}
	if w := orgRequest(t, f, "GET", "/api/v1/orgs/"+created.ID, "", "owner@test.com"); w.Code != http.StatusOK {
		t.Fatalf("get by id: got %d", w.Code)
	}
	// Add member directly.
	w = orgRequest(t, f, "POST", "/api/v1/orgs/acme-inc/members", `{"email":"member@test.com","role":"ADMIN"}`, "owner@test.com")
	if w.Code != http.StatusCreated {
		t.Fatalf("add member: got %d (%s)", w.Code, w.Body.String())
	}
	// Member (admin) updates org.
	w = orgRequest(t, f, "PATCH", "/api/v1/orgs/acme-inc", `{"name":"Acme Corp"}`, "member@test.com")
	if w.Code != http.StatusOK {
		t.Fatalf("admin update: got %d (%s)", w.Code, w.Body.String())
	}
	// Member cannot delete (admin, not owner) -> 403.
	if w := orgRequest(t, f, "DELETE", "/api/v1/orgs/acme-inc", "", "member@test.com"); w.Code != http.StatusForbidden {
		t.Fatalf("admin delete: got %d", w.Code)
	}
	// List members as member.
	w = orgRequest(t, f, "GET", "/api/v1/orgs/acme-inc/members", "", "member@test.com")
	if w.Code != http.StatusOK {
		t.Fatalf("list members: got %d (%s)", w.Code, w.Body.String())
	}
	var page orgdto.MembersPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Total != 2 {
		t.Fatalf("members page: %+v %v", page, err)
	}
	// Demote admin to member via owner, then admin-only route -> 403.
	w = orgRequest(t, f, "PATCH", "/api/v1/orgs/acme-inc/members/"+f.userIDs["member@test.com"], `{"role":"MEMBER"}`, "owner@test.com")
	if w.Code != http.StatusOK {
		t.Fatalf("demote: got %d (%s)", w.Code, w.Body.String())
	}
	if w := orgRequest(t, f, "POST", "/api/v1/orgs/acme-inc/invites", `{"email":"x@test.com"}`, "member@test.com"); w.Code != http.StatusForbidden {
		t.Fatalf("member invite: got %d", w.Code)
	}
}

func TestHTTPInviteAcceptDecline(t *testing.T) {
	f := newOrgFixture(t)

	w := orgRequest(t, f, "POST", "/api/v1/orgs", `{"name":"Acme"}`, "owner@test.com")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d", w.Code)
	}
	// Invite stranger (no account needed to invite).
	w = orgRequest(t, f, "POST", "/api/v1/orgs/acme/invites", `{"email":"stranger@test.com","role":"MEMBER"}`, "owner@test.com")
	if w.Code != http.StatusCreated {
		t.Fatalf("invite: got %d (%s)", w.Code, w.Body.String())
	}
	if n := len(f.fake.OfKind("send_org_invite_email")); n != 1 {
		t.Fatalf("expected 1 invite job, got %d", n)
	}
	// Stranger accepts (account exists from fixture).
	w = orgRequest(t, f, "POST", "/api/v1/orgs/invites/accept", `{"token":"bogus"}`, "stranger@test.com")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bogus accept: got %d", w.Code)
	}
	raw := lastInviteToken(t, f)
	w = orgRequest(t, f, "POST", "/api/v1/orgs/invites/accept", `{"token":"`+raw+`"}`, "stranger@test.com")
	if w.Code != http.StatusOK {
		t.Fatalf("accept: got %d (%s)", w.Code, w.Body.String())
	}
	// Now a member: get works.
	if w := orgRequest(t, f, "GET", "/api/v1/orgs/acme", "", "stranger@test.com"); w.Code != http.StatusOK {
		t.Fatalf("get after accept: got %d", w.Code)
	}
	// Invite + decline flow for a fresh invite.
	w = orgRequest(t, f, "POST", "/api/v1/orgs/acme/invites", `{"email":"member@test.com"}`, "owner@test.com")
	if w.Code != http.StatusCreated {
		t.Fatalf("invite2: got %d (%s)", w.Code, w.Body.String())
	}
	raw2 := lastInviteToken(t, f)
	w = orgRequest(t, f, "POST", "/api/v1/orgs/invites/decline", `{"token":"`+raw2+`"}`, "member@test.com")
	if w.Code != http.StatusOK {
		t.Fatalf("decline: got %d (%s)", w.Code, w.Body.String())
	}
	// Declined invite cannot be accepted.
	w = orgRequest(t, f, "POST", "/api/v1/orgs/invites/accept", `{"token":"`+raw2+`"}`, "member@test.com")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("accept declined: got %d", w.Code)
	}
}

// lastInviteToken extracts the raw token from the latest enqueued invite job.
func lastInviteToken(t *testing.T, f *orgFixture) string {
	t.Helper()
	got := f.fake.OfKind("send_org_invite_email")
	if len(got) == 0 {
		t.Fatal("no invite jobs")
	}
	args, ok := got[len(got)-1].(jobs.SendOrgInviteEmailArgs)
	if !ok {
		t.Fatalf("unexpected args type %T", got[len(got)-1])
	}
	link := args.InviteLink
	for i := len(link) - 1; i >= 0; i-- {
		if link[i] == '=' {
			return link[i+1:]
		}
	}
	t.Fatalf("no token in link %q", link)
	return ""
}

func TestHTTPOrgEdgeCases(t *testing.T) {
	f := newOrgFixture(t)

	w := orgRequest(t, f, "POST", "/api/v1/orgs", `{"name":"Acme"}`, "owner@test.com")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d", w.Code)
	}
	// Garbage id format -> 404.
	if w := orgRequest(t, f, "GET", "/api/v1/orgs/!!!", "", "owner@test.com"); w.Code != http.StatusNotFound {
		t.Fatalf("bad id: got %d", w.Code)
	}
	// Add member + invite two users.
	if w := orgRequest(t, f, "POST", "/api/v1/orgs/acme/members", `{"email":"member@test.com"}`, "owner@test.com"); w.Code != http.StatusCreated {
		t.Fatalf("add: got %d (%s)", w.Code, w.Body.String())
	}
	if w := orgRequest(t, f, "POST", "/api/v1/orgs/acme/invites", `{"email":"a@test.com"}`, "owner@test.com"); w.Code != http.StatusCreated {
		t.Fatalf("invite a: got %d", w.Code)
	}
	if w := orgRequest(t, f, "POST", "/api/v1/orgs/acme/invites", `{"email":"b@test.com","role":"ADMIN"}`, "owner@test.com"); w.Code != http.StatusCreated {
		t.Fatalf("invite b: got %d", w.Code)
	}
	// List invites.
	w = orgRequest(t, f, "GET", "/api/v1/orgs/acme/invites", "", "owner@test.com")
	if w.Code != http.StatusOK {
		t.Fatalf("list invites: got %d", w.Code)
	}
	var invPage orgdto.InvitesPage
	if err := json.Unmarshal(w.Body.Bytes(), &invPage); err != nil || invPage.Total != 2 {
		t.Fatalf("invites page: %+v %v", invPage, err)
	}
	// Revoke one.
	w = orgRequest(t, f, "DELETE", "/api/v1/orgs/acme/invites/"+invPage.Items[0].ID, "", "owner@test.com")
	if w.Code != http.StatusOK {
		t.Fatalf("revoke: got %d (%s)", w.Code, w.Body.String())
	}
	w = orgRequest(t, f, "GET", "/api/v1/orgs/acme/invites", "", "owner@test.com")
	var invPage2 orgdto.InvitesPage
	if err := json.Unmarshal(w.Body.Bytes(), &invPage2); err != nil || invPage2.Total != 1 {
		t.Fatalf("invites after revoke: %+v %v", invPage2, err)
	}
	// Pagination on members: 2 members, perPage=1&page=2 -> 1 item.
	w = orgRequest(t, f, "GET", "/api/v1/orgs/acme/members?perPage=1&page=2", "", "owner@test.com")
	var memPage orgdto.MembersPage
	if err := json.Unmarshal(w.Body.Bytes(), &memPage); err != nil || memPage.Total != 2 || len(memPage.Items) != 1 {
		t.Fatalf("members page2: %+v %v", memPage, err)
	}
	// Self-leave as member.
	w = orgRequest(t, f, "DELETE", "/api/v1/orgs/acme/members/"+f.userIDs["member@test.com"], "", "member@test.com")
	if w.Code != http.StatusOK {
		t.Fatalf("leave: got %d (%s)", w.Code, w.Body.String())
	}
	if w := orgRequest(t, f, "GET", "/api/v1/orgs/acme", "", "member@test.com"); w.Code != http.StatusNotFound {
		t.Fatalf("get after leave: got %d", w.Code)
	}
	// Last owner demote via HTTP -> 409.
	ownerID := f.userIDs["owner@test.com"]
	w = orgRequest(t, f, "PATCH", "/api/v1/orgs/acme/members/"+ownerID, `{"role":"ADMIN"}`, "owner@test.com")
	if w.Code != http.StatusConflict {
		t.Fatalf("demote last owner: got %d (%s)", w.Code, w.Body.String())
	}
	// Delete org -> 200, then gone.
	if w := orgRequest(t, f, "DELETE", "/api/v1/orgs/acme", "", "owner@test.com"); w.Code != http.StatusOK {
		t.Fatalf("delete: got %d", w.Code)
	}
	if w := orgRequest(t, f, "GET", "/api/v1/orgs/acme", "", "owner@test.com"); w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: got %d", w.Code)
	}
}
