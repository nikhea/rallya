package iam_test

import (
	"testing"

	"github.com/casbin/casbin/v3"
	casbinmodel "github.com/casbin/casbin/v3/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/iam"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
)

// newTestEnforcer builds a sqlite-backed enforcer (adapter automigrates
// casbin_rule here; production uses migrations + TurnOffAutoMigrate).
// Single-connection pool: :memory: databases are per-connection, so an
// open pool would scatter queries across empty DBs.
func newTestEnforcer(t *testing.T) *casbin.Enforcer {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	m, err := casbinmodel.NewModelFromString(iam.ModelText())
	if err != nil {
		t.Fatalf("model: %v", err)
	}
	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}
	if err := e.LoadPolicy(); err != nil {
		t.Fatalf("load: %v", err)
	}
	return e
}

func seedOrg(t *testing.T, e *casbin.Enforcer) string {
	t.Helper()
	org := uuid.NewString()
	if err := iam.SeedOrgPolicies(e, uuid.MustParse(org)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Idempotent reseed.
	if err := iam.SeedOrgPolicies(e, uuid.MustParse(org)); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	return org
}

func syncRole(t *testing.T, e *casbin.Enforcer, user, org uuid.UUID, role *orgmodel.MemberRole) {
	t.Helper()
	s := iam.NewMembershipSyncer(e)
	if err := s.SyncMembership(user, org, role); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func TestMatrixAllowDeny(t *testing.T) {
	e := newTestEnforcer(t)
	orgA, orgB := seedOrg(t, e), seedOrg(t, e)
	owner, admin, member, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	syncRole(t, e, owner, uuid.MustParse(orgA), ptrRole(orgmodel.MemberRoleOwner))
	syncRole(t, e, admin, uuid.MustParse(orgA), ptrRole(orgmodel.MemberRoleAdmin))
	syncRole(t, e, member, uuid.MustParse(orgA), ptrRole(orgmodel.MemberRoleMember))
	syncRole(t, e, admin, uuid.MustParse(orgB), ptrRole(orgmodel.MemberRoleMember))

	cases := []struct {
		name     string
		sub, dom string
		obj, act string
		want     bool
	}{
		{"owner wildcard delete org", owner.String(), orgA, iam.ObjOrg, iam.ActDelete, true},
		{"owner wildcard anything", owner.String(), orgA, iam.ObjEvent, iam.ActManage, true},
		{"owner scoped to own org", owner.String(), orgB, iam.ObjOrg, iam.ActDelete, false},
		{"admin update org", admin.String(), orgA, iam.ObjOrg, iam.ActUpdate, true},
		{"admin delete org denied", admin.String(), orgA, iam.ObjOrg, iam.ActDelete, false},
		{"admin invite create", admin.String(), orgA, iam.ObjInvite, iam.ActCreate, true},
		{"member read org", member.String(), orgA, iam.ObjOrg, iam.ActRead, true},
		{"member read members", member.String(), orgA, iam.ObjMember, iam.ActRead, true},
		{"member update org denied", member.String(), orgA, iam.ObjOrg, iam.ActUpdate, false},
		{"member invite denied", member.String(), orgA, iam.ObjInvite, iam.ActCreate, false},
		{"stranger denied", stranger.String(), orgA, iam.ObjOrg, iam.ActRead, false},
		{"cross-org: A-member in B denied", member.String(), orgB, iam.ObjOrg, iam.ActRead, false},
		{"cross-org: B-member reads B", admin.String(), orgB, iam.ObjOrg, iam.ActRead, true},
	}
	for _, tc := range cases {
		if got := iam.Enforce(e, tc.sub, tc.dom, tc.obj, tc.act); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestRoleChangeAndRemoval(t *testing.T) {
	e := newTestEnforcer(t)
	org := uuid.MustParse(seedOrg(t, e))
	user := uuid.New()

	syncRole(t, e, user, org, ptrRole(orgmodel.MemberRoleAdmin))
	if !iam.Enforce(e, user.String(), org.String(), iam.ObjInvite, iam.ActCreate) {
		t.Fatal("expected admin invite right")
	}
	// Demote: rights shrink without touching policies.
	syncRole(t, e, user, org, ptrRole(orgmodel.MemberRoleMember))
	if iam.Enforce(e, user.String(), org.String(), iam.ObjInvite, iam.ActCreate) {
		t.Fatal("expected invite right revoked after demote")
	}
	if !iam.Enforce(e, user.String(), org.String(), iam.ObjOrg, iam.ActRead) {
		t.Fatal("expected retained read right")
	}
	// Remove: everything denied; re-remove idempotent.
	syncRole(t, e, user, org, nil)
	if iam.Enforce(e, user.String(), org.String(), iam.ObjOrg, iam.ActRead) {
		t.Fatal("expected deny after removal")
	}
	syncRole(t, e, user, org, nil)
}

func TestRemoveOrgPolicies(t *testing.T) {
	e := newTestEnforcer(t)
	org := uuid.MustParse(seedOrg(t, e))
	user := uuid.New()
	syncRole(t, e, user, org, ptrRole(orgmodel.MemberRoleOwner))
	if !iam.Enforce(e, user.String(), org.String(), iam.ObjOrg, iam.ActDelete) {
		t.Fatal("expected allow before removal")
	}
	iam.RemoveOrgPolicies(e, org)
	if iam.Enforce(e, user.String(), org.String(), iam.ObjOrg, iam.ActRead) {
		t.Fatal("expected deny after policy removal")
	}
}

func TestSuperAdmin(t *testing.T) {
	e := newTestEnforcer(t)
	users := testutil.NewFakeUserReader()
	boss := users.Add(&model.User{Email: "boss@test.com", Status: model.UserStatusActive})
	users.Add(&model.User{Email: "pleb@test.com", Status: model.UserStatusActive})
	otherOrg := uuid.NewString()

	// Empty list: no-op, never wipes.
	if err := iam.SeedSuperAdmins(e, users, nil); err != nil {
		t.Fatalf("seed empty: %v", err)
	}
	// Unknown emails skipped, known granted.
	if err := iam.SeedSuperAdmins(e, users, []string{"boss@test.com", "ghost@test.com"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !iam.IsSuperAdmin(e, boss.ID) {
		t.Fatal("expected superadmin")
	}
	// Bypass: no membership anywhere, full access to foreign org.
	if !iam.Enforce(e, boss.ID.String(), otherOrg, iam.ObjOrg, iam.ActDelete) {
		t.Fatal("expected superadmin bypass")
	}
	pleb, _ := users.GetUserByEmail("pleb@test.com")
	if iam.Enforce(e, pleb.ID.String(), otherOrg, iam.ObjOrg, iam.ActRead) {
		t.Fatal("expected deny for non-member")
	}
	if iam.IsSuperAdmin(e, pleb.ID) {
		t.Fatal("pleb must not be superadmin")
	}
	// Rotation: removed from list -> revoked.
	if err := iam.SeedSuperAdmins(e, users, []string{"pleb@test.com"}); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	if iam.IsSuperAdmin(e, boss.ID) {
		t.Fatal("expected boss revoked")
	}
	if !iam.IsSuperAdmin(e, pleb.ID) {
		t.Fatal("expected pleb granted")
	}
}

func ptrRole(r orgmodel.MemberRole) *orgmodel.MemberRole { return &r }
