package iam_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/iam"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
)

// These tests exercise the IAM repair reads directly (the admin service
// wraps them with audit emits; covered in the handler suite).

func TestCountOrgPolicies(t *testing.T) {
	e := newTestEnforcer(t)
	org := uuid.New()
	if err := iam.SeedOrgPolicies(e, org); err != nil {
		t.Fatalf("seed: %v", err)
	}
	n, err := iam.CountOrgPolicies(e, org)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	// 1 owner wildcard + 32 admin + 4 member = 37 (pins matrix drift).
	if n != 37 {
		t.Fatalf("want 37 policy rows, got %d", n)
	}
	if err := iam.SeedOrgPolicies(e, org); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	n2, err := iam.CountOrgPolicies(e, org)
	if err != nil || n2 != n {
		t.Fatalf("reseed must be idempotent: %d -> %d, %v", n, n2, err)
	}
}

func TestUserGroupingsRoundtrip(t *testing.T) {
	e := newTestEnforcer(t)
	user, org := uuid.New(), uuid.New()
	role := orgmodel.MemberRoleAdmin
	syncer := iam.NewMembershipSyncer(e)
	if err := syncer.SyncMembership(user, org, &role); err != nil {
		t.Fatalf("sync: %v", err)
	}
	rows, err := iam.UserGroupings(e, user)
	if err != nil {
		t.Fatalf("groupings: %v", err)
	}
	if len(rows) != 1 || len(rows[0]) != 3 || rows[0][1] != "ADMIN" {
		t.Fatalf("want one ADMIN row, got %+v", rows)
	}
}
