package iam_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/iam"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
)

func init() { gin.SetMode(gin.TestMode) }

// authedCtx builds a request context carrying auth user + org membership,
// as the chained middlewares would leave it.
func authedCtx(user, org uuid.UUID, role orgmodel.MemberRole) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("auth.userID", user)
	c.Set("org.id", org)
	c.Set("org.role", role)
	return c, w
}

func TestRequirePermission(t *testing.T) {
	e := newTestEnforcer(t)
	org := uuid.MustParse(seedOrg(t, e))
	admin, member := uuid.New(), uuid.New()
	syncRole(t, e, admin, org, ptrRole(orgmodel.MemberRoleAdmin))
	syncRole(t, e, member, org, ptrRole(orgmodel.MemberRoleMember))

	run := func(user uuid.UUID, role orgmodel.MemberRole, obj, act string) int {
		c, w := authedCtx(user, org, role)
		iam.RequirePermission(e, obj, act)(c)
		return w.Code
	}
	// Allowed: chain runs through (gin default 200, no abort).
	if code := run(admin, orgmodel.MemberRoleAdmin, iam.ObjInvite, iam.ActCreate); code != http.StatusOK {
		t.Fatalf("admin invite: got %d", code)
	}
	// Role value in ctx is advisory only; enforcement reads Casbin state.
	if code := run(member, orgmodel.MemberRoleMember, iam.ObjInvite, iam.ActCreate); code != http.StatusForbidden {
		t.Fatalf("member invite: got %d", code)
	}
}

func TestRequirePermissionDenied(t *testing.T) {
	e := newTestEnforcer(t)
	org := uuid.MustParse(seedOrg(t, e))
	member := uuid.New()
	syncRole(t, e, member, org, ptrRole(orgmodel.MemberRoleMember))

	c, w := authedCtx(member, org, orgmodel.MemberRoleMember)
	iam.RequirePermission(e, iam.ObjInvite, iam.ActCreate)(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	// Anonymous -> 401.
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	iam.RequirePermission(e, iam.ObjOrg, iam.ActRead)(c3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w3.Code)
	}
}

func TestRequireSuperAdmin(t *testing.T) {
	e := newTestEnforcer(t)
	boss, pleb := uuid.New(), uuid.New()
	if _, err := e.AddNamedGroupingPolicy("g2", boss.String(), iam.SuperAdminRole); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mkCtx := func(user uuid.UUID) (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("auth.userID", user)
		return c, w
	}
	c, w := mkCtx(boss)
	iam.RequireSuperAdmin(e)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("boss: got %d", w.Code)
	}
	c2, w2 := mkCtx(pleb)
	iam.RequireSuperAdmin(e)(c2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("pleb: got %d", w2.Code)
	}
}
