package service

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	ticketdto "github.com/nikhea/rallya/internal/ticketing/dto"
	"github.com/nikhea/rallya/internal/ticketing/model"
	"github.com/nikhea/rallya/internal/ticketing/repository"
	"github.com/nikhea/rallya/internal/ticketing/utils"
)

// TicketService orchestrates ticket-type flows.
type TicketService struct {
	repo    *repository.TicketRepository
	events  EventResolver
	auditor auditsvc.Emitter
}

// NewTicketService builds the service. events is required (adapter);
// audit emission wires via setter and is nil-safe when absent.
func NewTicketService(repo *repository.TicketRepository, events EventResolver) *TicketService {
	return &TicketService{repo: repo, events: events}
}

// SetAuditEmitter wires audit-trail emission (nil-safe when absent).
func (s *TicketService) SetAuditEmitter(a auditsvc.Emitter) { s.auditor = a }

// emit records one audit entry (nil-safe when unwired). Call inside the
// action's tx so entry and mutation commit atomically.
func (s *TicketService) emit(tx *gorm.DB, e auditsvc.Entry) error {
	if s.auditor == nil {
		return nil
	}
	return s.auditor.EmitTx(tx, e)
}

// orgOf resolves the mutation's org for audit scope.
func (s *TicketService) orgOf(eventID uuid.UUID) (uuid.UUID, error) {
	return s.events.OrgOf(eventID)
}

// CreateInput carries validated create fields.
type CreateInput struct {
	Name          string
	Description   *string
	PriceCents    int
	Currency      string
	QuantityTotal int
	MaxPerOrder   *int
	SaleStartsAt  *time.Time
	SaleEndsAt    *time.Time
}

// CreateType creates a DRAFT ticket type (ADMIN+ upstream). Validates event
// capacity coupling in both... (ticket allocation vs event headroom).
func (s *TicketService) CreateType(callerID *uuid.UUID, orgRef, eventRef string, in CreateInput) (*ticketdto.TicketType, error) {
	ev, err := s.events.GetEvent(callerID, orgRef, eventRef)
	if err != nil {
		return nil, ErrEventUnresolved
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, ErrInvalidName
	}
	if in.PriceCents < 0 {
		return nil, ErrInvalidPrice
	}
	if in.QuantityTotal <= 0 {
		return nil, ErrInvalidQty
	}
	if err := checkWindow(in.SaleStartsAt, in.SaleEndsAt); err != nil {
		return nil, err
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "USD"
	}
	if in.MaxPerOrder != nil && *in.MaxPerOrder <= 0 {
		return nil, ErrInvalidQty
	}
	if err := s.checkEventHeadroom(ev, in.QuantityTotal, nil); err != nil {
		return nil, err
	}
	t := &model.TicketType{
		EventID: ev.ID, Name: name, Description: in.Description,
		PriceCents: in.PriceCents, Currency: currency,
		QuantityTotal: in.QuantityTotal, MaxPerOrder: in.MaxPerOrder,
		SaleStartsAt: in.SaleStartsAt, SaleEndsAt: in.SaleEndsAt,
		Status: model.TicketStatusDraft,
	}
	orgID, err := s.orgOf(ev.ID)
	if err != nil {
		return nil, err
	}
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateType(tx, t); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: callerID,
			Action: "ticket.created", ObjectType: auditmodel.ObjectTicket, ObjectID: &t.ID,
			After: map[string]any{"name": name, "priceCents": in.PriceCents, "quantityTotal": in.QuantityTotal},
		})
	})
	if err != nil {
		return nil, err
	}
	return toTicketType(t, ev.Published(), time.Now()), nil
}

// ListTypes returns an event's types (member view, drafts included).
func (s *TicketService) ListTypes(callerID *uuid.UUID, orgRef, eventRef string) ([]ticketdto.TicketType, error) {
	ev, err := s.events.GetEvent(callerID, orgRef, eventRef)
	if err != nil {
		return nil, ErrEventUnresolved
	}
	ts, err := s.repo.ListTypesByEvent(ev.ID, true)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]ticketdto.TicketType, 0, len(ts))
	for _, t := range ts {
		out = append(out, *toTicketType(&t, ev.Published(), now))
	}
	return out, nil
}

// ListPublicTypes returns active types with live availability (published only).
func (s *TicketService) ListPublicTypes(eventID uuid.UUID) ([]ticketdto.TicketType, error) {
	if _, err := s.events.GetPublicEvent(eventID); err != nil {
		return nil, ErrEventUnresolved
	}
	ts, err := s.repo.ListTypesByEvent(eventID, false)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]ticketdto.TicketType, 0, len(ts))
	for _, t := range ts {
		out = append(out, *toTicketType(&t, true, now))
	}
	return out, nil
}

// GetType returns one type (member view).
func (s *TicketService) GetType(callerID *uuid.UUID, orgRef, eventRef, ref string) (*ticketdto.TicketType, error) {
	ev, err := s.events.GetEvent(callerID, orgRef, eventRef)
	if err != nil {
		return nil, ErrEventUnresolved
	}
	t, err := s.resolveType(ev.ID, ref)
	if err != nil {
		return nil, err
	}
	return toTicketType(t, ev.Published(), time.Now()), nil
}

