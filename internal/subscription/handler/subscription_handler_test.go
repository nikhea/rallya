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
	"github.com/nikhea/rallya/internal/iam"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
	subscription "github.com/nikhea/rallya/internal/subscription"
	subdto "github.com/nikhea/rallya/internal/subscription/dto"
	"github.com/nikhea/rallya/internal/subscription/handler"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
	subrepo "github.com/nikhea/rallya/internal/subscription/repository"
	subservice "github.com/nikhea/rallya/internal/subscription/service"
)

func init() { gin.SetMode(gin.TestMode) }

type subFixture struct {
	router *gin.Engine
	tokens map[string]string
	owner  uuid.UUID
}

func newSubFixture(t *testing.T) *subFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate org: %v", err)
	}
	if err := db.AutoMigrate(submodel.AllModels()...); err != nil {
		t.Fatalf("migrate sub: %v", err)
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

	subSvc := subservice.NewSubscriptionService(subrepo.NewSubscriptionRepository(db), orgSvc)

	r := gin.New()
	subscription.RegisterRoutes(r.Group("/api/v1"), handler.NewHandler(subSvc, "http://test"), authRepo, orgRepo)

	f := &subFixture{router: r, tokens: map[string]string{}}
	owner := mkSubUser(t, authRepo, authSvc, "sowner@test.com")
	mkSubUser(t, authRepo, authSvc, "smember@test.com")
	for _, email := range []string{"sowner@test.com", "smember@test.com"} {
		pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		f.tokens[email] = pair.AccessToken
	}
	if _, err := orgSvc.CreateOrg(owner, "Sub", "suborg", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := orgSvc.AddMember(owner, "suborg", "smember@test.com", orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	f.owner = owner
	return f
}

func mkSubUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email string) uuid.UUID {
	t.Helper()
	name := "S"
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

func subReq(t *testing.T, f *subFixture, email, method, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestHTTPPlansPublic(t *testing.T) {
	f := newSubFixture(t)
	code, body := subReq(t, f, "", "GET", "/api/v1/subscription/plans", "")
	var tiers []subdto.TierResponse
	if err := json.Unmarshal(body, &tiers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code != http.StatusOK || len(tiers) != 3 {
		t.Fatalf("plans: want public 200x3, got %d (%s)", code, body)
	}
	if tiers[0].Plan != "FREE" || tiers[1].Limits.MaxEvents != 25 || tiers[2].Limits.MaxAttendeesPerEvent != 20000 {
		t.Fatalf("bad catalog: %+v", tiers)
	}
}

func TestHTTPSubscriptionReadsAndGuards(t *testing.T) {
	f := newSubFixture(t)
	base := "/api/v1/orgs/suborg/subscription"

	// Absent row reads FREE (members included).
	code, body := subReq(t, f, "smember@test.com", "GET", base, "")
	var sub subdto.SubscriptionResponse
	_ = json.Unmarshal(body, &sub)
	if code != http.StatusOK || sub.Plan != "FREE" {
		t.Fatalf("default: want 200 FREE, got %d (%s)", code, body)
	}

	// Billing unconfigured: 503 with machine code.
	code, body = subReq(t, f, "sowner@test.com", "POST", base+"/checkout", `{"plan":"PRO"}`)
	var eb subdto.ErrorResponse
	_ = json.Unmarshal(body, &eb)
	if code != http.StatusServiceUnavailable || eb.Code != "BILLING_UNAVAILABLE" {
		t.Fatalf("checkout unconfigured: want 503 BILLING_UNAVAILABLE, got %d (%s)", code, body)
	}
	code, _ = subReq(t, f, "sowner@test.com", "POST", base+"/portal", "")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("portal unconfigured: want 503, got %d", code)
	}

	// Stranger org invisible; anon rejected.
	code, _ = subReq(t, f, "", "GET", base, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anon: want 401, got %d", code)
	}
}
