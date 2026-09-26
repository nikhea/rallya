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
	organization "github.com/nikhea/rallya/internal/organization"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	"github.com/nikhea/rallya/internal/organization/handler"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
)

func init() { gin.SetMode(gin.TestMode) }

func newRoleHTTPFixture(t *testing.T) (*gin.Engine, map[string]string, string) {
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
	orgSvc.SetEntitlementProvider(proPlans{})
	orgSvc.SetAuditEmitter(auditservice.NewAuditService(auditrepo.NewAuditRepository(db)))

	r := gin.New()
	organization.RegisterRoutes(r.Group("/api/v1/orgs"), handler.NewHandler(orgSvc), orgRepo, authRepo, e)

	tokens := map[string]string{}
	owner := mkRoleUser(t, authRepo, authSvc, "powner@test.com")
	member := mkRoleUser(t, authRepo, authSvc, "pmember@test.com")
	mkRoleUser(t, authRepo, authSvc, "pstranger@test.com")
	for _, email := range []string{"powner@test.com", "pmember@test.com", "pstranger@test.com"} {
		pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		tokens[email] = pair.AccessToken
	}
	if _, err := orgSvc.CreateOrg(owner, "Parts", "parts", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := orgSvc.AddMember(owner, "parts", "pmember@test.com", orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	m, err := authRepo.GetUserByEmail("pmember@test.com")
	if err != nil {
		t.Fatalf("get member: %v", err)
	}
	_ = member
	return r, tokens, m.ID.String()
}

func mkRoleUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email string) uuid.UUID {
	t.Helper()
	name := "P"
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

func roleReq(t *testing.T, r *gin.Engine, tokens map[string]string, email, method, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+tokens[email])
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestHTTPCustomRoles(t *testing.T) {
	r, tokens, memberID := newRoleHTTPFixture(t)
	owner, member, stranger := "powner@test.com", "pmember@test.com", "pstranger@test.com"
	base := "/api/v1/orgs/parts/roles"
	door := `{"name":"door","permissions":[{"object":"checkin","action":"create"},{"object":"checkin","action":"read"}]}`

	// MEMBER cannot define (OWNER-only gate); stranger stealth 404; anon 401.
	if code, _ := roleReq(t, r, tokens, member, "POST", base, door); code != http.StatusForbidden {
		t.Fatalf("member define: want 403, got %d", code)
	}
	if code, _ := roleReq(t, r, tokens, stranger, "POST", base, door); code != http.StatusNotFound {
		t.Fatalf("stranger define: want 404, got %d", code)
	}
	if code, _ := roleReq(t, r, tokens, "", "POST", base, door); code != http.StatusUnauthorized {
		t.Fatalf("anon define: want 401, got %d", code)
	}

	// OWNER defines; dup 409; reserved/unknown 400.
	code, body := roleReq(t, r, tokens, owner, "POST", base, door)
	if code != http.StatusCreated {
		t.Fatalf("define: want 201, got %d (%s)", code, body)
	}
	if code, _ := roleReq(t, r, tokens, owner, "POST", base, door); code != http.StatusConflict {
		t.Fatalf("dup define: want 409, got %d", code)
	}
	if code, _ := roleReq(t, r, tokens, owner, "POST", base, `{"name":"admin","permissions":[{"object":"checkin","action":"read"}]}`); code != http.StatusBadRequest {
		t.Fatalf("reserved: want 400, got %d", code)
	}
	if code, _ := roleReq(t, r, tokens, owner, "POST", base, `{"name":"x","permissions":[{"object":"nope","action":"read"}]}`); code != http.StatusBadRequest {
		t.Fatalf("unknown perm: want 400, got %d", code)
	}

	// List shows the role (ADMIN+ readable; plain members 403 by design).
	code, body = roleReq(t, r, tokens, owner, "GET", base, "")
	var listed []orgdto.CustomRole
	_ = json.Unmarshal(body, &listed)
	if code != http.StatusOK || len(listed) != 1 || listed[0].Name != "door" || len(listed[0].Permissions) != 2 {
		t.Fatalf("list: want 1 door/2 perms, got %d %+v (%s)", code, listed, body)
	}
	if code, _ := roleReq(t, r, tokens, member, "GET", base, ""); code != http.StatusForbidden {
		t.Fatalf("member list: want 403, got %d", code)
	}

	// Assign to member; delete blocked while held; unassign; delete.
	assign := `{"userId":"` + memberID + `"}`
	if code, _ := roleReq(t, r, tokens, owner, "POST", base+"/door/assign", assign); code != http.StatusOK {
		t.Fatalf("assign: want 200, got %d", code)
	}
	if code, _ := roleReq(t, r, tokens, owner, "DELETE", base+"/door", ""); code != http.StatusConflict {
		t.Fatalf("delete held: want 409, got %d", code)
	}
	if code, _ := roleReq(t, r, tokens, member, "POST", base+"/door/assign", assign); code != http.StatusForbidden {
		t.Fatalf("member assign: want 403, got %d", code)
	}
	if code, _ := roleReq(t, r, tokens, owner, "POST", base+"/door/unassign", assign); code != http.StatusOK {
		t.Fatalf("unassign: want 200, got %d", code)
	}
	if code, _ := roleReq(t, r, tokens, owner, "DELETE", base+"/door", ""); code != http.StatusOK {
		t.Fatalf("delete: want 200, got %d", code)
	}
}
