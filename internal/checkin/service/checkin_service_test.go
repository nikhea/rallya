package service_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	attendeemodel "github.com/nikhea/rallya/internal/attendee/model"
	"github.com/nikhea/rallya/internal/attendee/repository"
	attendeeservice "github.com/nikhea/rallya/internal/attendee/service"
	attendeeutils "github.com/nikhea/rallya/internal/attendee/utils"
	"github.com/nikhea/rallya/internal/auth/testutil"
	checkinmodel "github.com/nikhea/rallya/internal/checkin/model"
	checkinrepo "github.com/nikhea/rallya/internal/checkin/repository"
	"github.com/nikhea/rallya/internal/checkin/service"
)

var testSecret = []byte("door-test-secret-32-bytes-padded!!")

type fakeEvents struct{ a, b uuid.UUID }

func (f fakeEvents) ResolveEventID(_ uuid.UUID, ref string) (uuid.UUID, error) {
	if ref == "other" {
		return f.b, nil
	}
	return f.a, nil
}

type checkinHarness struct {
	db     *gorm.DB
	svc    *service.CheckinService
	attSvc *attendeeservice.AttendeeService
	orgID  uuid.UUID
	events fakeEvents
	staff  uuid.UUID
}

func newHarness(t *testing.T) *checkinHarness {
	t.Helper()
	db, _, authSvc := testutil.Setup(t)
	for _, m := range [][]any{attendeemodel.AllModels(), checkinmodel.AllModels()} {
		if err := db.AutoMigrate(m...); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	attRepo := repository.NewAttendeeRepository(db)
	attSvc := attendeeservice.NewAttendeeService(attRepo, authSvc, nil, nil)
	cRepo := checkinrepo.NewCheckinRepository(db)
	ev := fakeEvents{a: uuid.New(), b: uuid.New()}
	svc := service.NewCheckinService(db, cRepo, attSvc, ev)
	svc.SetQRSecret(testSecret)
	return &checkinHarness{db: db, svc: svc, attSvc: attSvc, orgID: uuid.New(), events: ev, staff: uuid.New()}
}

// mint inserts one REGISTERED row and returns its QR code.
func (h *checkinHarness) mint(t *testing.T, eventID uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	minted, err := h.attSvc.MintForOrder(h.db, uuid.New(), uuid.New(), eventID, "fan@test.com", "Fan", 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return minted[0].ID, attendeeutils.BuildPayload(minted[0].ID, minted[0].QRToken, testSecret)
}

func TestScanFreshThenRescan(t *testing.T) {
	h := newHarness(t)
	id, code := h.mint(t, h.events.a)

	r, err := h.svc.Scan(h.orgID, "fest", code, "", h.staff)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if r.Outcome != checkinmodel.OutcomeCheckedIn || r.CheckedInAt == nil {
		t.Fatalf("want CHECKED_IN with timestamp, got %+v", r)
	}

	r2, err := h.svc.Scan(h.orgID, "fest", code, "", h.staff)
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if r2.Outcome != checkinmodel.OutcomeAlreadyCheckedIn {
		t.Fatalf("want ALREADY_CHECKED_IN, got %s", r2.Outcome)
	}
	if *r2.AttendeeID != id {
		t.Fatalf("rescan must attribute the row")
	}
}

func TestScanRefusals(t *testing.T) {
	h := newHarness(t)

	// Malformed: unscannable, NULL-attendee log.
	r, err := h.svc.Scan(h.orgID, "fest", "not-a-code", "", h.staff)
	if err != nil {
		t.Fatalf("malformed: %v", err)
	}
	if r.Outcome != checkinmodel.OutcomeInvalidCode || r.AttendeeID != nil {
		t.Fatalf("malformed: want bare INVALID_CODE, got %+v", r)
	}

	// Forged: valid shape, broken HMAC (flip a token char).
	parts := strings.Split(h.mustMintCode(t), ".")
	parts[1] = "x" + parts[1][1:]
	r, err = h.svc.Scan(h.orgID, "fest", strings.Join(parts, "."), "", h.staff)
	if err != nil {
		t.Fatalf("forged: %v", err)
	}
	if r.Outcome != checkinmodel.OutcomeInvalidCode || r.AttendeeID != nil {
		t.Fatalf("forged: want bare INVALID_CODE, got %+v", r)
	}

	// Valid HMAC, unknown row: stealth INVALID_CODE (no oracle).
	ghost := attendeeutils.BuildPayload(uuid.New(), "tokentokentokentokentokentoken12", testSecret)
	r, err = h.svc.Scan(h.orgID, "fest", ghost, "", h.staff)
	if err != nil {
		t.Fatalf("ghost: %v", err)
	}
	if r.Outcome != checkinmodel.OutcomeInvalidCode {
		t.Fatalf("ghost: want INVALID_CODE, got %s", r.Outcome)
	}

	// Valid ticket, wrong door.
	_, code := h.mint(t, h.events.b)
	r, err = h.svc.Scan(h.orgID, "fest", code, "", h.staff)
	if err != nil {
		t.Fatalf("wrong event: %v", err)
	}
	if r.Outcome != checkinmodel.OutcomeWrongEvent {
		t.Fatalf("wrong event: want WRONG_EVENT, got %s", r.Outcome)
	}

	// Cancelled row refuses.
	id, code := h.mint(t, h.events.a)
	if err := h.db.Model(&attendeemodel.Attendee{}).Where("id = ?", id).
		Update("status", attendeemodel.AttendeeStatusCancelled).Error; err != nil {
		t.Fatalf("cancel: %v", err)
	}
	r, err = h.svc.Scan(h.orgID, "fest", code, "", h.staff)
	if err != nil {
		t.Fatalf("cancelled: %v", err)
	}
	if r.Outcome != checkinmodel.OutcomeCancelled {
		t.Fatalf("cancelled: want CANCELLED, got %s", r.Outcome)
	}
}

func (h *checkinHarness) mustMintCode(t *testing.T) string {
	t.Helper()
	_, code := h.mint(t, h.events.a)
	return code
}

func TestScanManual(t *testing.T) {
	h := newHarness(t)
	id, _ := h.mint(t, h.events.a)

	r, err := h.svc.Scan(h.orgID, "fest", "", id.String(), h.staff)
	if err != nil {
		t.Fatalf("manual: %v", err)
	}
	if r.Outcome != checkinmodel.OutcomeCheckedIn || r.Method != checkinmodel.MethodManual {
		t.Fatalf("want manual CHECKED_IN, got %+v", r)
	}

	// Unknown ID is a real 404 (staff picked a bad roster row).
	r2, err := h.svc.Scan(h.orgID, "fest", "", uuid.New().String(), h.staff)
	if err == nil || r2 != nil {
		t.Fatalf("want ErrAttendeeNotFound, got %+v, %v", r2, err)
	}
}

func TestScanBatchMixed(t *testing.T) {
	h := newHarness(t)
	_, good := h.mint(t, h.events.a)
	results, err := h.svc.ScanBatch(h.orgID, "fest", []string{good, "junk", good}, h.staff)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	want := []checkinmodel.Outcome{
		checkinmodel.OutcomeCheckedIn,
		checkinmodel.OutcomeInvalidCode,
		checkinmodel.OutcomeAlreadyCheckedIn,
	}
	for i, w := range want {
		if results[i].Outcome != w {
			t.Fatalf("item %d: want %s, got %s", i, w, results[i].Outcome)
		}
	}

	// Over the cap.
	big := make([]string, service.MaxBatchSize+1)
	for i := range big {
		big[i] = "junk"
	}
	if _, err := h.svc.ScanBatch(h.orgID, "fest", big, h.staff); err == nil {
		t.Fatalf("want ErrBatchTooLarge")
	}
}

func TestConcurrentDoubleScan(t *testing.T) {
	h := newHarness(t)
	_, code := h.mint(t, h.events.a)

	const n = 8
	outcomes := make([]checkinmodel.Outcome, n)
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := h.svc.Scan(h.orgID, "fest", code, "", h.staff)
			if err != nil {
				t.Errorf("scan %d: %v", i, err)
				return
			}
			outcomes[i] = r.Outcome
		}(i)
	}
	wg.Wait()

	fresh, seen := 0, 0
	for _, o := range outcomes {
		switch o {
		case checkinmodel.OutcomeCheckedIn:
			fresh++
		case checkinmodel.OutcomeAlreadyCheckedIn:
			seen++
		default:
			t.Fatalf("unexpected outcome %s", o)
		}
	}
	if fresh != 1 || seen != n-1 {
		t.Fatalf("want exactly 1 fresh + %d rescans, got %d + %d", n-1, fresh, seen)
	}
}

func TestStats(t *testing.T) {
	h := newHarness(t)
	_, code1 := h.mint(t, h.events.a)
	h.mint(t, h.events.a)
	id3, _ := h.mint(t, h.events.a)
	if _, err := h.svc.Scan(h.orgID, "fest", code1, "", h.staff); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := h.db.Model(&attendeemodel.Attendee{}).Where("id = ?", id3).
		Update("status", attendeemodel.AttendeeStatusCancelled).Error; err != nil {
		t.Fatalf("cancel: %v", err)
	}

	reg, checked, cancelled, err := h.svc.Stats(h.orgID, "fest")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if reg != 1 || checked != 1 || cancelled != 1 {
		t.Fatalf("want 1/1/1, got %d/%d/%d", reg, checked, cancelled)
	}
}
