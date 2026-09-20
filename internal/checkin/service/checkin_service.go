package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/attendee/model"
	attendeeutils "github.com/nikhea/rallya/internal/attendee/utils"
	checkinmodel "github.com/nikhea/rallya/internal/checkin/model"
	"github.com/nikhea/rallya/internal/checkin/repository"
)

// MaxBatchSize caps door batches (413 over the limit).
const MaxBatchSize = 50

// ScanVerdict is the atomic row outcome (owned here; the attendees domain
// implements CheckinApplier against this shape — MintedAttendee precedent).
// Business refusals ride the verdict, never errors: only truly unknown
// rows surface gorm.ErrRecordNotFound.
type ScanVerdict struct {
	EventOK     bool
	TokenOK     bool
	Status      model.AttendeeStatus
	Flipped     bool
	CheckedInAt *time.Time
}

// CheckinApplier is the consumer-declared seam into the attendees domain:
// the row flip runs there (it owns the table), logging and QR verify here.
type CheckinApplier interface {
	ApplyCheckin(tx *gorm.DB, attendeeID uuid.UUID, rawToken string, eventID uuid.UUID) (*ScanVerdict, error)
	ApplyCheckinManual(tx *gorm.DB, attendeeID, eventID uuid.UUID) (*ScanVerdict, error)
	RevertCheckin(tx *gorm.DB, attendeeID, eventID uuid.UUID) (*RevertVerdict, error)
	CountByStatus(eventID uuid.UUID) (map[string]int64, error)
}

// RevertVerdict is the atomic revert outcome (owned here; implemented by
// attendees — ScanVerdict precedent).
type RevertVerdict struct {
	EventOK  bool
	Status   model.AttendeeStatus
	Reverted bool
}

// EventResolver resolves event refs within an org (implemented by events).
type EventResolver interface {
	ResolveEventID(orgID uuid.UUID, ref string) (uuid.UUID, error)
}

// CheckinService orchestrates door scans.
type CheckinService struct {
	db      *gorm.DB
	repo    *repository.CheckinRepository
	attends CheckinApplier
	events  EventResolver
	secret  []byte
}

// NewCheckinService builds the service. Secret arrives via SetQRSecret
// (resolved once at boot — fail-closed, never os.Exit per request).
func NewCheckinService(db *gorm.DB, repo *repository.CheckinRepository, attends CheckinApplier, events EventResolver) *CheckinService {
	return &CheckinService{db: db, repo: repo, attends: attends, events: events}
}

// SetQRSecret injects the HMAC secret (same value as attendees minting).
func (s *CheckinService) SetQRSecret(secret []byte) { s.secret = secret }

// ScanResult is one scan outcome (handler maps to dto; service stays
// wire-shape free).
type ScanResult struct {
	Outcome     checkinmodel.Outcome
	Method      checkinmodel.Method
	AttendeeID  *uuid.UUID
	CheckedInAt *time.Time
}

// OutcomeError carries a refusal back to the handler as data, not failure:
// the handler returns 200 with the outcome.
type OutcomeError struct {
	Outcome    checkinmodel.Outcome
	AttendeeID *uuid.UUID
}

func (e *OutcomeError) Error() string { return "checkin refused: " + string(e.Outcome) }

// Scan checks in one code: raw QR string or roster attendee ID.
func (s *CheckinService) Scan(orgID uuid.UUID, eventRef, code, attendeeID string, staffID uuid.UUID) (*ScanResult, error) {
	if code != "" {
		return s.scanQR(orgID, eventRef, code, staffID)
	}
	return s.scanManual(orgID, eventRef, attendeeID, staffID)
}

