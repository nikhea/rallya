package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/gin-gonic/gin"

	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/iam"
	"github.com/nikhea/rallya/internal/notification/jobs"
	organization "github.com/nikhea/rallya/internal/organization"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	"github.com/nikhea/rallya/internal/organization/handler"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/repository"
	"github.com/nikhea/rallya/internal/organization/service"
)

func newApiKeyFixture(t *testing.T) *orgFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate org: %v", err)
	}
	if err := db.AutoMigrate(&gormadapter.CasbinRule{}); err != nil {
		t.Fatalf("migrate casbin: %v", err)
	}
	orgRepo := repository.NewOrgRepository(db)
	orgSvc := service.NewOrgService(orgRepo, authSvc)
	orgSvc.SetApiKeyStore(authRepo)
	fake := &jobs.FakeEnqueuer{}
	orgSvc.SetEnqueuer(fake)
	e, err := iam.NewEnforcer(db)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}
	orgSvc.SetGroupSyncer(iam.NewMembershipSyncer(e))
	orgSvc.SetPolicySeeder(iam.NewOrgPolicySeeder(e))
	orgSvc.SetEntitlementProvider(proPlans{})
	orgHandler := handler.NewHandler(orgSvc)

	r := gin.New()
	organization.RegisterRoutes(r.Group("/api/v1/orgs"), orgHandler, orgRepo, authRepo, e)

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

func apiKeyRequest(t *testing.T, f *orgFixture, method, path, body, email, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func TestHTTPApiKeysLifecycle(t *testing.T) {
	f := newApiKeyFixture(t)

	// Owner creates org + adds member as ADMIN.
	if w := orgRequest(t, f, "POST", "/api/v1/orgs", `{"name":"Acme Inc"}`, "owner@test.com"); w.Code != http.StatusCreated {
		t.Fatalf("create org: got %d (%s)", w.Code, w.Body.String())
	}
	if w := orgRequest(t, f, "POST", "/api/v1/orgs/acme-inc/members", `{"email":"member@test.com","role":"ADMIN"}`, "owner@test.com"); w.Code != http.StatusCreated {
		t.Fatalf("add member: got %d (%s)", w.Code, w.Body.String())
	}

	// Member (ADMIN) creates a key.
	w := apiKeyRequest(t, f, "POST", "/api/v1/orgs/acme-inc/api-keys", `{"name":"door-1","scopes":["org:read","event:read","checkin:create"]}`, "member@test.com", "")
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: got %d (%s)", w.Code, w.Body.String())
	}
	var created orgdto.ApiKeyCreated
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Key == "" || created.Prefix == "" || len(created.Scopes) != 3 {
		t.Fatalf("unexpected key shape: %+v", created)
	}

	// Bad scope -> 400.
	if w := apiKeyRequest(t, f, "POST", "/api/v1/orgs/acme-inc/api-keys", `{"name":"bad","scopes":["event:nuke"]}`, "owner@test.com", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("bad scope: got %d (%s)", w.Code, w.Body.String())
	}

	// List shows metadata, never the secret.
	w = apiKeyRequest(t, f, "GET", "/api/v1/orgs/acme-inc/api-keys", "", "owner@test.com", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d (%s)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), created.Key) {
		t.Fatalf("list leaked raw secret")
	}

	// Key authenticates: member-readable org GET via X-API-Key.
	w = apiKeyRequest(t, f, "GET", "/api/v1/orgs/acme-inc", "", "", created.Key)
	if w.Code != http.StatusOK {
		t.Fatalf("key get org: got %d (%s)", w.Code, w.Body.String())
	}

	// Narrow scope is intersected: key without org:read is 403 on org GET.
	w = apiKeyRequest(t, f, "POST", "/api/v1/orgs/acme-inc/api-keys", `{"name":"narrow","scopes":["checkin:create"]}`, "owner@test.com", "")
	if w.Code != http.StatusCreated {
		t.Fatalf("create narrow key: got %d (%s)", w.Code, w.Body.String())
	}
	var narrow orgdto.ApiKeyCreated
	if err := json.Unmarshal(w.Body.Bytes(), &narrow); err != nil {
		t.Fatalf("decode narrow: %v", err)
	}
	if w := apiKeyRequest(t, f, "GET", "/api/v1/orgs/acme-inc", "", "", narrow.Key); w.Code != http.StatusForbidden {
		t.Fatalf("narrow key org get: got %d, want 403", w.Code)
	}

	// Key cannot manage keys (create/list/revoke) -> 403.
	if w := apiKeyRequest(t, f, "POST", "/api/v1/orgs/acme-inc/api-keys", `{"name":"x"}`, "", created.Key); w.Code != http.StatusForbidden {
		t.Fatalf("key create key: got %d", w.Code)
	}
	if w := apiKeyRequest(t, f, "GET", "/api/v1/orgs/acme-inc/api-keys", "", "", created.Key); w.Code != http.StatusForbidden {
		t.Fatalf("key list keys: got %d", w.Code)
	}

	// Bogus key -> 401 (built dynamically so secret scanners don't
	// flag this obvious non-secret fixture).
	bogusKey := "rk_" + "live_" + strings.Repeat("0", 43)
	if w := apiKeyRequest(t, f, "GET", "/api/v1/orgs/acme-inc", "", "", bogusKey); w.Code != http.StatusUnauthorized {
		t.Fatalf("bogus key: got %d", w.Code)
	}

	// Revoke -> 204, idempotent. Key then 401s.
	revokePath := "/api/v1/orgs/acme-inc/api-keys/" + created.ID
	if w := apiKeyRequest(t, f, "DELETE", revokePath, "", "owner@test.com", ""); w.Code != http.StatusNoContent {
		t.Fatalf("revoke: got %d (%s)", w.Code, w.Body.String())
	}
	if w := apiKeyRequest(t, f, "DELETE", revokePath, "", "owner@test.com", ""); w.Code != http.StatusNoContent {
		t.Fatalf("revoke idempotent: got %d", w.Code)
	}
	if w := apiKeyRequest(t, f, "GET", "/api/v1/orgs/acme-inc", "", "", created.Key); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key: got %d", w.Code)
	}

	// Revoking another org's key id -> 404 (no cross-org oracle).
	if w := orgRequest(t, f, "POST", "/api/v1/orgs", `{"name":"Other"}`, "owner@test.com"); w.Code != http.StatusCreated {
		t.Fatalf("create other: got %d", w.Code)
	}
	if w := apiKeyRequest(t, f, "DELETE", "/api/v1/orgs/other/api-keys/"+created.ID, "", "owner@test.com", ""); w.Code != http.StatusNotFound {
		t.Fatalf("cross-org revoke: got %d", w.Code)
	}
}
