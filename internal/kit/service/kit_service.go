package service

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	attendeeModel "github.com/nikhea/rallya/internal/attendee/model"
	auditModel "github.com/nikhea/rallya/internal/audit/model"
	auditSvc "github.com/nikhea/rallya/internal/audit/service"
	kitModel "github.com/nikhea/rallya/internal/kit/model"
	"github.com/nikhea/rallya/internal/kit/repository"
)

// EventResolver resolves event refs within an org (implemented by events).
type EventResolver interface {
	ResolveEventID(orgID uuid.UUID, ref string) (uuid.UUID, error)
}

// AttendeeChecker is the consumer-declared seam into the attendees domain:
// collection eligibility reads the row status there (it owns the table).
// Unknown rows and cross-event rows surface gorm.ErrRecordNotFound.
type AttendeeChecker interface {
	AttendeeStatusInEvent(attendeeID, eventID uuid.UUID) (attendeeModel.AttendeeStatus, error)
}

// KitService orchestrates kit types and handouts.
type KitService struct {
	db      *gorm.DB
	repo    *repository.KitRepository
	attends AttendeeChecker
	events  EventResolver
	auditor auditSvc.Emitter
}

// NewKitService builds the service.
func NewKitService(db *gorm.DB, repo *repository.KitRepository, attends AttendeeChecker, events EventResolver) *KitService {
	return &KitService{db: db, repo: repo, attends: attends, events: events}
}

// SetAuditEmitter wires audit-trail emission (nil-safe when absent).
func (s *KitService) SetAuditEmitter(a auditSvc.Emitter) { s.auditor = a }

// HasActiveCollections reports whether an attendee holds any non-voided
// handout. Implements the check-in domain's CollectionGuard contract
// (revert guard) — nil-tx falls back to the shared handle.
func (s *KitService) HasActiveCollections(tx *gorm.DB, attendeeID uuid.UUID) (bool, error) {
	return s.repo.HasActiveCollections(tx, attendeeID)
}

// audit records kit actions with changed-fields-only diffs.
func (s *KitService) audit(tx *gorm.DB, orgID, staffID uuid.UUID, action, objectType string, objectID *uuid.UUID, after map[string]any) error {
	if s.auditor == nil {
		return nil
	}
	return s.auditor.EmitTx(tx, auditSvc.Entry{
		OrgID: &orgID, ActorID: &staffID,
		Action: action, ObjectType: objectType, ObjectID: objectID,
		After: after,
	})
}

// KitWithCounts is one kit type with live handout tallies (service stays
// wire-shape free; the handler maps to dto).
type KitWithCounts struct {
	Kit       kitModel.Kit
	Pending   int64
	Collected int64
	Voided    int64
	Remaining int64
}

// CollectionView is one handout with its kit name resolved.
type CollectionView struct {
	Collection kitModel.KitCollection
	KitName    string
}

// CreateKit defines one named kit type for an event.
func (s *KitService) CreateKit(orgID uuid.UUID, eventRef, name, description string, quantityTotal int, staffID uuid.UUID) (*KitWithCounts, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrKitNotFound
	}
	if strings.TrimSpace(name) == "" {
		return nil, ErrKitNotFound
	}
	if quantityTotal < 1 {
		return nil, ErrInvalidQuantity
	}
	k := &kitModel.Kit{
		EventID: eventID, Name: strings.TrimSpace(name),
		Description: strings.TrimSpace(description), QuantityTotal: quantityTotal,
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateKit(tx, k); err != nil {
			return err
		}
		return s.audit(tx, orgID, staffID, "kit.created", auditModel.ObjectKit, &k.ID, map[string]any{
			"name": k.Name, "quantityTotal": k.QuantityTotal,
		})
	})
	if err != nil {
		return nil, err
	}
	return &KitWithCounts{Kit: *k, Remaining: int64(quantityTotal)}, nil
}

