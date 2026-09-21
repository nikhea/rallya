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
	"gorm.io/gorm"

	admin "github.com/nikhea/rallya/internal/admin"
	admindto "github.com/nikhea/rallya/internal/admin/dto"
	"github.com/nikhea/rallya/internal/admin/handler"
	adminservice "github.com/nikhea/rallya/internal/admin/service"
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
	ordermodel "github.com/nikhea/rallya/internal/order/model"
	orderrepo "github.com/nikhea/rallya/internal/order/repository"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
)

func init() { gin.SetMode(gin.TestMode) }

type adminFixture struct {
	router  *gin.Engine
	tokens  map[string]string
	orgID   string
	userID  string
	db      *gorm.DB
	authSvc *authservice.AuthService
	enf     *casbin.Enforcer
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate org: %v", err)
	}
	if err := db.AutoMigrate(auditmodel.AllModels()...); err != nil {
		t.Fatalf("migrate audit: %v", err)
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

	auditSvc := auditservice.NewAuditService(auditrepo.NewAuditRepository(db))
	adminSvc := adminservice.NewAdminService(orgRepo, authRepo, orderrepo.NewOrderRepository(db), e)
	adminSvc.SetAuditEmitter(auditSvc)

	r := gin.New()
	admin.RegisterRoutes(r.Group("/api/v1"), handler.NewHandler(adminSvc), authRepo, e)

	f := &adminFixture{router: r, tokens: map[string]string{}, db: db, authSvc: authSvc, enf: e}
	root := mkAdminUser(t, authRepo, authSvc, "root@test.com")
	owner := mkAdminUser(t, authRepo, authSvc, "xowner@test.com")
	f.userID = owner.String()
	for _, email := range []string{"root@test.com", "xowner@test.com"} {
		pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		f.tokens[email] = pair.AccessToken
	}
	_ = root
	if err := iam.SeedSuperAdmins(e, authSvc, []string{"root@test.com"}); err != nil {
		t.Fatalf("seed superadmin: %v", err)
	}
	org, err := orgSvc.CreateOrg(owner, "Xray", "xray", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	f.orgID = org.ID
	return f
}

func mkAdminUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email string) uuid.UUID {
	t.Helper()
	name := "R"
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

func adminReq(t *testing.T, f *adminFixture, email, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func auditCount(t *testing.T, f *adminFixture, action string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Model(&auditmodel.AuditEvent{}).Where("action = ?", action).Count(&n).Error; err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return n
}

func TestHTTPAdminReads(t *testing.T) {
	f := newAdminFixture(t)
	root, owner := "root@test.com", "xowner@test.com"

	// Non-superadmin owner is forbidden everywhere.
	for _, p := range []string{"/api/v1/admin/orgs", "/api/v1/admin/orgs/" + f.orgID, "/api/v1/admin/users", "/api/v1/admin/users/" + f.userID, "/api/v1/admin/users/" + f.userID + "/orders"} {
		if code, _ := adminReq(t, f, owner, p); code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d", p, code)
		}
	}
	if code, _ := adminReq(t, f, "", "/api/v1/admin/orgs"); code != http.StatusUnauthorized {
		t.Fatalf("anon: want 401, got %d", code)
	}

	// Inventory.
	code, body := adminReq(t, f, root, "/api/v1/admin/orgs")
	var orgs admindto.OrgsPage
	_ = json.Unmarshal(body, &orgs)
	if code != http.StatusOK || orgs.Total != 1 || orgs.Items[0].Slug != "xray" || orgs.Items[0].Members != 1 {
		t.Fatalf("orgs: want 1 xray/1 member, got %d %+v (%s)", code, orgs, body)
	}

	// Org detail with roster.
	code, body = adminReq(t, f, root, "/api/v1/admin/orgs/"+f.orgID)
	var detail admindto.OrgDetail
	_ = json.Unmarshal(body, &detail)
	if code != http.StatusOK || len(detail.Members) != 1 || detail.Members[0].Email != "xowner@test.com" {
		t.Fatalf("org detail: got %d %+v (%s)", code, detail, body)
	}

	// Unknown org 404.
	if code, _ := adminReq(t, f, root, "/api/v1/admin/orgs/"+uuid.New().String()); code != http.StatusNotFound {
		t.Fatalf("unknown org: want 404, got %d", code)
	}

	// User search + detail.
	code, body = adminReq(t, f, root, "/api/v1/admin/users?q=xowner")
	var users admindto.UsersPage
	_ = json.Unmarshal(body, &users)
	if code != http.StatusOK || users.Total != 1 || users.Items[0].SuperAdmin {
		t.Fatalf("users: got %d %+v (%s)", code, users, body)
	}
	code, body = adminReq(t, f, root, "/api/v1/admin/users/"+f.userID)
	var user admindto.UserDetail
	_ = json.Unmarshal(body, &user)
	if code != http.StatusOK || len(user.Memberships) != 1 || user.Memberships[0].OrgSlug != "xray" {
		t.Fatalf("user detail: got %d %+v (%s)", code, user, body)
	}

	// Order history (empty, Stripe-free shape proven by type).
	code, body = adminReq(t, f, root, "/api/v1/admin/users/"+f.userID+"/orders")
	if code != http.StatusOK {
		t.Fatalf("orders: want 200, got %d (%s)", code, body)
	}

	// Every read audited (actor = superadmin).
	for action, want := range map[string]int64{
		"admin.orgs_listed": 1, "admin.org_viewed": 1, "admin.users_searched": 1,
		"admin.user_viewed": 1, "admin.user_orders_viewed": 1,
	} {
		if got := auditCount(t, f, action); got != want {
			t.Fatalf("audit %s: want %d, got %d", action, want, got)
		}
	}
}
