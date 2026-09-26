package service_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"

	authmodel "github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/iam"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/repository"
	"github.com/nikhea/rallya/internal/organization/service"
)

// TestRoleRegistryMatchesIAM pins the grantable vocabulary to IAM's
// matrices behaviorally: every matrix pair defines, garbage rejects.
func TestRoleRegistryMatchesIAM(t *testing.T) {
	f := newRoleFixture(t)
	i := 0
	define := func(org, name string, perms ...service.RolePermission) error {
		_, err := f.svc.DefineRole(f.owner, org, name, perms)
		return err
	}
	second := "roles-second"
	if _, err := f.svc.CreateOrg(f.owner, "Second", second, ""); err != nil {
		t.Fatalf("create second org: %v", err)
	}
	for _, p := range append(append([]iam.Permission{}, iam.AdminPermissions()...), iam.MemberPermissions()...) {
		org := "roles"
		if i >= service.MaxCustomRoles-1 {
			org = second
		}
		name := fmt.Sprintf("matrix-%d", i)
		if err := define(org, name, service.RolePermission{Object: p.Obj, Action: p.Act}); err != nil {
			t.Fatalf("matrix pair %s:%s must define: %v", p.Obj, p.Act, err)
		}
		i++
	}
	for _, bad := range []service.RolePermission{
		{Object: "nope", Action: "read"},
		{Object: "event", Action: "nuke"},
		{Object: "", Action: ""},
	} {
		if err := define("roles", "garbage", bad); !errors.Is(err, service.ErrInvalidRolePermissions) {
			t.Fatalf("garbage pair %+v: want ErrInvalidRolePermissions, got %v", bad, err)
		}
	}
	if err := define("roles", "empty"); !errors.Is(err, service.ErrInvalidRolePermissions) {
		t.Fatalf("empty perms: want ErrInvalidRolePermissions, got %v", err)
	}
}

type roleFixture struct {
	db    *gorm.DB
	svc   *service.OrgService
	org   string
	owner uuid.UUID
	staff uuid.UUID
	e     *casbin.Enforcer
}

func casbinRuleForTest() any {
	return &gormadapter.CasbinRule{}
}

func newRoleFixture(t *testing.T) *roleFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.AutoMigrate(casbinRuleForTest()); err != nil {
		t.Fatalf("migrate casbin: %v", err)
	}
	users := testutil.NewFakeUserReader()
	owner := users.Add(&authmodel.User{Email: "rowner@test.com", Status: authmodel.UserStatusActive})
	staff := users.Add(&authmodel.User{Email: "rstaff@test.com", Status: authmodel.UserStatusActive})
	repo := repository.NewOrgRepository(db)
	svc := service.NewOrgService(repo, users)
	svc.SetEntitlementProvider(proEntitlements())
	e, err := iam.NewEnforcer(db)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}
	svc.SetGroupSyncer(iam.NewMembershipSyncer(e))
	svc.SetPolicySeeder(iam.NewOrgPolicySeeder(e))
	org, err := svc.CreateOrg(owner.ID, "Roles", "roles", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := svc.AddMember(owner.ID, org.Slug, staff.Email, orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return &roleFixture{db: db, svc: svc, org: org.Slug, owner: owner.ID, staff: staff.ID, e: e}
}

func TestDefineGuards(t *testing.T) {
	f := newRoleFixture(t)
	door := []service.RolePermission{{Object: "checkin", Action: "create"}, {Object: "checkin", Action: "read"}}

	if _, err := f.svc.DefineRole(f.owner, f.org, "door", door); err != nil {
		t.Fatalf("define: %v", err)
	}
	if _, err := f.svc.DefineRole(f.owner, f.org, "Door", door); !errors.Is(err, service.ErrRoleExists) {
		t.Fatalf("case-insensitive dup: want ErrRoleExists, got %v", err)
	}
	for _, name := range []string{"ADMIN", "Owner", "superadmin", "bad name!", ""} {
		if _, err := f.svc.DefineRole(f.owner, f.org, name, door); err == nil {
			t.Fatalf("name %q must reject", name)
		}
	}
	if _, err := f.svc.DefineRole(f.staff, f.org, "other", door); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("non-owner define: want ErrForbidden, got %v", err)
	}
}

