// Package model holds the Events domain persistence schema.
// Events are org-scoped (tenant isolation); slugs unique per org.
// Ticketing (types, pricing, capacity enforcement) lands later.
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EventStatus is the publish lifecycle.
type EventStatus string

const (
	EventStatusDraft     EventStatus = "DRAFT"
	EventStatusPublished EventStatus = "PUBLISHED"
	EventStatusCancelled EventStatus = "CANCELLED"
)

// Valid reports whether s is a known status.
func (s EventStatus) Valid() bool {
	return s == EventStatusDraft || s == EventStatusPublished || s == EventStatusCancelled
}

// Event is an org-scoped event.
type Event struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	OrganizationID uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:idx_events_org_slug" json:"organizationId"`

	Title string `gorm:"size:255;not null" json:"title"`
	Slug  string `gorm:"size:150;not null;uniqueIndex:idx_events_org_slug" json:"slug"`

	Description *string `gorm:"type:text" json:"description,omitempty"`

	Venue    *string `gorm:"size:255" json:"venue,omitempty"`
	Location *string `gorm:"size:255" json:"location,omitempty"`

	StartsAt *time.Time `json:"startsAt,omitempty"`
	EndsAt   *time.Time `json:"endsAt,omitempty"`

	// Capacity NULL = unlimited; unenforced until ticketing.
	Capacity *int `json:"capacity,omitempty"`

	CoverURL *string `gorm:"type:text" json:"coverUrl,omitempty"`

	Status EventStatus `gorm:"size:20;not null;default:DRAFT" json:"status"`

	CreatedBy *uuid.UUID `gorm:"type:uuid" json:"createdBy,omitempty"`

	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Event) TableName() string { return "events" }

func (e *Event) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

// Published reports public visibility.
func (e *Event) Published() bool { return e.Status == EventStatusPublished }
