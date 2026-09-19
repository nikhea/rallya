package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	authdto "github.com/nikhea/rallya/internal/auth/dto"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/repository"
	"github.com/nikhea/rallya/internal/organization/service"
)

// fakeSyncer records grouping sync calls.
type fakeSyncer struct {
	calls []syncCall
	err   error
}

type syncCall struct {
	user uuid.UUID
	org  uuid.UUID
	role *orgmodel.MemberRole
}

func (f *fakeSyncer) SyncMembership(userID, orgID uuid.UUID, role *orgmodel.MemberRole) error {
	f.calls = append(f.calls, syncCall{user: userID, org: orgID, role: role})
	return f.err
}

type orgFixture struct {
	db       *gorm.DB
	svc      *service.OrgService
	repo     *repository.OrgRepository
	users    *testutil.FakeUserReader
	fake     *jobs.FakeEnqueuer
	sync     *fakeSyncer
	owner    *model.User
	admin    *model.User
	member   *model.User
	stranger *model.User
}

func newOrgFixture(t *testing.T) *orgFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	repo := repository.NewOrgRepository(db)
	users := testutil.NewFakeUserReader()
	svc := service.NewOrgService(repo, users)
	fake := &jobs.FakeEnqueuer{}
	svc.SetEnqueuer(fake)
	f := &orgFixture{
		svc: svc, repo: repo, users: users, fake: fake, sync: &fakeSyncer{},
	}
	svc.SetGroupSyncer(f.sync)
	mk := func(email, first string) *model.User {
		return users.Add(&model.User{
			Email: email, EmailVerified: true, Status: model.UserStatusActive,
			Profile: &model.UserProfile{FirstName: testutil.StrPtr(first)},
		})
	}
	f.owner = mk("owner@test.com", "Owner")
	f.admin = mk("admin@test.com", "Admin")
	f.member = mk("member@test.com", "Member")
	f.stranger = mk("stranger@test.com", "Stranger")
	f.db = db
	return f
}

// expireInvite backdates an invite's expiry via its emailed link.
func expireInvite(t *testing.T, f *orgFixture, link string) {
	t.Helper()
	inv, err := f.repo.GetInviteByHash(token.HashToken(tokenFromLink(link)))
	if err != nil {
		t.Fatalf("load invite: %v", err)
	}
	if err := f.db.Model(&orgmodel.Invite{}).Where("id = ?", inv.ID).
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire invite: %v", err)
	}
}

