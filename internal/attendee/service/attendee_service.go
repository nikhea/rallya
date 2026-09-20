package service

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	attendeeDTO "github.com/nikhea/rallya/internal/attendee/dto"
	"github.com/nikhea/rallya/internal/attendee/model"
	"github.com/nikhea/rallya/internal/attendee/repository"
	attendeeutils "github.com/nikhea/rallya/internal/attendee/utils"
	auth "github.com/nikhea/rallya/internal/auth"
	ordersevice "github.com/nikhea/rallya/internal/order/service"
)

// EventResolver resolves event UUIDs and org ownership (implemented by events).
type EventResolver interface {
	ResolveEventID(orgID uuid.UUID, ref string) (uuid.UUID, error)
	OrgOf(eventID uuid.UUID) (uuid.UUID, error)
}

// OrgAccess answers management rights (implemented by org service).
type OrgAccess interface {
	CanManage(userID, orgID uuid.UUID) bool
}

// AttendeeService orchestrates attendee flows.
type AttendeeService struct {
	repo   *repository.AttendeeRepository
	users  auth.UserReader
	events EventResolver
	orgs   OrgAccess
}

// NewAttendeeService builds the service. All collaborators required.
func NewAttendeeService(repo *repository.AttendeeRepository, users auth.UserReader, events EventResolver, orgs OrgAccess) *AttendeeService {
	return &AttendeeService{repo: repo, users: users, events: events, orgs: orgs}
}

// MintForOrder mints one row per unit, skipping existing (orderID, unit)
// pairs so webhook redelivery converges instead of duplicating. Returns raw
// tokens alongside IDs for email embedding (never persisted). Implements the
// order domain's AttendeeMinter contract.
func (s *AttendeeService) MintForOrder(tx *gorm.DB, orderID, userID, eventID uuid.UUID, email, name string, quantity int) ([]ordersevice.MintedAttendee, error) {
	out := make([]ordersevice.MintedAttendee, 0, quantity)
	for unit := 0; unit < quantity; unit++ {
		exists, err := s.repo.ExistsForOrder(tx, orderID, unit)
		if err != nil {
			return nil, err
		}
		if exists {
			continue
		}
		raw, err := attendeeutils.GenerateToken()
		if err != nil {
			return nil, err
		}
		a := &model.Attendee{
			OrderID: &orderID, UnitIndex: unit, EventID: eventID,
			UserID: &userID, Email: email, TokenHash: attendeeutils.HashToken(raw),
			Status: model.AttendeeStatusRegistered,
		}
		if strings.TrimSpace(name) != "" {
			a.Name = &name
		}
		if err := s.repo.CreateAttendee(tx, a); err != nil {
			return nil, err
		}
		out = append(out, ordersevice.MintedAttendee{ID: a.ID, QRToken: raw})
	}
	return out, nil
}

// CancelForOrder flips an order's rows to CANCELLED (audit trail kept).
func (s *AttendeeService) CancelForOrder(tx *gorm.DB, orderID uuid.UUID) error {
	return s.repo.CancelForOrder(tx, orderID)
}

// ResolveEvent resolves an event ref within an org (route handlers use the
// org ID already in context from RequireOrgContext).
func (s *AttendeeService) ResolveEvent(orgID uuid.UUID, ref string) (uuid.UUID, error) {
	return s.events.ResolveEventID(orgID, ref)
}

// AddManual creates a walk-in/comp/staff row (ADMIN+ upstream). The email
// need not belong to an account (userID stays nil); name is optional.
func (s *AttendeeService) AddManual(orgID uuid.UUID, eventRef, email string, name *string) (*attendeeDTO.Attendee, error) {
	eventID, err := s.events.ResolveEventID(orgID, eventRef)
	if err != nil {
		return nil, ErrAttendeeNotFound
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, ErrInvalidEmail
	}
	raw, err := attendeeutils.GenerateToken()
	if err != nil {
		return nil, err
	}
	a := &model.Attendee{
		EventID: eventID, Email: email, Name: name,
		TokenHash: attendeeutils.HashToken(raw),
		Status:    model.AttendeeStatusRegistered,
	}
	if err := s.repo.CreateAttendee(nil, a); err != nil {
		return nil, err
	}
	return toAttendee(a, nil), nil
}

// GetAttendee returns a row to its owner (by user or email match not needed:
// ownership is user-bound; ADMIN+ via admin path).
func (s *AttendeeService) GetAttendee(userID, attendeeID uuid.UUID) (*attendeeDTO.Attendee, error) {
	a, err := s.repo.GetAttendeeByID(attendeeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttendeeNotFound
		}
		return nil, err
	}
	if a.UserID == nil || *a.UserID != userID {
		return nil, ErrAttendeeNotFound
	}
	return toAttendee(a, nil), nil
}