// UpdateInput carries optional update fields (nil = unchanged).
type UpdateInput struct {
	Name          *string
	Description   *string
	PriceCents    *int
	Currency      *string
	QuantityTotal *int
	MaxPerOrder   *int
	SaleStartsAt  *time.Time
	SaleEndsAt    *time.Time
}

// UpdateType edits a type (ADMIN+ upstream). Quantity cuts below sold and
// event-capacity violations are rejected.
func (s *TicketService) UpdateType(callerID *uuid.UUID, orgRef, eventRef, ref string, in UpdateInput) (*ticketdto.TicketType, error) {
	ev, err := s.events.GetEvent(callerID, orgRef, eventRef)
	if err != nil {
		return nil, ErrEventUnresolved
	}
	t, err := s.resolveType(ev.ID, ref)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return nil, ErrInvalidName
		}
		t.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		t.Description = in.Description
	}
	if in.PriceCents != nil {
		if *in.PriceCents < 0 {
			return nil, ErrInvalidPrice
		}
		t.PriceCents = *in.PriceCents
	}
	if in.Currency != nil {
		c := strings.ToUpper(strings.TrimSpace(*in.Currency))
		t.Currency = c
	}
	if in.QuantityTotal != nil {
		if *in.QuantityTotal <= 0 {
			return nil, ErrInvalidQty
		}
		if *in.QuantityTotal < t.QuantitySold {
			return nil, ErrHasSales
		}
		t.QuantityTotal = *in.QuantityTotal
	}
	if in.MaxPerOrder != nil {
		if *in.MaxPerOrder <= 0 {
			return nil, ErrInvalidQty
		}
		t.MaxPerOrder = in.MaxPerOrder
	}
	if in.SaleStartsAt != nil {
		t.SaleStartsAt = in.SaleStartsAt
	}
	if in.SaleEndsAt != nil {
		t.SaleEndsAt = in.SaleEndsAt
	}
	if err := checkWindow(t.SaleStartsAt, t.SaleEndsAt); err != nil {
		return nil, err
	}
	if err := s.checkEventHeadroom(ev, t.QuantityTotal, &t.ID); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateType(nil, t); err != nil {
		return nil, err
	}
	return toTicketType(t, ev.Published(), time.Now()), nil
}

// DeleteType hard-deletes a type with zero sales (ADMIN+ upstream).
func (s *TicketService) DeleteType(callerID *uuid.UUID, orgRef, eventRef, ref string) error {
	ev, err := s.events.GetEvent(callerID, orgRef, eventRef)
	if err != nil {
		return ErrEventUnresolved
	}
	t, err := s.resolveType(ev.ID, ref)
	if err != nil {
		return err
	}
	if t.QuantitySold > 0 {
		return ErrHasSales
	}
	orgID, err := s.orgOf(ev.ID)
	if err != nil {
		return err
	}
	name := t.Name
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.DeleteType(tx, t.ID); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: callerID,
			Action: "ticket.deleted", ObjectType: auditmodel.ObjectTicket, ObjectID: &t.ID,
			Before: map[string]any{"name": name},
		})
	})
}

// Activate transitions DRAFT/PAUSED -> ACTIVE.
func (s *TicketService) Activate(callerID *uuid.UUID, orgRef, eventRef, ref string) (*ticketdto.TicketType, error) {
	return s.setStatus(callerID, orgRef, eventRef, ref, model.TicketStatusActive,
		model.TicketStatusDraft, model.TicketStatusPaused)
}

// Pause transitions ACTIVE -> PAUSED.
func (s *TicketService) Pause(callerID *uuid.UUID, orgRef, eventRef, ref string) (*ticketdto.TicketType, error) {
	return s.setStatus(callerID, orgRef, eventRef, ref, model.TicketStatusPaused,
		model.TicketStatusActive)
}

func (s *TicketService) setStatus(callerID *uuid.UUID, orgRef, eventRef, ref string, to model.TicketStatus, from ...model.TicketStatus) (*ticketdto.TicketType, error) {
	ev, err := s.events.GetEvent(callerID, orgRef, eventRef)
	if err != nil {
		return nil, ErrEventUnresolved
	}
	t, err := s.resolveType(ev.ID, ref)
	if err != nil {
		return nil, err
	}
	ok := false
	for _, f := range from {
		if t.Status == f {
			ok = true
		}
	}
	if !ok {
		return nil, ErrInvalidStatus
	}
	before := string(t.Status)
	t.Status = to
	orgID, err := s.orgOf(ev.ID)
	if err != nil {
		return nil, err
	}
	action := "ticket.activated"
	if to == model.TicketStatusPaused {
		action = "ticket.paused"
	}
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpdateType(tx, t); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: callerID,
			Action: action, ObjectType: auditmodel.ObjectTicket, ObjectID: &t.ID,
			Before: map[string]any{"status": before},
			After:  map[string]any{"status": string(to)},
		})
	})
	if err != nil {
		return nil, err
	}
	return toTicketType(t, ev.Published(), time.Now()), nil
}