// ListKits returns an event's kit types with live tallies.
func (s *KitService) ListKits(orgID uuid.UUID, eventRef string) ([]KitWithCounts, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrKitNotFound
	}
	kits, err := s.repo.ListKitsByEvent(eventID)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountsByKit(eventID)
	if err != nil {
		return nil, err
	}
	out := make([]KitWithCounts, 0, len(kits))
	for _, k := range kits {
		m := counts[k.ID]
		pending := m[string(kitModel.CollectionStatusPending)]
		collected := m[string(kitModel.CollectionStatusCollected)]
		voided := m[string(kitModel.CollectionStatusVoided)]
		remaining := int64(k.QuantityTotal) - pending - collected
		if remaining < 0 {
			remaining = 0
		}
		out = append(out, KitWithCounts{Kit: k, Pending: pending, Collected: collected, Voided: voided, Remaining: remaining})
	}
	return out, nil
}

// UpdateKit patches name/description/quantity. QuantityTotal cannot drop
// below the already-collected count (422).
func (s *KitService) UpdateKit(orgID uuid.UUID, eventRef, kitRef string, name, description *string, quantityTotal *int, staffID uuid.UUID) (*KitWithCounts, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrKitNotFound
	}
	kitID, err := uuid.Parse(strings.TrimSpace(kitRef))
	if err != nil {
		return nil, ErrKitNotFound
	}
	var updated kitModel.Kit
	err = s.db.Transaction(func(tx *gorm.DB) error {
		k, err := s.repo.GetKitForUpdate(tx, kitID)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrKitNotFound
			}
			return err
		}
		if k.EventID != eventID {
			return ErrKitNotFound
		}
		if name != nil {
			if strings.TrimSpace(*name) == "" {
				return ErrKitNotFound
			}
			k.Name = strings.TrimSpace(*name)
		}
		if description != nil {
			k.Description = strings.TrimSpace(*description)
		}
		if quantityTotal != nil {
			if *quantityTotal < 1 {
				return ErrInvalidQuantity
			}
			var collected int64
			if err := tx.Model(&kitModel.KitCollection{}).
				Where("kit_id = ? AND status = ?", kitID, kitModel.CollectionStatusCollected).
				Count(&collected).Error; err != nil {
				return err
			}
			if int64(*quantityTotal) < collected {
				return ErrQuantityBelowCollected
			}
			k.QuantityTotal = *quantityTotal
		}
		if err := s.repo.UpdateKit(tx, k); err != nil {
			return err
		}
		updated = *k
		return s.audit(tx, orgID, staffID, "kit.updated", auditModel.ObjectKit, &k.ID, map[string]any{
			"name": k.Name, "quantityTotal": k.QuantityTotal,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.withCounts(eventID, updated)
}

// DeleteKit removes a kit type. Refused while active (non-voided)
// collections exist (422) — void them first.
func (s *KitService) DeleteKit(orgID uuid.UUID, eventRef, kitRef string, staffID uuid.UUID) error {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return ErrKitNotFound
	}
	kitID, err := uuid.Parse(strings.TrimSpace(kitRef))
	if err != nil {
		return ErrKitNotFound
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		k, err := s.repo.GetKitForUpdate(tx, kitID)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrKitNotFound
			}
			return err
		}
		if k.EventID != eventID {
			return ErrKitNotFound
		}
		active, err := s.repo.CountActive(tx, kitID)
		if err != nil {
			return err
		}
		if active > 0 {
			return ErrKitHasCollections
		}
		if err := s.repo.DeleteKit(tx, kitID); err != nil {
			return err
		}
		return s.audit(tx, orgID, staffID, "kit.deleted", auditModel.ObjectKit, &kitID, map[string]any{"name": k.Name})
	})
}

