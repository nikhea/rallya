package repository

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/event/model"
)

// EventFilter scopes listings.
type EventFilter struct {
	OrganizationID *uuid.UUID
	Status         *model.EventStatus
	From           *time.Time
	To             *time.Time
	Query          string
	// IncludeDrafts gates DRAFT visibility (members only, enforced upstream).
	IncludeDrafts bool
}

// EventRepository is GORM persistence for the Events domain.
type EventRepository struct {
	db *gorm.DB
}

// NewEventRepository wraps db for event persistence.
func NewEventRepository(db *gorm.DB) *EventRepository {
	return &EventRepository{db: db}
}

// DB exposes the handle for service-level transactions.
func (r *EventRepository) DB() *gorm.DB { return r.db }

// CreateEvent inserts an event.
func (r *EventRepository) CreateEvent(db *gorm.DB, e *model.Event) error {
	return dbOr(r, db).Create(e).Error
}

// GetEventByID loads an event by UUID.
func (r *EventRepository) GetEventByID(id uuid.UUID) (*model.Event, error) {
	var e model.Event
	if err := r.db.First(&e, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &e, nil
}

// GetEventBySlug loads an event by org + slug.
func (r *EventRepository) GetEventBySlug(orgID uuid.UUID, slug string) (*model.Event, error) {
	var e model.Event
	if err := r.db.First(&e, "organization_id = ? AND slug = ?", orgID, slug).Error; err != nil {
		return nil, err
	}
	return &e, nil
}

// UpdateEvent saves event changes.
func (r *EventRepository) UpdateEvent(db *gorm.DB, e *model.Event) error {
	return dbOr(r, db).Save(e).Error
}

// DeleteEvent hard-deletes an event.
func (r *EventRepository) DeleteEvent(db *gorm.DB, id uuid.UUID) error {
	return dbOr(r, db).Delete(&model.Event{}, "id = ?", id).Error
}

// CreateImage inserts a gallery row.
func (r *EventRepository) CreateImage(db *gorm.DB, img *model.EventImage) error {
	return dbOr(r, db).Create(img).Error
}

// ListImagesByEvent returns an event's gallery, oldest first.
func (r *EventRepository) ListImagesByEvent(eventID uuid.UUID) ([]model.EventImage, error) {
	var out []model.EventImage
	if err := r.db.Order("created_at ASC").Find(&out, "event_id = ?", eventID).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ListEvents returns paginated events honoring the filter.
func (r *EventRepository) ListEvents(f EventFilter, limit, offset int, sort string) ([]model.Event, int64, error) {
	var out []model.Event
	var total int64
	q := r.db.Model(&model.Event{})
	if f.OrganizationID != nil {
		q = q.Where("organization_id = ?", *f.OrganizationID)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	} else if !f.IncludeDrafts {
		q = q.Where("status = ?", model.EventStatusPublished)
	}
	if f.From != nil {
		q = q.Where("starts_at IS NULL OR starts_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("starts_at IS NULL OR starts_at <= ?", *f.To)
	}
	if s := strings.TrimSpace(f.Query); s != "" {
		// LOWER() LIKE: case-insensitive on Postgres and SQLite
		// (ILIKE is Postgres-only).
		like := "%" + strings.ToLower(s) + "%"
		q = q.Where("LOWER(title) LIKE ? OR LOWER(description) LIKE ?", like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if sort == "created" {
		q = q.Order("created_at DESC")
	} else {
		q = q.Order("starts_at ASC NULLS LAST, created_at DESC")
	}
	if err := q.Limit(limit).Offset(offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func dbOr(r *EventRepository, db *gorm.DB) *gorm.DB {
	if db != nil {
		return db
	}
	return r.db
}