// GetAttendeeAdmin returns a row for org admins.
func (s *AttendeeService) GetAttendeeAdmin(userID, attendeeID uuid.UUID) (*attendeeDTO.Attendee, error) {
	a, err := s.repo.GetAttendeeByID(attendeeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttendeeNotFound
		}
		return nil, err
	}
	orgID, err := s.events.OrgOf(a.EventID)
	if err != nil {
		return nil, ErrAttendeeNotFound
	}
	if !s.orgs.CanManage(userID, orgID) {
		return nil, ErrAttendeeNotFound
	}
	return toAttendee(a, nil), nil
}

// ListMine returns the caller's rows, newest first.
func (s *AttendeeService) ListMine(userID uuid.UUID, limit, offset int) ([]attendeeDTO.Attendee, int64, error) {
	as, total, err := s.repo.ListByUser(userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return mapAttendees(as), total, nil
}

// ListEventRoster returns an event's roster (ADMIN+ upstream).
func (s *AttendeeService) ListEventRoster(eventID uuid.UUID, query string, limit, offset int) ([]attendeeDTO.Attendee, int64, error) {
	as, total, err := s.repo.ListByEvent(eventID, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return mapAttendees(as), total, nil
}

// CorrectName sets/corrects a row's display name (ADMIN+ upstream).
func (s *AttendeeService) CorrectName(attendeeID uuid.UUID, name *string, email *string) (*attendeeDTO.Attendee, error) {
	a, err := s.repo.GetAttendeeByID(attendeeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttendeeNotFound
		}
		return nil, err
	}
	if name != nil {
		a.Name = name
	}
	if email != nil {
		em := strings.ToLower(strings.TrimSpace(*email))
		if em == "" || !strings.Contains(em, "@") {
			return nil, ErrInvalidEmail
		}
		a.Email = em
	}
	if err := s.repo.UpdateAttendee(nil, a); err != nil {
		return nil, err
	}
	return toAttendee(a, nil), nil
}

func mapAttendees(as []model.Attendee) []attendeeDTO.Attendee {
	out := make([]attendeeDTO.Attendee, 0, len(as))
	for _, a := range as {
		out = append(out, *toAttendee(&a, nil))
	}
	return out
}

func toAttendee(a *model.Attendee, qrPayload *string) *attendeeDTO.Attendee {
	var orderID, userID *string
	if a.OrderID != nil {
		s := a.OrderID.String()
		orderID = &s
	}
	if a.UserID != nil {
		s := a.UserID.String()
		userID = &s
	}
	return &attendeeDTO.Attendee{
		ID: a.ID.String(), OrderID: orderID, UnitIndex: a.UnitIndex,
		EventID: a.EventID.String(), UserID: userID,
		Email: a.Email, Name: a.Name, Status: string(a.Status),
		QRPayload:   qrPayload,
		CheckedInAt: attendeeutils.FormatTimePtr(a.CheckedInAt),
		CreatedAt:   attendeeutils.FormatTime(a.CreatedAt),
	}
}

// CorrectAttendeeInEvent corrects a row scoped to an event (rejects
// cross-event URL juggling with not-found).
func (s *AttendeeService) CorrectAttendeeInEvent(eventID, attendeeID uuid.UUID, name *string, email *string) (*attendeeDTO.Attendee, error) {
	a, err := s.repo.GetAttendeeByID(attendeeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttendeeNotFound
		}
		return nil, err
	}
	if a.EventID != eventID {
		return nil, ErrAttendeeNotFound
	}
	return s.CorrectName(attendeeID, name, email)
}

// CancelMine flips the caller's row to CANCELLED (audit trail kept).
// Already-terminal rows are rejected.
func (s *AttendeeService) CancelMine(userID, attendeeID uuid.UUID) (*attendeeDTO.Attendee, error) {
	a, err := s.repo.GetAttendeeByID(attendeeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttendeeNotFound
		}
		return nil, err
	}
	if a.UserID == nil || *a.UserID != userID {
		return nil, ErrAttendeeNotFound
	}
	if a.Status != model.AttendeeStatusRegistered {
		return nil, ErrInvalidStatus
	}
	a.Status = model.AttendeeStatusCancelled
	if err := s.repo.UpdateAttendee(nil, a); err != nil {
		return nil, err
	}
	return toAttendee(a, nil), nil
}