// Collect hands a kit to a checked-in attendee. Default collects
// immediately; reserve holds a PENDING unit. IdempotencyKey makes tablet
// retries converge on the existing record.
func (s *KitService) Collect(orgID uuid.UUID, eventRef, kitRef, attendeeRef string, reserve bool, idempotencyKey string, staffID uuid.UUID) (*CollectionView, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrKitNotFound
	}
	kitID, err := uuid.Parse(strings.TrimSpace(kitRef))
	if err != nil {
		return nil, ErrKitNotFound
	}
	attendeeID, err := uuid.Parse(strings.TrimSpace(attendeeRef))
	if err != nil {
		return nil, ErrAttendeeNotFound
	}
	status, err := s.attends.AttendeeStatusInEvent(attendeeID, eventID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrAttendeeNotFound
		}
		return nil, err
	}
	if status != attendeeModel.AttendeeStatusCheckedIn {
		return nil, ErrNotCheckedIn
	}
	var out *kitModel.KitCollection
	var kitName string
	err = s.db.Transaction(func(tx *gorm.DB) error {
		k, err := s.repo.GetKitForUpdate(tx, kitID)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrKitNotFound
			}
			return err
		}
		if k.EventID != eventID {
			return ErrKitNotFound
		}
		kitName = k.Name
		if idempotencyKey != "" {
			existing, err := s.repo.FindByIdempotency(tx, kitID, attendeeID, idempotencyKey)
			if err != nil {
				return err
			}
			if existing != nil {
				out = existing
				return nil
			}
		}
		active, err := s.repo.CountActive(tx, kitID)
		if err != nil {
			return err
		}
		if active >= int64(k.QuantityTotal) {
			return ErrQuantityExhausted
		}
		c := &kitModel.KitCollection{
			KitID: kitID, EventID: eventID, AttendeeID: attendeeID,
			IdempotencyKey: idempotencyKey, Status: kitModel.CollectionStatusPending,
		}
		action := "kit.reserved"
		after := map[string]any{"kitId": kitID.String(), "attendeeId": attendeeID.String(), "status": string(c.Status)}
		if !reserve {
			now := time.Now().UTC()
			c.Status = kitModel.CollectionStatusCollected
			c.CollectedAt = &now
			c.CollectedBy = &staffID
			action = "kit.collected"
			after["status"] = string(c.Status)
		}
		if err := s.repo.CreateCollection(tx, c); err != nil {
			if isConflictErr(err) {
				return ErrAlreadyCollected
			}
			return err
		}
		out = c
		return s.audit(tx, orgID, staffID, action, auditModel.ObjectKitCollection, &c.ID, after)
	})
	if err != nil {
		return nil, err
	}
	return &CollectionView{Collection: *out, KitName: kitName}, nil
}

// MarkCollected flips PENDING -> COLLECTED with staff stamps.
func (s *KitService) MarkCollected(orgID uuid.UUID, eventRef, collectionRef string, staffID uuid.UUID) (*CollectionView, error) {
	return s.transition(orgID, eventRef, collectionRef, staffID, kitModel.CollectionStatusCollected, "kit.collected",
		map[kitModel.CollectionStatus]bool{kitModel.CollectionStatusPending: true})
}

// Void cancels a PENDING or COLLECTED handout (frees re-issue).
func (s *KitService) Void(orgID uuid.UUID, eventRef, collectionRef string, staffID uuid.UUID) (*CollectionView, error) {
	return s.transition(orgID, eventRef, collectionRef, staffID, kitModel.CollectionStatusVoided, "kit.voided",
		map[kitModel.CollectionStatus]bool{kitModel.CollectionStatusPending: true, kitModel.CollectionStatusCollected: true})
}