func TestAssignEnforcesUnion(t *testing.T) {
	f := newRoleFixture(t)
	door := []service.RolePermission{{Object: "checkin", Action: "create"}, {Object: "checkin", Action: "read"}}
	if _, err := f.svc.DefineRole(f.owner, f.org, "door", door); err != nil {
		t.Fatalf("define: %v", err)
	}
	orgID := mustOrgID(t, f)

	can := func(user uuid.UUID, obj, act string) bool {
		return iam.Enforce(f.e, user.String(), orgID.String(), obj, act)
	}
	if can(f.staff, "checkin", "create") {
		t.Fatalf("staff must not scan before grant")
	}
	if err := f.svc.AssignRole(f.owner, f.org, f.staff, "door"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if !can(f.staff, "checkin", "create") {
		t.Fatalf("staff must scan after grant")
	}
	if !can(f.staff, "event", "read") {
		t.Fatalf("base MEMBER reads must survive the custom grant")
	}
	if can(f.staff, "event", "delete") {
		t.Fatalf("custom grant must not leak event:delete")
	}
	// Idempotent re-assign.
	if err := f.svc.AssignRole(f.owner, f.org, f.staff, "door"); err != nil {
		t.Fatalf("re-assign: %v", err)
	}
	if err := f.svc.UnassignRole(f.owner, f.org, f.staff, "door"); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if can(f.staff, "checkin", "create") {
		t.Fatalf("staff must not scan after revoke")
	}
	// Idempotent unassign.
	if err := f.svc.UnassignRole(f.owner, f.org, f.staff, "door"); err != nil {
		t.Fatalf("re-unassign: %v", err)
	}
}

func TestDeleteBlockedWhileAssigned(t *testing.T) {
	f := newRoleFixture(t)
	door := []service.RolePermission{{Object: "checkin", Action: "read"}}
	if _, err := f.svc.DefineRole(f.owner, f.org, "door", door); err != nil {
		t.Fatalf("define: %v", err)
	}
	if err := f.svc.AssignRole(f.owner, f.org, f.staff, "door"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := f.svc.DeleteRole(f.owner, f.org, "door"); !errors.Is(err, service.ErrRoleAssigned) {
		t.Fatalf("delete assigned: want ErrRoleAssigned, got %v", err)
	}
	if err := f.svc.UnassignRole(f.owner, f.org, f.staff, "door"); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if err := f.svc.DeleteRole(f.owner, f.org, "door"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := f.svc.AssignRole(f.owner, f.org, f.staff, "door"); !errors.Is(err, service.ErrRoleNotFound) {
		t.Fatalf("assign deleted: want ErrRoleNotFound, got %v", err)
	}
}

func TestRemoveMemberStripsCustoms(t *testing.T) {
	f := newRoleFixture(t)
	door := []service.RolePermission{{Object: "checkin", Action: "read"}}
	if _, err := f.svc.DefineRole(f.owner, f.org, "door", door); err != nil {
		t.Fatalf("define: %v", err)
	}
	if err := f.svc.AssignRole(f.owner, f.org, f.staff, "door"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := f.svc.RemoveMember(f.owner, f.org, f.staff); err != nil {
		t.Fatalf("remove: %v", err)
	}
	orgID := mustOrgID(t, f)
	if iam.Enforce(f.e, f.staff.String(), orgID.String(), "checkin", "read") {
		t.Fatalf("removed member must lose custom grants")
	}
	var n int64
	if err := f.db.Model(&orgmodel.MemberCustomRole{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("assignment rows must cascade-clear, got %d, %v", n, err)
	}
}

func mustOrgID(t *testing.T, f *roleFixture) uuid.UUID {
	t.Helper()
	id, err := f.svc.ResolveOrgID(f.org)
	if err != nil {
		t.Fatalf("resolve org: %v", err)
	}
	return id
}