// Reserve atomically claims n units for the future orders flow.
// Row-locked: quantity_sold can never overshoot under concurrency.
func (s *TicketService) Reserve(typeID uuid.UUID, n int) error {
	if n <= 0 {
		return ErrInvalidQty
	}
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		return s.ReserveTx(tx, typeID, n)
	})
}

// ReserveTx is Reserve joined to the caller's tx (orders create the row
// atomically with the hold — no phantom inventory on failure).
func (s *TicketService) ReserveTx(tx *gorm.DB, typeID uuid.UUID, n int) error {
	if n <= 0 {
		return ErrInvalidQty
	}
	t, err := s.repo.LockType(tx, typeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTicketNotFound
		}
		return err
	}
	if t.Status != model.TicketStatusActive {
		return ErrNotOnSale
	}
	if t.MaxPerOrder != nil && n > *t.MaxPerOrder {
		return ErrTooMany
	}
	if t.QuantitySold+n > t.QuantityTotal {
		return ErrSoldOut
	}
	return s.repo.AddSold(tx, typeID, n)
}

// Release returns n units (refunds/cancellations later).
func (s *TicketService) Release(typeID uuid.UUID, n int) error {
	if n <= 0 {
		return ErrInvalidQty
	}
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		return s.ReleaseTx(tx, typeID, n)
	})
}

// ReleaseTx is Release joined to the caller's tx.
func (s *TicketService) ReleaseTx(tx *gorm.DB, typeID uuid.UUID, n int) error {
	if n <= 0 {
		return ErrInvalidQty
	}
	t, err := s.repo.LockType(tx, typeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTicketNotFound
		}
		return err
	}
	if t.QuantitySold-n < 0 {
		return ErrInvalidQty
	}
	return s.repo.AddSold(tx, typeID, -n)
}

// ---------- availability ----------

// Availability evaluates live sale state (single code path for public
// reads and future orders).
func Availability(t *model.TicketType, eventPublished bool, now time.Time) (forSale bool, remaining int, reason string) {
	remaining = t.Remaining()
	switch {
	case !eventPublished:
		return false, remaining, "event_unpublished"
	case t.Status != model.TicketStatusActive:
		return false, remaining, "not_active"
	case t.SaleStartsAt != nil && now.Before(*t.SaleStartsAt):
		return false, remaining, "not_started"
	case t.SaleEndsAt != nil && !now.Before(*t.SaleEndsAt):
		return false, remaining, "ended"
	case remaining <= 0:
		return false, 0, "sold_out"
	default:
		return true, remaining, ""
	}
}

// ---------- helpers ----------

func (s *TicketService) resolveType(eventID uuid.UUID, ref string) (*model.TicketType, error) {
	id, err := uuid.Parse(strings.TrimSpace(ref))
	if err != nil {
		return nil, ErrTicketNotFound
	}
	t, err := s.repo.GetTypeByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}
	if t.EventID != eventID {
		return nil, ErrTicketNotFound
	}
	return t, nil
}

// checkEventHeadroom enforces sum(type quantities) <= event capacity
// (both directions: create new and grow existing, excluding self).
func (s *TicketService) checkEventHeadroom(ev EventInfo, qty int, excludeID *uuid.UUID) error {
	if ev.Capacity == nil {
		return nil
	}
	allocated, err := s.repo.AllocatedTotal(ev.ID, excludeID)
	if err != nil {
		return err
	}
	if allocated+int64(qty) > int64(*ev.Capacity) {
		return ErrEventCapacity
	}
	return nil
}

func checkWindow(starts, ends *time.Time) error {
	if starts != nil && ends != nil && !ends.UTC().After(starts.UTC()) {
		return ErrInvalidWindow
	}
	return nil
}

func toTicketType(t *model.TicketType, published bool, now time.Time) *ticketdto.TicketType {
	forSale, remaining, reason := Availability(t, published, now)
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	return &ticketdto.TicketType{
		ID: t.ID.String(), EventID: t.EventID.String(), Name: t.Name, Description: t.Description,
		PriceCents: t.PriceCents, Currency: t.Currency,
		QuantityTotal: t.QuantityTotal, QuantitySold: t.QuantitySold,
		Remaining: remaining, ForSale: forSale, UnavailableReason: reasonPtr, MaxPerOrder: t.MaxPerOrder,
		SaleStartsAt: utils.FormatTimePtr(t.SaleStartsAt),
		SaleEndsAt:   utils.FormatTimePtr(t.SaleEndsAt),
		Status:       string(t.Status), SoldOut: t.SoldOut(),
		CreatedAt: utils.FormatTime(t.CreatedAt),
	}
}

// InspectType returns a type with live published context for internal
// consumers (orders). No guards — callers enforce their own rules.
func (s *TicketService) InspectType(typeID uuid.UUID) (*ticketdto.TicketType, bool, error) {
	t, err := s.repo.GetTypeByID(typeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, ErrTicketNotFound
		}
		return nil, false, err
	}
	published := false
	if _, err := s.events.GetPublicEvent(t.EventID); err == nil {
		published = true
	}
	return toTicketType(t, published, time.Now()), published, nil
}
