package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	attendeeModel "github.com/nikhea/rallya/internal/attendee/model"
	"github.com/nikhea/rallya/internal/attendee/repository"
	"github.com/nikhea/rallya/internal/attendee/service"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/testutil"
)

// fakeEvents implements service.EventResolver.
type fakeEvents struct {
	ids map[string]uuid.UUID
}

func (f *fakeEvents) ResolveEventID(orgID uuid.UUID, ref string) (uuid.UUID, error) {
	_ = orgID
	if id, ok := f.ids[ref]; ok {
		return id, nil
	}
	return uuid.Nil, errors.New("no event")
}

func (f *fakeEvents) OrgOf(eventID uuid.UUID) (uuid.UUID, error) {
	for _, id := range f.ids {
		if id == eventID {
			return uuid.New(), nil
		}
	}
	return uuid.Nil, errors.New("no event")
}

// fakeOrgs implements service.OrgAccess.
type fakeOrgs struct{ admins map[uuid.UUID]bool }

func (f *fakeOrgs) CanManage(userID, orgID uuid.UUID) bool {
	_ = orgID
	return f.admins[userID]
}

type attendeeFixture struct {
	svc   *service.AttendeeService
	users *testutil.FakeUserReader
	owner *model.User
	user  *model.User
	event uuid.UUID
	org   uuid.UUID
}

func newAttendeeFixture(t *testing.T) *attendeeFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	if err := db.AutoMigrate(attendeeModel.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewAttendeeRepository(db)
	users := testutil.NewFakeUserReader()
	owner := users.Add(&model.User{Email: "owner@test.com", EmailVerified: true, Status: model.UserStatusActive})
	user := users.Add(&model.User{Email: "user@test.com", EmailVerified: true, Status: model.UserStatusActive})
	events := &fakeEvents{ids: map[string]uuid.UUID{}}
	event := uuid.New()
	events.ids["fest"] = event
	svc := service.NewAttendeeService(repo, users, events, &fakeOrgs{admins: map[uuid.UUID]bool{owner.ID: true}})
	return &attendeeFixture{svc: svc, users: users, owner: owner, user: user, event: event, org: uuid.New()}
}

func TestMintIdempotentAndCancel(t *testing.T) {
	f := newAttendeeFixture(t)
	order := uuid.New()

	minted, err := f.svc.MintForOrder(nil, order, f.user.ID, f.event, f.user.Email, "User", 3)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if len(minted) != 3 {
		t.Fatalf("expected 3, got %d", len(minted))
	}
	seen := map[string]bool{}
	for _, m := range minted {
		if m.QRToken == "" || seen[m.QRToken] {
			t.Fatalf("bad tokens: %+v", minted)
		}
		seen[m.QRToken] = true
	}
	// Redelivery converges: zero new rows.
	again, err := f.svc.MintForOrder(nil, order, f.user.ID, f.event, f.user.Email, "User", 3)
	if err != nil || len(again) != 0 {
		t.Fatalf("remint: %+v %v", again, err)
	}
	// Owner reads with QR-less shape; admin reads too.
	if _, err := f.svc.GetAttendee(f.user.ID, minted[0].ID); err != nil {
		t.Fatalf("get own: %v", err)
	}
	if _, err := f.svc.GetAttendee(uuid.New(), minted[0].ID); !errors.Is(err, service.ErrAttendeeNotFound) {
		t.Fatalf("expected ErrAttendeeNotFound, got %v", err)
	}
	if _, err := f.svc.GetAttendeeAdmin(f.owner.ID, minted[0].ID); err != nil {
		t.Fatalf("admin get: %v", err)
	}
	// Owner cancels own row.
	cancelled, err := f.svc.CancelMine(f.user.ID, minted[0].ID)
	if err != nil || cancelled.Status != "CANCELLED" {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}
	if _, err := f.svc.CancelMine(f.user.ID, minted[0].ID); !errors.Is(err, service.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	// Order-level cancel flips the rest.
	if err := f.svc.CancelForOrder(nil, order); err != nil {
		t.Fatalf("order cancel: %v", err)
	}
	mine, total, err := f.svc.ListMine(f.user.ID, 20, 0)
	if err != nil || total != 3 || len(mine) != 3 {
		t.Fatalf("list: %d %v", total, err)
	}
}

func TestManualAddAndCorrect(t *testing.T) {
	f := newAttendeeFixture(t)

	if _, err := f.svc.AddManual(f.org, "fest", "not-an-email", nil); !errors.Is(err, service.ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}
	if _, err := f.svc.AddManual(f.org, "ghost", "w@test.com", nil); err == nil {
		t.Fatal("expected error for unknown event")
	}
	a, err := f.svc.AddManual(f.org, "fest", "walkin@test.com", strPtrA("Walk In"))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if a.Email != "walkin@test.com" || a.OrderID != nil {
		t.Fatalf("unexpected row: %+v", a)
	}
	// Correct name + email.
	fixed, err := f.svc.CorrectAttendeeInEvent(f.event, mustParseA(t, a.ID), strPtrA("Jane"), nil)
	if err != nil || fixed.Name == nil || *fixed.Name != "Jane" {
		t.Fatalf("correct: %+v %v", fixed, err)
	}
	// Cross-event scoping: wrong event id 404s.
	if _, err := f.svc.CorrectAttendeeInEvent(uuid.New(), mustParseA(t, a.ID), strPtrA("X"), nil); !errors.Is(err, service.ErrAttendeeNotFound) {
		t.Fatalf("expected ErrAttendeeNotFound, got %v", err)
	}
	bad := "bad-email"
	if _, err := f.svc.CorrectAttendeeInEvent(f.event, mustParseA(t, a.ID), nil, &bad); !errors.Is(err, service.ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}
	// Roster lists it.
	items, total, err := f.svc.ListEventRoster(f.event, "", 20, 0)
	if err != nil || total != 1 || items[0].Email != "walkin@test.com" {
		t.Fatalf("roster: %+v %d %v", items, total, err)
	}
}

func strPtrA(s string) *string { return &s }

func mustParseA(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return id
}
