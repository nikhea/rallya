package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/service"
)

func TestUpdateOrgSlugConflict(t *testing.T) {
	f := newOrgFixture(t)
	if _, err := f.svc.CreateOrg(f.owner.ID, "Acme", "acme", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.svc.CreateOrg(f.owner.ID, "Beta", "beta", ""); err != nil {
		t.Fatalf("create2: %v", err)
	}
	if _, err := f.svc.UpdateOrg(f.owner.ID, "beta", "", "acme", ""); !errors.Is(err, service.ErrSlugTaken) {
		t.Fatalf("expected ErrSlugTaken, got %v", err)
	}
	if err := f.svc.DeleteOrg(f.owner.ID, "nope"); !errors.Is(err, service.ErrOrgNotFound) {
		t.Fatalf("expected ErrOrgNotFound, got %v", err)
	}
}

func TestInviteListAndRevoke(t *testing.T) {
	f := newOrgFixture(t)
	d, _ := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	slug := d.Slug

	if err := f.svc.InviteMember(f.owner.ID, slug, "a@test.com", orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("invite a: %v", err)
	}
	if err := f.svc.InviteMember(f.owner.ID, slug, "b@test.com", orgmodel.MemberRoleAdmin); err != nil {
		t.Fatalf("invite b: %v", err)
	}
	invites, total, err := f.svc.ListInvites(f.owner.ID, slug, 10, 0)
	if err != nil || total != 2 || len(invites) != 2 {
		t.Fatalf("list: total=%d err=%v", total, err)
	}
	// Non-admin cannot list.
	if _, _, err := f.svc.ListInvites(f.stranger.ID, slug, 10, 0); !errors.Is(err, service.ErrOrgNotFound) {
		t.Fatalf("expected ErrOrgNotFound, got %v", err)
	}
	// Revoke one; accept-after-revoke fails.
	inviteID, err := uuid.Parse(invites[0].ID)
	if err != nil {
		t.Fatalf("parse invite id: %v", err)
	}
	if err := f.svc.RevokeInvite(f.owner.ID, slug, inviteID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	second := &model.User{Email: invites[0].Email, EmailVerified: true, Status: model.UserStatusActive}
	f.users.Add(second)
	raw := inviteRawFor(t, f, invites[0].Email)
	if _, err := f.svc.AcceptInvite(second.ID, raw); !errors.Is(err, service.ErrInvalidInvite) {
		t.Fatalf("expected ErrInvalidInvite, got %v", err)
	}
	// Revoke unknown id.
	if err := f.svc.RevokeInvite(f.owner.ID, slug, second.ID); !errors.Is(err, service.ErrInvalidInvite) {
		t.Fatalf("expected ErrInvalidInvite, got %v", err)
	}
}

// inviteRawFor pulls the raw token from the latest invite job for an email.
func inviteRawFor(t *testing.T, f *orgFixture, email string) string {
	t.Helper()
	link := ""
	for _, j := range f.fake.OfKind("send_org_invite_email") {
		args, ok := j.(jobs.SendOrgInviteEmailArgs)
		if !ok {
			t.Fatalf("unexpected args type %T", j)
		}
		if args.Email == email {
			link = args.InviteLink
		}
	}
	for i := len(link) - 1; i >= 0; i-- {
		if link[i] == '=' {
			return link[i+1:]
		}
	}
	t.Fatalf("no invite job for %s", email)
	return ""
}

func TestAddMemberInactiveUser(t *testing.T) {
	f := newOrgFixture(t)
	d, _ := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	inactive := &model.User{Email: "off@test.com", EmailVerified: true, Status: model.UserStatusInactive}
	f.users.Add(inactive)
	if _, err := f.svc.AddMember(f.owner.ID, d.Slug, "off@test.com", orgmodel.MemberRoleMember); !errors.Is(err, service.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestAcceptIdempotentForMembers(t *testing.T) {
	f := newOrgFixture(t)
	d, _ := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	slug := d.Slug

	if err := f.svc.InviteMember(f.owner.ID, slug, f.member.Email, orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("invite: %v", err)
	}
	raw := inviteRawFor(t, f, f.member.Email)
	first, err := f.svc.AcceptInvite(f.member.ID, raw)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	// Accepting again (same token) returns the membership, no error.
	second, err := f.svc.AcceptInvite(f.member.ID, raw)
	if err != nil {
		t.Fatalf("re-accept: %v", err)
	}
	if second.Role != first.Role {
		t.Fatalf("role changed: %+v vs %+v", first, second)
	}
	// Decline after accept -> conflict.
	if err := f.svc.DeclineInvite(f.member.ID, raw); !errors.Is(err, service.ErrInviteConsumed) {
		t.Fatalf("expected ErrInviteConsumed, got %v", err)
	}
}

func TestSyncerErrorToleratedAndRepair(t *testing.T) {
	f := newOrgFixture(t)
	f.sync.err = errSyncBoom{}
	d, err := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	if err != nil {
		t.Fatalf("create must succeed despite sync failure: %v", err)
	}
	// Repair path replays groupings.
	f.sync.err = nil
	f.sync.calls = nil
	if err := f.svc.SyncUserPolicies(f.owner.ID); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(f.sync.calls) != 1 || f.sync.calls[0].role == nil {
		t.Fatalf("expected 1 repair sync, got %+v", f.sync.calls)
	}
	_ = d
}

func TestRemoveNonMember(t *testing.T) {
	f := newOrgFixture(t)
	d, _ := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	if err := f.svc.RemoveMember(f.owner.ID, d.Slug, f.stranger.ID); !errors.Is(err, service.ErrOrgNotFound) {
		t.Fatalf("expected ErrOrgNotFound, got %v", err)
	}
}

type errSyncBoom struct{}

func (errSyncBoom) Error() string { return "sync boom" }

func TestAutoSlugUniqueness(t *testing.T) {
	f := newOrgFixture(t)

	var slugs []string
	for i := 0; i < 3; i++ {
		d, err := f.svc.CreateOrg(f.owner.ID, "Acme Inc", "", "")
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		slugs = append(slugs, d.Slug)
	}
	want := []string{"acme-inc", "acme-inc-2", "acme-inc-3"}
	for i := range want {
		if slugs[i] != want[i] {
			t.Fatalf("slugs = %v, want %v", slugs, want)
		}
	}
	// Names with no slug-safe characters fall back to org/org-2.
	d1, err := f.svc.CreateOrg(f.owner.ID, "!!!", "", "")
	if err != nil || d1.Slug != "org" {
		t.Fatalf("fallback slug: %+v %v", d1, err)
	}
	d2, err := f.svc.CreateOrg(f.owner.ID, "???", "", "")
	if err != nil || d2.Slug != "org-2" {
		t.Fatalf("fallback slug 2: %+v %v", d2, err)
	}
	// Explicit duplicate still conflicts (no silent rename).
	if _, err := f.svc.CreateOrg(f.owner.ID, "Other", "acme-inc", ""); !errors.Is(err, service.ErrSlugTaken) {
		t.Fatalf("expected ErrSlugTaken, got %v", err)
	}
}

func TestUpdateMyPreferences(t *testing.T) {
	f := newOrgFixture(t)
	d, _ := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	slug := d.Slug

	// Stranger cannot set prefs.
	if err := f.svc.UpdateMyPreferences(f.stranger.ID, slug, false); !errors.Is(err, service.ErrOrgNotFound) {
		t.Fatalf("expected ErrOrgNotFound, got %v", err)
	}
	// Owner opts out.
	if err := f.svc.UpdateMyPreferences(f.owner.ID, slug, false); err != nil {
		t.Fatalf("opt out: %v", err)
	}
	var oid uuid.UUID
	oid, err := f.svc.ResolveOrgID(slug)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, err := f.svc.NotifyTargets(oid)
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no targets after opt-out, got %+v", got)
	}
	// Opt back in.
	if err := f.svc.UpdateMyPreferences(f.owner.ID, slug, true); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	got, err = f.svc.NotifyTargets(oid)
	if err != nil || len(got) != 1 || got[0].Email != "owner@test.com" || got[0].Name != "Owner" {
		t.Fatalf("targets: %+v %v", got, err)
	}
}

type stubCleaner struct {
	calls []uuid.UUID
	err   error
}

func (s *stubCleaner) DeleteOrgAssets(orgID uuid.UUID) error {
	s.calls = append(s.calls, orgID)
	return s.err
}

func TestDeleteOrgCleansAssets(t *testing.T) {
	f := newOrgFixture(t)
	d, _ := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	cleaner := &stubCleaner{}
	f.svc.SetAssetCleaner(cleaner)
	if err := f.svc.DeleteOrg(f.owner.ID, d.Slug); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(cleaner.calls) != 1 {
		t.Fatalf("expected 1 cleanup call, got %+v", cleaner.calls)
	}
	// Cleaner failure never fails the delete.
	d2, _ := f.svc.CreateOrg(f.owner.ID, "Beta", "", "")
	bad := &stubCleaner{err: errors.New("boom")}
	f.svc.SetAssetCleaner(bad)
	if err := f.svc.DeleteOrg(f.owner.ID, d2.Slug); err != nil {
		t.Fatalf("delete must survive cleaner failure: %v", err)
	}
}
