package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	attendeemodel "github.com/nikhea/rallya/internal/attendee/model"
	checkinmodel "github.com/nikhea/rallya/internal/checkin/model"
	"github.com/nikhea/rallya/internal/checkin/service"
)

func TestRevertMisScan(t *testing.T) {
	h := newHarness(t)
	id, code := h.mint(t, h.events.a)

	if _, err := h.svc.Scan(h.orgID, "fest", code, "", h.staff); err != nil {
		t.Fatalf("scan: %v", err)
	}
	r, err := h.svc.Revert(h.orgID, "fest", id.String(), h.staff)
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if r.Outcome != checkinmodel.OutcomeReverted || r.Method != checkinmodel.MethodManual {
		t.Fatalf("want manual REVERTED, got %+v", r)
	}

	// Row is REGISTERED again with no timestamp: rescans land fresh.
	var a attendeemodel.Attendee
	if err := h.db.First(&a, "id = ?", id).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if a.Status != attendeemodel.AttendeeStatusRegistered || a.CheckedInAt != nil {
		t.Fatalf("want REGISTERED without timestamp, got %+v", a)
	}
	r2, err := h.svc.Scan(h.orgID, "fest", code, "", h.staff)
	if err != nil || r2.Outcome != checkinmodel.OutcomeCheckedIn {
		t.Fatalf("post-revert scan: want fresh CHECKED_IN, got %+v, %v", r2, err)
	}
}

func TestRevertGuards(t *testing.T) {
	h := newHarness(t)

	// Never-scanned row: 422.
	id, _ := h.mint(t, h.events.a)
	if _, err := h.svc.Revert(h.orgID, "fest", id.String(), h.staff); !errors.Is(err, service.ErrNotCheckedIn) {
		t.Fatalf("registered revert: want ErrNotCheckedIn, got %v", err)
	}

	// Cancelled row: 422 (terminal, no revert).
	if err := h.db.Model(&attendeemodel.Attendee{}).Where("id = ?", id).
		Update("status", attendeemodel.AttendeeStatusCancelled).Error; err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := h.svc.Revert(h.orgID, "fest", id.String(), h.staff); !errors.Is(err, service.ErrNotCheckedIn) {
		t.Fatalf("cancelled revert: want ErrNotCheckedIn, got %v", err)
	}

	// Wrong event: stealth 404.
	id2, code2 := h.mint(t, h.events.b)
	if _, err := h.svc.Scan(h.orgID, "other", code2, "", h.staff); err != nil {
		t.Fatalf("scan other: %v", err)
	}
	if _, err := h.svc.Revert(h.orgID, "fest", id2.String(), h.staff); !errors.Is(err, service.ErrAttendeeNotFound) {
		t.Fatalf("wrong-event revert: want ErrAttendeeNotFound, got %v", err)
	}

	// Unknown ID: 404.
	if _, err := h.svc.Revert(h.orgID, "fest", uuid.New().String(), h.staff); !errors.Is(err, service.ErrAttendeeNotFound) {
		t.Fatalf("unknown revert: want ErrAttendeeNotFound, got %v", err)
	}
}
