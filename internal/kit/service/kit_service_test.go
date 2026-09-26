package service_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	attendeeModel "github.com/nikhea/rallya/internal/attendee/model"
	attendeeRepo "github.com/nikhea/rallya/internal/attendee/repository"
	attendeeService "github.com/nikhea/rallya/internal/attendee/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
	checkinModel "github.com/nikhea/rallya/internal/checkin/model"
	checkinRepo "github.com/nikhea/rallya/internal/checkin/repository"
	checkinService "github.com/nikhea/rallya/internal/checkin/service"
	kitModel "github.com/nikhea/rallya/internal/kit/model"
	"github.com/nikhea/rallya/internal/kit/repository"
	"github.com/nikhea/rallya/internal/kit/service"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
	subservice "github.com/nikhea/rallya/internal/subscription/service"
)

type fakeEvents struct{ a, b uuid.UUID }

func (f fakeEvents) ResolveEventID(_ uuid.UUID, ref string) (uuid.UUID, error) {
	if ref == "other" {
		return f.b, nil
	}
	return f.a, nil
}

type fakeAttends struct {
	mu    sync.Mutex
	rows  map[uuid.UUID]attendeeModel.AttendeeStatus
	event uuid.UUID
}

func (f *fakeAttends) AttendeeStatusInEvent(attendeeID, eventID uuid.UUID) (attendeeModel.AttendeeStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.rows[attendeeID]
	if !ok || eventID != f.event {
		return "", gorm.ErrRecordNotFound
	}
	return st, nil
}

type kitHarness struct {
	db      *gorm.DB
	svc     *service.KitService
	attends *fakeAttends
	events  fakeEvents
	orgID   uuid.UUID
	staff   uuid.UUID
}