func TestCreateAndGetOrg(t *testing.T) {
	f := newOrgFixture(t)

	d, err := f.svc.CreateOrg(f.owner.ID, "Acme Inc", "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if d.Slug != "acme-inc" {
		t.Fatalf("expected slugified slug, got %q", d.Slug)
	}
	if d.Role != string(orgmodel.MemberRoleOwner) {
		t.Fatalf("expected OWNER, got %v", d.Role)
	}
	// Duplicate slug.
	if _, err := f.svc.CreateOrg(f.admin.ID, "Other", "acme-inc", ""); !errors.Is(err, service.ErrSlugTaken) {
		t.Fatalf("expected ErrSlugTaken, got %v", err)
	}
	// Bad slug.
	if _, err := f.svc.CreateOrg(f.admin.ID, "Bad", "BAD SLUG!", ""); !errors.Is(err, service.ErrInvalidSlug) {
		t.Fatalf("expected ErrInvalidSlug, got %v", err)
	}
	// Empty name.
	if _, err := f.svc.CreateOrg(f.admin.ID, "  ", "", ""); !errors.Is(err, service.ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
	// Get by id and slug; stranger gets stealth not-found.
	if _, err := f.svc.GetOrg(f.owner.ID, d.ID); err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if _, err := f.svc.GetOrg(f.owner.ID, "acme-inc"); err != nil {
		t.Fatalf("get by slug: %v", err)
	}
	if _, err := f.svc.GetOrg(f.stranger.ID, "acme-inc"); !errors.Is(err, service.ErrOrgNotFound) {
		t.Fatalf("expected ErrOrgNotFound, got %v", err)
	}
	// Sync recorded for creator.
	if len(f.sync.calls) != 1 || f.sync.calls[0].role == nil || *f.sync.calls[0].role != orgmodel.MemberRoleOwner {
		t.Fatalf("expected owner sync, got %+v", f.sync.calls)
	}
	// Me-embedding shape.
	orgs, err := f.svc.ListMemberships(f.owner.ID)
	if err != nil || len(orgs) != 1 || orgs[0].Slug != "acme-inc" {
		t.Fatalf("memberships: %+v %v", orgs, err)
	}
	var _ authdto.MembershipLister = f.svc
}

func TestUpdateAndDeleteOrg(t *testing.T) {
	f := newOrgFixture(t)
	d, _ := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")

	// Member cannot update.
	m, _ := f.svc.AddMember(f.owner.ID, d.Slug, f.member.Email, orgmodel.MemberRoleMember)
	_ = m
	if _, err := f.svc.UpdateOrg(f.member.ID, d.Slug, "Nope", "", ""); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	// Promote to admin via owner, then update works.
	if _, err := f.svc.UpdateMemberRole(f.owner.ID, d.Slug, f.member.ID, orgmodel.MemberRoleAdmin); err != nil {
		t.Fatalf("promote: %v", err)
	}
	updated, err := f.svc.UpdateOrg(f.member.ID, d.Slug, "Acme Corp", "acme-corp", "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Acme Corp" || updated.Slug != "acme-corp" {
		t.Fatalf("unexpected org: %+v", updated)
	}
	// Admin cannot delete.
	if err := f.svc.DeleteOrg(f.member.ID, "acme-corp"); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	// Owner deletes; sync removals recorded.
	if err := f.svc.DeleteOrg(f.owner.ID, "acme-corp"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	removed := 0
	for _, c := range f.sync.calls {
		if c.role == nil {
			removed++
		}
	}
	if removed != 2 {
		t.Fatalf("expected 2 removal syncs, got %+v", f.sync.calls)
	}
}

func TestMemberGuardsAndLastOwner(t *testing.T) {
	f := newOrgFixture(t)
	d, _ := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	slug := d.Slug

	// AddMember: unknown account.
	if _, err := f.svc.AddMember(f.owner.ID, slug, "ghost@test.com", orgmodel.MemberRoleMember); !errors.Is(err, service.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
	// AddMember: non-admin grantor.
	if _, err := f.svc.AddMember(f.stranger.ID, slug, f.member.Email, orgmodel.MemberRoleMember); !errors.Is(err, service.ErrOrgNotFound) {
		t.Fatalf("expected ErrOrgNotFound (stealth), got %v", err)
	}
	// Owner adds admin + member.
	if _, err := f.svc.AddMember(f.owner.ID, slug, f.admin.Email, orgmodel.MemberRoleAdmin); err != nil {
		t.Fatalf("add admin: %v", err)
	}
	if _, err := f.svc.AddMember(f.owner.ID, slug, f.member.Email, orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	// Duplicate add.
	if _, err := f.svc.AddMember(f.owner.ID, slug, f.member.Email, orgmodel.MemberRoleMember); !errors.Is(err, service.ErrAlreadyMember) {
		t.Fatalf("expected ErrAlreadyMember, got %v", err)
	}
	// Admin cannot grant OWNER.
	if _, err := f.svc.AddMember(f.admin.ID, slug, f.stranger.Email, orgmodel.MemberRoleOwner); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	// Admin cannot remove OWNER.
	if err := f.svc.RemoveMember(f.admin.ID, slug, f.owner.ID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	// Non-owner cannot change roles.
	if _, err := f.svc.UpdateMemberRole(f.admin.ID, slug, f.member.ID, orgmodel.MemberRoleAdmin); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	// Owner demotes self (last owner) -> blocked.
	if _, err := f.svc.UpdateMemberRole(f.owner.ID, slug, f.owner.ID, orgmodel.MemberRoleAdmin); !errors.Is(err, service.ErrLastOwner) {
		t.Fatalf("expected ErrLastOwner, got %v", err)
	}
	// Promote admin to owner, then demote original.
	if _, err := f.svc.UpdateMemberRole(f.owner.ID, slug, f.admin.ID, orgmodel.MemberRoleOwner); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if _, err := f.svc.UpdateMemberRole(f.owner.ID, slug, f.owner.ID, orgmodel.MemberRoleAdmin); err != nil {
		t.Fatalf("demote after handover: %v", err)
	}
	// Self-leave as member works.
	if err := f.svc.RemoveMember(f.member.ID, slug, f.member.ID); err != nil {
		t.Fatalf("self leave: %v", err)
	}
}

func TestInviteAcceptDeclineFlow(t *testing.T) {
	f := newOrgFixture(t)
	d, _ := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	slug := d.Slug

	// Non-admin cannot invite.
	if err := f.svc.InviteMember(f.stranger.ID, slug, "new@test.com", orgmodel.MemberRoleMember); !errors.Is(err, service.ErrOrgNotFound) {
		t.Fatalf("expected ErrOrgNotFound, got %v", err)
	}
	// Invite existing member -> 409.
	if err := f.svc.InviteMember(f.owner.ID, slug, f.owner.Email, orgmodel.MemberRoleMember); !errors.Is(err, service.ErrAlreadyMember) {
		t.Fatalf("expected ErrAlreadyMember, got %v", err)
	}
	// Invite outsider (no account yet) works.
	if err := f.svc.InviteMember(f.owner.ID, slug, "new@test.com", orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("invite: %v", err)
	}
	invJobs := f.fake.OfKind("send_org_invite_email")
	if len(invJobs) != 1 {
		t.Fatalf("expected 1 invite job, got %d", len(invJobs))
	}
	args := invJobs[0].(jobs.SendOrgInviteEmailArgs)
	if args.OrgSlug != slug || args.Role != "MEMBER" || args.InviteLink == "" || args.InviterName != "Owner" {
		t.Fatalf("unexpected invite args: %+v", args)
	}

	// Accept with wrong email account -> mismatch.
	if _, err := f.svc.AcceptInvite(f.stranger.ID, tokenFromLink(args.InviteLink)); !errors.Is(err, service.ErrInviteEmailMismatch) {
		t.Fatalf("expected ErrInviteEmailMismatch, got %v", err)
	}
	// Register the invitee account (simulate signup) then accept.
	newbie := &model.User{Email: "new@test.com", EmailVerified: true, Status: model.UserStatusActive}
	f.users.Add(newbie)
	detail, err := f.svc.AcceptInvite(newbie.ID, tokenFromLink(args.InviteLink))
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if detail.Role != string(orgmodel.MemberRoleMember) {
		t.Fatalf("expected MEMBER, got %v", detail.Role)
	}
	// Second invite + decline path.
	if err := f.svc.InviteMember(f.owner.ID, slug, "second@test.com", orgmodel.MemberRoleAdmin); err != nil {
		t.Fatalf("invite2: %v", err)
	}
	second := &model.User{Email: "second@test.com", EmailVerified: true, Status: model.UserStatusActive}
	f.users.Add(second)
	args2 := f.fake.OfKind("send_org_invite_email")[1].(jobs.SendOrgInviteEmailArgs)
	if args2.Role != "ADMIN" {
		t.Fatalf("expected ADMIN invite, got %+v", args2)
	}
	if err := f.svc.DeclineInvite(second.ID, tokenFromLink(args2.InviteLink)); err != nil {
		t.Fatalf("decline: %v", err)
	}
	// Decline idempotent; accept-after-decline invalid.
	if err := f.svc.DeclineInvite(second.ID, tokenFromLink(args2.InviteLink)); err != nil {
		t.Fatalf("decline idempotent: %v", err)
	}
	if _, err := f.svc.AcceptInvite(second.ID, tokenFromLink(args2.InviteLink)); !errors.Is(err, service.ErrInvalidInvite) {
		t.Fatalf("expected ErrInvalidInvite, got %v", err)
	}
	// Bogus token.
	if _, err := f.svc.AcceptInvite(second.ID, "bogus"); !errors.Is(err, service.ErrInvalidInvite) {
		t.Fatalf("expected ErrInvalidInvite, got %v", err)
	}
	// Expired invite.
	if err := f.svc.InviteMember(f.owner.ID, slug, "old@test.com", orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("invite3: %v", err)
	}
	oldie := &model.User{Email: "old@test.com", EmailVerified: true, Status: model.UserStatusActive}
	f.users.Add(oldie)
	args3 := f.fake.OfKind("send_org_invite_email")[2].(jobs.SendOrgInviteEmailArgs)
	expireInvite(t, f, args3.InviteLink)
	if _, err := f.svc.AcceptInvite(oldie.ID, tokenFromLink(args3.InviteLink)); !errors.Is(err, service.ErrInvalidInvite) {
		t.Fatalf("expected ErrInvalidInvite (expired), got %v", err)
	}
}

func tokenFromLink(link string) string {
	for i := len(link) - 1; i >= 0; i-- {
		if link[i] == '=' {
			return link[i+1:]
		}
	}
	return link
}
