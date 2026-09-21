package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	audit "github.com/nikhea/rallya/internal/audit"
	auditdto "github.com/nikhea/rallya/internal/audit/dto"
	"github.com/nikhea/rallya/internal/audit/handler"
	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditrepo "github.com/nikhea/rallya/internal/audit/repository"
	auditservice "github.com/nikhea/rallya/internal/audit/service"
	authdto "github.com/nikhea/rallya/internal/auth/dto"
	authmodel "github.com/nikhea/rallya/internal/auth/model"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	authservice "github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/iam"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
)

func init() { gin.SetMode(gin.TestMode) }

type auditFixture struct {
	router  *gin.Engine
	tokens  map[string]string
	orgID   string
	svc     *auditservice.AuditService
	authSvc *authservice.AuthService
	enf     *casbin.Enforcer
}

func newAuditFixture(t *testing.T) *auditFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate org: %v", err)
	}
	if err := db.AutoMigrate(auditmodel.AllModels()...); err != nil {
		t.Fatalf("migrate audit: %v", err)
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

	auditSvc := auditservice.NewAuditService(auditrepo.NewAuditRepository(db))
	orgSvc.SetAuditEmitter(auditSvc)

	r := gin.New()
	audit.RegisterRoutes(r.Group("/api/v1"), handler.NewHandler(auditSvc), authRepo, orgRepo, e)

	f := &auditFixture{router: r, tokens: map[string]string{}, svc: auditSvc, authSvc: authSvc, enf: e}
	owner := mkAuditUser(t, authRepo, authSvc, "wowner@test.com")
	mkAuditUser(t, authRepo, authSvc, "wmember@test.com")
	mkAuditUser(t, authRepo, authSvc, "wstranger@test.com")
	for _, email := range []string{"wowner@test.com", "wmember@test.com", "wstranger@test.com"} {
		pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		f.tokens[email] = pair.AccessToken
	}
	org, err := orgSvc.CreateOrg(owner, "Watch", "watch", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	f.orgID = org.ID
	if _, err := orgSvc.AddMember(owner, "watch", "wmember@test.com", orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return f
}

func mkAuditUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email string) uuid.UUID {
	t.Helper()
	name := "W"
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

func auditReq(t *testing.T, f *auditFixture, email, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestHTTPOrgAuditRead(t *testing.T) {
	f := newAuditFixture(t)
	owner, member, stranger := "wowner@test.com", "wmember@test.com", "wstranger@test.com"

	// Fixture writes (create org + add member) emitted entries.
	code, body := auditReq(t, f, owner, "/api/v1/orgs/watch/audit")
	if code != http.StatusOK {
		t.Fatalf("org audit: want 200, got %d (%s)", code, body)
	}
	var page auditdto.AuditPage
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("want 2 entries (org.created + member.added), got %d", page.Total)
	}
	if page.Items[0].Action != "member.added" || page.Items[1].Action != "org.created" {
		t.Fatalf("want newest-first lifecycle, got %+v", page.Items)
	}

	// Action filter.
	code, body = auditReq(t, f, owner, "/api/v1/orgs/watch/audit?action=org.created")
	var filtered auditdto.AuditPage
	_ = json.Unmarshal(body, &filtered)
	if code != http.StatusOK || filtered.Total != 1 {
		t.Fatalf("filter: want 1, got %d (%s)", code, body)
	}

	// MEMBER cannot read (403); anonymous 401; stranger stealth 404.
	if code, _ := auditReq(t, f, member, "/api/v1/orgs/watch/audit"); code != http.StatusForbidden {
		t.Fatalf("member: want 403, got %d", code)
	}
	if code, _ := auditReq(t, f, "", "/api/v1/orgs/watch/audit"); code != http.StatusUnauthorized {
		t.Fatalf("anon: want 401, got %d", code)
	}
	if code, _ := auditReq(t, f, stranger, "/api/v1/orgs/watch/audit"); code != http.StatusNotFound {
		t.Fatalf("stranger: want 404, got %d", code)
	}

	// Bad filters 400, never silently ignored.
	for _, q := range []string{"?page=0", "?perPage=101", "?since=not-a-time", "?actor=nope"} {
		if code, _ := auditReq(t, f, owner, "/api/v1/orgs/watch/audit"+q); code != http.StatusBadRequest {
			t.Fatalf("filter %s: want 400, got %d", q, code)
		}
	}
}

func TestHTTPPlatformAuditSuperadmin(t *testing.T) {
	f := newAuditFixture(t)

	// Non-superadmin owner is forbidden on the platform read.
	if code, _ := auditReq(t, f, "wowner@test.com", "/api/v1/admin/audit"); code != http.StatusForbidden {
		t.Fatalf("owner platform: want 403, got %d", code)
	}

	// Promote to superadmin: cross-tenant read opens.
	if err := iam.SeedSuperAdmins(f.enf, f.authSvc, []string{"wowner@test.com"}); err != nil {
		t.Fatalf("seed superadmin: %v", err)
	}
	code, body := auditReq(t, f, "wowner@test.com", "/api/v1/admin/audit")
	var page auditdto.AuditPage
	_ = json.Unmarshal(body, &page)
	if code != http.StatusOK || page.Total != 2 {
		t.Fatalf("superadmin platform: want 200/2, got %d/%d (%s)", code, page.Total, body)
	}
	code, body = auditReq(t, f, "wowner@test.com", "/api/v1/admin/audit?org="+f.orgID)
	_ = json.Unmarshal(body, &page)
	if code != http.StatusOK || page.Total != 2 {
		t.Fatalf("superadmin org filter: want 200/2, got %d/%d (%s)", code, page.Total, body)
	}
}