func newKitHarness(t *testing.T) *kitHarness {
	t.Helper()
	db, _, _ := testutil.Setup(t)
	if err := db.AutoMigrate(kitModel.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Partial unique indexes (SQLite honors them; AutoMigrate cannot
	// express them — same gap as production DDL, created explicitly).
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_kit_collections_active ON kit_collections (kit_id, attendee_id) WHERE status <> 'VOIDED'")
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_kit_collections_idem ON kit_collections (kit_id, attendee_id, idempotency_key) WHERE idempotency_key <> ''")
	ev := fakeEvents{a: uuid.New(), b: uuid.New()}
	attends := &fakeAttends{rows: map[uuid.UUID]attendeeModel.AttendeeStatus{}, event: ev.a}
	svc := service.NewKitService(db, repository.NewKitRepository(db), attends, ev)
	// Kit suites predate plans: default to Pro (flag tests override).
	svc.SetEntitlementProvider(&fakeKitPlans{ent: submodel.EntitlementForPlan(submodel.PlanPro)})
	return &kitHarness{db: db, svc: svc, attends: attends, events: ev, orgID: uuid.New(), staff: uuid.New()}
}

func (h *kitHarness) checkedIn(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	h.attends.rows[id] = attendeeModel.AttendeeStatusCheckedIn
	return id
}

func (h *kitHarness) kit(t *testing.T, total int) uuid.UUID {
	t.Helper()
	k, err := h.svc.CreateKit(h.orgID, "fest", "VIP pack", "", total, h.staff)
	if err != nil {
		t.Fatalf("create kit: %v", err)
	}
	if k.Remaining != int64(total) {
		t.Fatalf("fresh kit remaining = %d, want %d", k.Remaining, total)
	}
	return k.Kit.ID
}

func TestKitCollectFlow(t *testing.T) {
	h := newKitHarness(t)
	kitID := h.kit(t, 10)
	att := h.checkedIn(t)

	v, err := h.svc.Collect(h.orgID, "fest", kitID.String(), att.String(), false, "", h.staff)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if v.Collection.Status != kitModel.CollectionStatusCollected || v.Collection.CollectedAt == nil || v.Collection.CollectedBy == nil {
		t.Fatalf("immediate collect must stamp COLLECTED, got %+v", v.Collection)
	}

	// Same attendee, same kit: dup refused.
	if _, err := h.svc.Collect(h.orgID, "fest", kitID.String(), att.String(), false, "", h.staff); err != service.ErrAlreadyCollected {
		t.Fatalf("re-collect: want ErrAlreadyCollected, got %v", err)
	}

	// Void frees re-issue.
	if _, err := h.svc.Void(h.orgID, "fest", v.Collection.ID.String(), h.staff); err != nil {
		t.Fatalf("void: %v", err)
	}
	v2, err := h.svc.Collect(h.orgID, "fest", kitID.String(), att.String(), false, "", h.staff)
	if err != nil {
		t.Fatalf("re-issue after void: %v", err)
	}
	if v2.Collection.ID == v.Collection.ID {
		t.Fatal("re-issue must mint a new row")
	}

	// Reserve path: PENDING -> COLLECTED -> terminal.
	att2 := h.checkedIn(t)
	r, err := h.svc.Collect(h.orgID, "fest", kitID.String(), att2.String(), true, "", h.staff)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if r.Collection.Status != kitModel.CollectionStatusPending || r.Collection.CollectedAt != nil {
		t.Fatalf("reserve must stay PENDING unstamped, got %+v", r.Collection)
	}
	m, err := h.svc.MarkCollected(h.orgID, "fest", r.Collection.ID.String(), h.staff)
	if err != nil {
		t.Fatalf("mark collected: %v", err)
	}
	if m.Collection.Status != kitModel.CollectionStatusCollected {
		t.Fatalf("want COLLECTED, got %s", m.Collection.Status)
	}
	if _, err := h.svc.MarkCollected(h.orgID, "fest", r.Collection.ID.String(), h.staff); err != service.ErrInvalidStatus {
		t.Fatalf("double mark: want ErrInvalidStatus, got %v", err)
	}
	if _, err := h.svc.Void(h.orgID, "fest", r.Collection.ID.String(), h.staff); err != nil {
		t.Fatalf("void collected: %v", err)
	}
	if _, err := h.svc.Void(h.orgID, "fest", r.Collection.ID.String(), h.staff); err != service.ErrInvalidStatus {
		t.Fatalf("double void: want ErrInvalidStatus, got %v", err)
	}
}

func TestKitEligibilityGate(t *testing.T) {
	h := newKitHarness(t)
	kitID := h.kit(t, 10)

	reg := uuid.New()
	h.attends.rows[reg] = attendeeModel.AttendeeStatusRegistered
	if _, err := h.svc.Collect(h.orgID, "fest", kitID.String(), reg.String(), false, "", h.staff); err != service.ErrNotCheckedIn {
		t.Fatalf("registered: want ErrNotCheckedIn, got %v", err)
	}

	cancelled := uuid.New()
	h.attends.rows[cancelled] = attendeeModel.AttendeeStatusCancelled
	if _, err := h.svc.Collect(h.orgID, "fest", kitID.String(), cancelled.String(), false, "", h.staff); err != service.ErrNotCheckedIn {
		t.Fatalf("cancelled: want ErrNotCheckedIn, got %v", err)
	}

	if _, err := h.svc.Collect(h.orgID, "fest", kitID.String(), uuid.NewString(), false, "", h.staff); err != service.ErrAttendeeNotFound {
		t.Fatalf("unknown: want ErrAttendeeNotFound, got %v", err)
	}
	if _, err := h.svc.Collect(h.orgID, "fest", kitID.String(), "not-a-uuid", false, "", h.staff); err != service.ErrAttendeeNotFound {
		t.Fatalf("bad uuid: want ErrAttendeeNotFound, got %v", err)
	}
}

func TestKitStealthShapes(t *testing.T) {
	h := newKitHarness(t)
	kitID := h.kit(t, 10)
	att := h.checkedIn(t)

	// Unknown event ref resolves elsewhere: attendee not in scope (stealth 404).
	if _, err := h.svc.Collect(h.orgID, "other", kitID.String(), att.String(), false, "", h.staff); err != service.ErrAttendeeNotFound {
		t.Fatalf("wrong event: want ErrAttendeeNotFound, got %v", err)
	}
	if _, err := h.svc.Collect(h.orgID, "fest", uuid.NewString(), att.String(), false, "", h.staff); err != service.ErrKitNotFound {
		t.Fatalf("unknown kit: want ErrKitNotFound, got %v", err)
	}
	if _, err := h.svc.Collect(h.orgID, "fest", "nope", att.String(), false, "", h.staff); err != service.ErrKitNotFound {
		t.Fatalf("bad kit uuid: want ErrKitNotFound, got %v", err)
	}
	if _, err := h.svc.MarkCollected(h.orgID, "other", uuid.NewString(), h.staff); err != service.ErrCollectionNotFound {
		t.Fatalf("cross-event transition: want ErrCollectionNotFound, got %v", err)
	}
	if _, err := h.svc.CreateKit(h.orgID, "fest", "  ", "", 1, h.staff); err != service.ErrKitNotFound {
		t.Fatalf("blank name: want ErrKitNotFound, got %v", err)
	}
	if _, err := h.svc.CreateKit(h.orgID, "fest", "Zero", "", 0, h.staff); err != service.ErrInvalidQuantity {
		t.Fatalf("zero quantity: want ErrInvalidQuantity, got %v", err)
	}
}

func TestKitQuantityGuards(t *testing.T) {
	h := newKitHarness(t)
	kitID := h.kit(t, 2)
	a1, a2, a3 := h.checkedIn(t), h.checkedIn(t), h.checkedIn(t)

	if _, err := h.svc.Collect(h.orgID, "fest", kitID.String(), a1.String(), false, "", h.staff); err != nil {
		t.Fatalf("collect 1: %v", err)
	}
	if _, err := h.svc.Collect(h.orgID, "fest", kitID.String(), a2.String(), true, "", h.staff); err != nil {
		t.Fatalf("reserve 2: %v", err)
	}
	// Reservations hold stock: third handout exhausts.
	if _, err := h.svc.Collect(h.orgID, "fest", kitID.String(), a3.String(), false, "", h.staff); err != service.ErrQuantityExhausted {
		t.Fatalf("over total: want ErrQuantityExhausted, got %v", err)
	}

	// Tallies ride the list.
	kits, err := h.svc.ListKits(h.orgID, "fest")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(kits) != 1 || kits[0].Collected != 1 || kits[0].Pending != 1 || kits[0].Remaining != 0 {
		t.Fatalf("bad tallies: %+v", kits)
	}

	// Clamp: total cannot drop below collected (1).
	zero := 0
	if _, err := h.svc.UpdateKit(h.orgID, "fest", kitID.String(), nil, nil, &zero, h.staff); err != service.ErrInvalidQuantity {
		t.Fatalf("zero update: want ErrInvalidQuantity, got %v", err)
	}
	one := 1
	if _, err := h.svc.UpdateKit(h.orgID, "fest", kitID.String(), nil, nil, &one, h.staff); err != nil {
		t.Fatalf("clamp to collected count is fine: %v", err)
	}
	// Below collected is refused: fresh kit, two immediate collects, drop to 1.
	bigID := h.kit(t, 5)
	b1, b2 := h.checkedIn(t), h.checkedIn(t)
	if _, err := h.svc.Collect(h.orgID, "fest", bigID.String(), b1.String(), false, "", h.staff); err != nil {
		t.Fatalf("collect b1: %v", err)
	}
	if _, err := h.svc.Collect(h.orgID, "fest", bigID.String(), b2.String(), false, "", h.staff); err != nil {
		t.Fatalf("collect b2: %v", err)
	}
	if _, err := h.svc.UpdateKit(h.orgID, "fest", bigID.String(), nil, nil, &one, h.staff); err != service.ErrQuantityBelowCollected {
		t.Fatalf("below collected: want ErrQuantityBelowCollected, got %v", err)
	}

	// Delete refused while active rows exist.
	if err := h.svc.DeleteKit(h.orgID, "fest", kitID.String(), h.staff); err != service.ErrKitHasCollections {
		t.Fatalf("delete with actives: want ErrKitHasCollections, got %v", err)
	}
	// Void everything, then delete succeeds (regression: same-tx handle).
	rows, _, err := h.svc.ListCollections(h.orgID, "fest", &kitID, nil, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if _, err := h.svc.Void(h.orgID, "fest", r.Collection.ID.String(), h.staff); err != nil {
			t.Fatalf("void %s: %v", r.Collection.ID, err)
		}
	}
	if err := h.svc.DeleteKit(h.orgID, "fest", kitID.String(), h.staff); err != nil {
		t.Fatalf("delete after void: %v", err)
	}
	if _, err := h.svc.ListKits(h.orgID, "fest"); err != nil {
		t.Fatalf("list after delete: %v", err)
	}
}

func TestKitIdempotentReplay(t *testing.T) {
	h := newKitHarness(t)
	kitID := h.kit(t, 10)
	att := h.checkedIn(t)

	first, err := h.svc.Collect(h.orgID, "fest", kitID.String(), att.String(), false, "tablet-retry-1", h.staff)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	second, err := h.svc.Collect(h.orgID, "fest", kitID.String(), att.String(), false, "tablet-retry-1", h.staff)
	if err != nil {
		t.Fatalf("replay must not error: %v", err)
	}
	if second.Collection.ID != first.Collection.ID {
		t.Fatal("replay must return the existing record")
	}
	var n int64
	if err := h.db.Model(&kitModel.KitCollection{}).Where("kit_id = ?", kitID).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want exactly 1 row after replay, got %d", n)
	}
}

func TestKitDoubleCollectConvergence(t *testing.T) {
	h := newKitHarness(t)
	kitID := h.kit(t, 10)
	att := h.checkedIn(t)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = h.svc.Collect(h.orgID, "fest", kitID.String(), att.String(), false, "", h.staff)
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else if err != service.ErrAlreadyCollected {
			t.Fatalf("loser must see ErrAlreadyCollected, got %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("want exactly 1 winner, errs=%v", errs)
	}
}

// TestRevertBlockedByCollections proves the check-in revert guard: a
// mis-scan undo fails while non-voided handouts exist, then succeeds
// after voiding. Full stack (attendee + checkin + kit) through seams.
func TestRevertBlockedByCollections(t *testing.T) {
	db, _, authSvc := testutil.Setup(t)
	for _, m := range [][]any{attendeeModel.AllModels(), checkinModel.AllModels(), kitModel.AllModels()} {
		if err := db.AutoMigrate(m...); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_kit_collections_active ON kit_collections (kit_id, attendee_id) WHERE status <> 'VOIDED'")
	ev := fakeEvents{a: uuid.New(), b: uuid.New()}
	orgID, staff := uuid.New(), uuid.New()

	attSvc := attendeeService.NewAttendeeService(attendeeRepo.NewAttendeeRepository(db), authSvc, nil, nil)
	checkSvc := checkinService.NewCheckinService(db, checkinRepo.NewCheckinRepository(db), attSvc, ev)
	kitSvc := service.NewKitService(db, repository.NewKitRepository(db), attSvc, ev)
	kitSvc.SetEntitlementProvider(&fakeKitPlans{ent: submodel.EntitlementForPlan(submodel.PlanPro)})
	checkSvc.SetCollectionGuard(kitSvc)

	minted, err := attSvc.MintForOrder(db, uuid.New(), uuid.New(), ev.a, "fan@test.com", "Fan", 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	attID := minted[0].ID

	// Manual door scan (no QR secret needed).
	r, err := checkSvc.Scan(orgID, "fest", "", attID.String(), staff)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if r.Outcome != checkinModel.OutcomeCheckedIn {
		t.Fatalf("want CHECKED_IN, got %s", r.Outcome)
	}

	k, err := kitSvc.CreateKit(orgID, "fest", "Pack", "", 5, staff)
	if err != nil {
		t.Fatalf("create kit: %v", err)
	}
	v, err := kitSvc.Collect(orgID, "fest", k.Kit.ID.String(), attID.String(), false, "", staff)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	if _, err := checkSvc.Revert(orgID, "fest", attID.String(), staff); err != checkinService.ErrCollectionsOutstanding {
		t.Fatalf("revert with handout: want ErrCollectionsOutstanding, got %v", err)
	}
	if _, err := kitSvc.Void(orgID, "fest", v.Collection.ID.String(), staff); err != nil {
		t.Fatalf("void: %v", err)
	}
	rr, err := checkSvc.Revert(orgID, "fest", attID.String(), staff)
	if err != nil {
		t.Fatalf("revert after void: %v", err)
	}
	if rr.Outcome != checkinModel.OutcomeReverted {
		t.Fatalf("want REVERTED, got %s", rr.Outcome)
	}
}

type fakeKitPlans struct {
	ent submodel.Entitlement
}

func (f *fakeKitPlans) EntitlementFor(_ uuid.UUID) (submodel.Entitlement, error) {
	return f.ent, nil
}

func TestKitPlanGates(t *testing.T) {
	newKitHarnessWithPlans := func(t *testing.T, ent submodel.Entitlement) *kitHarness {
		t.Helper()
		h := newKitHarness(t)
		h.svc.SetEntitlementProvider(&fakeKitPlans{ent: ent})
		return h
	}

	t.Run("free tier cannot define kits", func(t *testing.T) {
		h := newKitHarnessWithPlans(t, submodel.FreeEntitlement())
		if _, err := h.svc.CreateKit(h.orgID, "fest", "Pack", "", 5, h.staff); err != subservice.ErrUpgradeRequired {
			t.Fatalf("want ErrUpgradeRequired, got %v", err)
		}
	})
	t.Run("per-event kit count enforced", func(t *testing.T) {
		ent := submodel.EntitlementForPlan(submodel.PlanPro)
		ent.Limits.MaxKitsPerEvent = 1
		h := newKitHarnessWithPlans(t, ent)
		if _, err := h.svc.CreateKit(h.orgID, "fest", "One", "", 5, h.staff); err != nil {
			t.Fatalf("first: %v", err)
		}
		if _, err := h.svc.CreateKit(h.orgID, "fest", "Two", "", 5, h.staff); err != subservice.ErrUpgradeRequired {
			t.Fatalf("second: want ErrUpgradeRequired, got %v", err)
		}
	})
}