func (s *KitService) transition(orgID uuid.UUID, eventRef, collectionRef string, staffID uuid.UUID, to kitModel.CollectionStatus, action string, from map[kitModel.CollectionStatus]bool) (*CollectionView, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrCollectionNotFound
	}
	id, err := uuid.Parse(strings.TrimSpace(collectionRef))
	if err != nil {
		return nil, ErrCollectionNotFound
	}
	var out *kitModel.KitCollection
	var kitName string
	err = s.db.Transaction(func(tx *gorm.DB) error {
		c, err := s.repo.GetCollectionForUpdate(tx, id)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrCollectionNotFound
			}
			return err
		}
		if c.EventID != eventID {
			return ErrCollectionNotFound
		}
		if !from[c.Status] {
			return ErrInvalidStatus
		}
		now := time.Now().UTC()
		c.Status = to
		if to == kitModel.CollectionStatusCollected {
			c.CollectedAt = &now
			c.CollectedBy = &staffID
		}
		if err := s.repo.UpdateCollection(tx, c); err != nil {
			return err
		}
		out = c
		k, err := s.repo.GetKitByID(tx, c.KitID)
		if err != nil {
			return err
		}
		kitName = k.Name
		return s.audit(tx, orgID, staffID, action, auditModel.ObjectKitCollection, &c.ID, map[string]any{"status": string(to)})
	})
	if err != nil {
		return nil, err
	}
	return &CollectionView{Collection: *out, KitName: kitName}, nil
}

// ListCollections returns an event's handouts with optional filters.
func (s *KitService) ListCollections(orgID uuid.UUID, eventRef string, kitID, attendeeID *uuid.UUID, status string) ([]CollectionView, int64, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, 0, ErrKitNotFound
	}
	if status != "" {
		switch kitModel.CollectionStatus(status) {
		case kitModel.CollectionStatusPending, kitModel.CollectionStatusCollected, kitModel.CollectionStatusVoided:
		default:
			return nil, 0, ErrInvalidStatus
		}
	}
	rows, total, err := s.repo.ListCollections(eventID, repository.CollectionFilter{
		KitID: kitID, Status: status, AttendeeID: attendeeID,
	})
	if err != nil {
		return nil, 0, err
	}
	return s.withNames(eventID, rows, total)
}

// AttendeeCollections answers the door-table question: every kit handout
// for one person at one event.
func (s *KitService) AttendeeCollections(orgID uuid.UUID, eventRef, attendeeRef string) ([]CollectionView, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrAttendeeNotFound
	}
	attendeeID, err := uuid.Parse(strings.TrimSpace(attendeeRef))
	if err != nil {
		return nil, ErrAttendeeNotFound
	}
	rows, _, err := s.repo.ListCollections(eventID, repository.CollectionFilter{AttendeeID: &attendeeID})
	if err != nil {
		return nil, err
	}
	views, _, err := s.withNames(eventID, rows, int64(len(rows)))
	return views, err
}

func (s *KitService) withNames(eventID uuid.UUID, rows []kitModel.KitCollection, total int64) ([]CollectionView, int64, error) {
	kits, err := s.repo.ListKitsByEvent(eventID)
	if err != nil {
		return nil, 0, err
	}
	names := make(map[uuid.UUID]string, len(kits))
	for _, k := range kits {
		names[k.ID] = k.Name
	}
	out := make([]CollectionView, 0, len(rows))
	for _, r := range rows {
		out = append(out, CollectionView{Collection: r, KitName: names[r.KitID]})
	}
	return out, total, nil
}

func (s *KitService) withCounts(eventID uuid.UUID, k kitModel.Kit) (*KitWithCounts, error) {
	counts, err := s.repo.CountsByKit(eventID)
	if err != nil {
		return nil, err
	}
	m := counts[k.ID]
	pending := m[string(kitModel.CollectionStatusPending)]
	collected := m[string(kitModel.CollectionStatusCollected)]
	voided := m[string(kitModel.CollectionStatusVoided)]
	remaining := int64(k.QuantityTotal) - pending - collected
	if remaining < 0 {
		remaining = 0
	}
	return &KitWithCounts{Kit: k, Pending: pending, Collected: collected, Voided: voided, Remaining: remaining}, nil
}

// isConflictErr recognizes unique-violation errors on both engines
// (Postgres "duplicate key", SQLite "UNIQUE constraint failed").
func isConflictErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "UNIQUE constraint failed")
}
