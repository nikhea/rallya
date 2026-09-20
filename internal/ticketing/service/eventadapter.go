package service

import (
	"github.com/google/uuid"

	eventservice "github.com/nikhea/rallya/internal/event/service"
)

// EventInfo is the minimal event shape ticketing needs (no event imports
// beyond the adapter file; keeps the interface mockable).
type EventInfo struct {
	ID       uuid.UUID
	Status   string
	Capacity *int
}

// EventResolver is implemented by the events domain (adapter below).
// Ticketing never touches event tables directly.
type EventResolver interface {
	GetEvent(callerID *uuid.UUID, orgRef, eventRef string) (EventInfo, error)
	GetPublicEvent(eventID uuid.UUID) (EventInfo, error)
}

// Published reports a published event.
func (e EventInfo) Published() bool { return e.Status == "PUBLISHED" }

// EventAdapter implements EventResolver over the event service.
type EventAdapter struct {
	Svc *eventservice.EventService
}

// NewEventAdapter wraps the event service for ticketing wiring.
func NewEventAdapter(svc *eventservice.EventService) *EventAdapter {
	return &EventAdapter{Svc: svc}
}

// GetEvent resolves a member-visible event (drafts included).
func (a *EventAdapter) GetEvent(callerID *uuid.UUID, orgRef, eventRef string) (EventInfo, error) {
	e, err := a.Svc.GetEvent(callerID, orgRef, eventRef)
	if err != nil {
		return EventInfo{}, err
	}
	return EventInfo{
		ID: uuid.MustParse(e.ID), Status: e.Status, Capacity: e.Capacity,
	}, nil
}

// GetPublicEvent resolves a published event by ID.
func (a *EventAdapter) GetPublicEvent(eventID uuid.UUID) (EventInfo, error) {
	e, err := a.Svc.GetPublicEvent(eventID)
	if err != nil {
		return EventInfo{}, err
	}
	return EventInfo{
		ID: eventID, Status: e.Status, Capacity: e.Capacity,
	}, nil
}
