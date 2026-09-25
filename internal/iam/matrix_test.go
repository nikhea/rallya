package iam_test

import (
	"testing"

	"github.com/casbin/casbin/v3"
	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/iam"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
)

// expectedGrants hardcodes the intended matrix INDEPENDENTLY of
// AdminPermissions()/MemberPermissions() so matrix changes break tests
// loudly and deliberately.
var expectedGrants = map[orgmodel.MemberRole]map[string]map[string]bool{
	orgmodel.MemberRoleAdmin: {
		iam.ObjOrg:      {"read": true, "update": true},
		iam.ObjMember:   {"read": true, "create": true, "delete": true},
		iam.ObjInvite:   {"read": true, "create": true, "delete": true},
		iam.ObjEvent:    {"read": true, "create": true, "update": true, "delete": true, "publish": true},
		iam.ObjTicket:   {"read": true, "create": true, "update": true, "delete": true},
		iam.ObjAttendee: {"read": true, "create": true, "update": true},
		iam.ObjCheckin:  {"read": true, "create": true, "update": true},
		iam.ObjAudit:    {"read": true},
		iam.ObjRole:     {"read": true},
		iam.ObjApikey:   {"read": true, "create": true, "delete": true},
	},
	orgmodel.MemberRoleMember: {
		iam.ObjOrg:    {"read": true},
		iam.ObjMember: {"read": true},
		iam.ObjEvent:  {"read": true},
		iam.ObjTicket: {"read": true},
	},
}

var (
	allObjects = []string{iam.ObjOrg, iam.ObjMember, iam.ObjInvite, iam.ObjEvent, "billing"}
	allActions = []string{iam.ActRead, iam.ActCreate, iam.ActUpdate, iam.ActDelete, iam.ActManage, "execute"}
)

// TestExhaustiveMatrix crosses roles x objects x actions x orgs against
// hardcoded expectations. OWNER allows everything in-domain; everyone is
// denied cross-domain; unknown objects/actions are always denied.
func TestExhaustiveMatrix(t *testing.T) {
	e := newTestEnforcer(t)
	orgA, orgB := uuid.New(), uuid.New()
	mustSeed(t, e, orgA)
	mustSeed(t, e, orgB)

	roles := map[orgmodel.MemberRole]uuid.UUID{
		orgmodel.MemberRoleOwner:  uuid.New(),
		orgmodel.MemberRoleAdmin:  uuid.New(),
		orgmodel.MemberRoleMember: uuid.New(),
	}
	stranger := uuid.New()
	for role, user := range roles {
		syncRole(t, e, user, orgA, ptrRole(role))
	}

	failures := 0
	check := func(name, sub, dom, obj, act string, want bool) {
		t.Helper()
		if got := iam.Enforce(e, sub, dom, obj, act); got != want {
			t.Errorf("%s: Enforce(%s,%s) = %v, want %v", name, obj, act, got, want)
			failures++
		}
	}

	for role, user := range roles {
		for _, obj := range allObjects {
			for _, act := range allActions {
				want := false
				if role == orgmodel.MemberRoleOwner {
					want = true // wildcard *,* in-domain
				} else if expectedGrants[role][obj][act] {
					want = true
				}
				check("in-domain "+string(role), user.String(), orgA.String(), obj, act, want)
				// Cross-domain: always denied (superadmin aside).
				check("cross-domain "+string(role), user.String(), orgB.String(), obj, act, false)
			}
		}
	}
	// Stranger denied everywhere, including unknown universe.
	for _, obj := range allObjects {
		for _, act := range allActions {
			check("stranger", stranger.String(), orgA.String(), obj, act, false)
		}
	}
	if failures > 0 {
		t.Fatalf("%d matrix mismatches", failures)
	}
}

// TestMatrixMatchesDeclaredPermissions guards the seam between the
// hardcoded test matrix and the production Admin/Member matrices: every
// declared grant must be expected, and every expected grant declared.
func TestMatrixMatchesDeclaredPermissions(t *testing.T) {
	declared := map[orgmodel.MemberRole]map[string]map[string]bool{
		orgmodel.MemberRoleAdmin:  {},
		orgmodel.MemberRoleMember: {},
	}
	for _, p := range iam.AdminPermissions() {
		m, ok := declared[orgmodel.MemberRoleAdmin][p.Obj]
		if !ok {
			m = map[string]bool{}
			declared[orgmodel.MemberRoleAdmin][p.Obj] = m
		}
		m[p.Act] = true
	}
	for _, p := range iam.MemberPermissions() {
		m, ok := declared[orgmodel.MemberRoleMember][p.Obj]
		if !ok {
			m = map[string]bool{}
			declared[orgmodel.MemberRoleMember][p.Obj] = m
		}
		m[p.Act] = true
	}
	for role, objs := range expectedGrants {
		for obj, acts := range objs {
			for act := range acts {
				if !declared[role][obj][act] {
					t.Fatalf("expected grant missing from declared matrix: %s %s %s", role, obj, act)
				}
			}
		}
	}
	for role, objs := range declared {
		for obj, acts := range objs {
			for act := range acts {
				if !expectedGrants[role][obj][act] {
					t.Fatalf("declared grant missing from expected matrix: %s %s %s", role, obj, act)
				}
			}
		}
	}
}

// TestMalformedInputs verifies fail-closed behavior on garbage.
func TestMalformedInputs(t *testing.T) {
	e := newTestEnforcer(t)
	org := uuid.New()
	mustSeed(t, e, org)
	user := uuid.New()
	syncRole(t, e, user, org, ptrRole(orgmodel.MemberRoleOwner))

	for _, tc := range [][4]string{
		{"", org.String(), iam.ObjOrg, iam.ActRead},
		{user.String(), "", iam.ObjOrg, iam.ActRead},
		{user.String(), org.String(), "", iam.ActRead},
		{user.String(), org.String(), iam.ObjOrg, ""},
		{"not-a-uuid", org.String(), iam.ObjOrg, iam.ActRead},
		{user.String(), "not-a-uuid", iam.ObjOrg, iam.ActRead},
	} {
		if iam.Enforce(e, tc[0], tc[1], tc[2], tc[3]) {
			t.Fatalf("expected deny for malformed input %+v", tc)
		}
	}
	// Owner wildcard still exact on shape: empty action never matches "*".
	if iam.Enforce(e, user.String(), org.String(), iam.ObjOrg, "") {
		t.Fatal("expected deny for empty action even for owner")
	}
}

// mustSeed wraps SeedOrgPolicies with fail-fast.
func mustSeed(t *testing.T, e *casbin.Enforcer, org uuid.UUID) {
	t.Helper()
	if err := iam.SeedOrgPolicies(e, org); err != nil {
		t.Fatalf("seed: %v", err)
	}
}