// scanQR verifies the HMAC, then applies the flip atomically.
func (s *CheckinService) scanQR(orgID uuid.UUID, eventRef, code string, staffID uuid.UUID) (*ScanResult, error) {
	if len(s.secret) == 0 {
		return nil, ErrQRSecretUnset
	}
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrCheckinNotFound
	}
	vp, err := attendeeutils.VerifyPayload(code, s.secret)
	if err != nil {
		// Unscannable: nothing to attribute — log with NULL attendee.
		_ = s.repo.InsertTx(nil, &checkinmodel.CheckinLog{
			EventID: eventID, ScannedBy: staffID,
			Outcome: checkinmodel.OutcomeInvalidCode, Method: checkinmodel.MethodQR,
		})
		return &ScanResult{Outcome: checkinmodel.OutcomeInvalidCode, Method: checkinmodel.MethodQR}, nil
	}
	res := &ScanResult{Method: checkinmodel.MethodQR, AttendeeID: &vp.AttendeeID}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		verdict, err := s.attends.ApplyCheckin(tx, vp.AttendeeID, vp.RawToken, eventID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				res.Outcome = checkinmodel.OutcomeInvalidCode
				return s.repo.InsertTx(tx, &checkinmodel.CheckinLog{
					EventID: eventID, AttendeeID: &vp.AttendeeID, ScannedBy: staffID,
					Outcome: res.Outcome, Method: checkinmodel.MethodQR,
				})
			}
			return err
		}
		res.Outcome = verdictOutcome(verdict)
		res.CheckedInAt = verdict.CheckedInAt
		return s.repo.InsertTx(tx, &checkinmodel.CheckinLog{
			EventID: eventID, AttendeeID: &vp.AttendeeID, ScannedBy: staffID,
			Outcome: res.Outcome, Method: checkinmodel.MethodQR,
		})
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// scanManual checks in by roster ID (QR-less fallback), logged as manual.
func (s *CheckinService) scanManual(orgID uuid.UUID, eventRef, attendeeRef string, staffID uuid.UUID) (*ScanResult, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrCheckinNotFound
	}
	attendeeID, err := uuid.Parse(attendeeRef)
	if err != nil {
		return nil, ErrAttendeeNotFound
	}
	res := &ScanResult{Method: checkinmodel.MethodManual, AttendeeID: &attendeeID}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		verdict, err := s.attends.ApplyCheckinManual(tx, attendeeID, eventID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAttendeeNotFound
			}
			return err
		}
		res.Outcome = verdictOutcome(verdict)
		res.CheckedInAt = verdict.CheckedInAt
		return s.repo.InsertTx(tx, &checkinmodel.CheckinLog{
			EventID: eventID, AttendeeID: &attendeeID, ScannedBy: staffID,
			Outcome: res.Outcome, Method: checkinmodel.MethodManual,
		})
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ScanBatch scans QR codes with per-item outcomes (no fail-fast).
func (s *CheckinService) ScanBatch(orgID uuid.UUID, eventRef string, codes []string, staffID uuid.UUID) ([]ScanResult, error) {
	if len(codes) > MaxBatchSize {
		return nil, ErrBatchTooLarge
	}
	out := make([]ScanResult, 0, len(codes))
	for _, code := range codes {
		r, err := s.scanQR(orgID, eventRef, code, staffID)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, nil
}

// Revert undoes a mis-scan: CHECKED_IN -> REGISTERED, timestamp cleared,
// the attempt logged as REVERTED. Only CHECKED_IN rows revert (anything
// else is a 422); wrong-event rows stealth-404.
func (s *CheckinService) Revert(orgID uuid.UUID, eventRef, attendeeRef string, staffID uuid.UUID) (*ScanResult, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrCheckinNotFound
	}
	attendeeID, err := uuid.Parse(attendeeRef)
	if err != nil {
		return nil, ErrAttendeeNotFound
	}
	res := &ScanResult{Outcome: checkinmodel.OutcomeReverted, Method: checkinmodel.MethodManual, AttendeeID: &attendeeID}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		verdict, err := s.attends.RevertCheckin(tx, attendeeID, eventID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAttendeeNotFound
			}
			return err
		}
		if !verdict.EventOK {
			return ErrAttendeeNotFound
		}
		if !verdict.Reverted {
			return ErrNotCheckedIn
		}
		return s.repo.InsertTx(tx, &checkinmodel.CheckinLog{
			EventID: eventID, AttendeeID: &attendeeID, ScannedBy: staffID,
			Outcome: res.Outcome, Method: checkinmodel.MethodManual,
		})
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Stats tallies the roster for the door dashboard.
func (s *CheckinService) Stats(orgID uuid.UUID, eventRef string) (registered, checkedIn, cancelled int64, err error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return 0, 0, 0, ErrCheckinNotFound
	}
	counts, err := s.attends.CountByStatus(eventID)
	if err != nil {
		return 0, 0, 0, err
	}
	return counts[string(model.AttendeeStatusRegistered)],
		counts[string(model.AttendeeStatusCheckedIn)],
		counts[string(model.AttendeeStatusCancelled)], nil
}

// verdictOutcome maps a row verdict to the wire outcome. Token mismatch
// collapses into INVALID_CODE (stealth — no oracle for code probers), but a
// cryptographically VALID code for another event reports WRONG_EVENT (only
// reachable with a genuine ticket, and genuinely useful at multi-event
// venues). Manual scans set TokenOK, so they skip the token arm.
func verdictOutcome(v *ScanVerdict) checkinmodel.Outcome {
	switch {
	case !v.TokenOK:
		return checkinmodel.OutcomeInvalidCode
	case !v.EventOK:
		return checkinmodel.OutcomeWrongEvent
	case v.Flipped:
		return checkinmodel.OutcomeCheckedIn
	case v.Status == model.AttendeeStatusCheckedIn:
		return checkinmodel.OutcomeAlreadyCheckedIn
	case v.Status == model.AttendeeStatusCancelled:
		return checkinmodel.OutcomeCancelled
	default:
		return checkinmodel.OutcomeInvalidCode
	}
}
