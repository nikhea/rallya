package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nikhea/rallya/internal/ticketing/model"
)

// TicketRepository is GORM persistence for the Ticketing domain.
type TicketRepository struct {
	db *gorm.DB
}

// NewTicketRepository wraps db for ticket persistence.
func NewTicketRepository(db *gorm.DB) *TicketRepository {
	return &TicketRepository{db: db}
}

// DB exposes the handle for service-level transactions.
func (r *TicketRepository) DB() *gorm.DB { return r.db }

// CreateType inserts a ticket type.
func (r *TicketRepository) CreateType(db *gorm.DB, t *model.TicketType) error {
	return dbOr(r, db).Create(t).Error
}

// GetTypeByID loads a ticket type.
func (r *TicketRepository) GetTypeByID(id uuid.UUID) (*model.TicketType, error) {
	var t model.TicketType
	if err := r.db.First(&t, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTypesByEvent returns an event's types, oldest first.
func (r *TicketRepository) ListTypesByEvent(eventID uuid.UUID, includeDrafts bool) ([]model.TicketType, error) {
	var out []model.TicketType
	q := r.db.Where("event_id = ?", eventID)
	if !includeDrafts {
		q = q.Where("status = ?", model.TicketStatusActive)
	}
	if err := q.Order("created_at ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateType saves type changes.
func (r *TicketRepository) UpdateType(db *gorm.DB, t *model.TicketType) error {
	return dbOr(r, db).Save(t).Error
}

// DeleteType hard-deletes a type.
func (r *TicketRepository) DeleteType(db *gorm.DB, id uuid.UUID) error {
	return dbOr(r, db).Delete(&model.TicketType{}, "id = ?", id).Error
}

// LockType loads a type with a row-level write lock (must run inside a tx).
// On SQLite the clause is a no-op; on Postgres it serializes Reserve calls
// so quantity_sold can never overshoot.
func (r *TicketRepository) LockType(db *gorm.DB, id uuid.UUID) (*model.TicketType, error) {
	var t model.TicketType
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// AddSold atomically bumps quantity_sold inside the caller's tx.
func (r *TicketRepository) AddSold(db *gorm.DB, id uuid.UUID, n int) error {
	return dbOr(r, db).Model(&model.TicketType{}).
		Where("id = ?", id).
		Update("quantity_sold", gorm.Expr("quantity_sold + ?", n)).Error
}

// AllocatedTotal sums quantity_total across an event's types (capacity check).
func (r *TicketRepository) AllocatedTotal(eventID uuid.UUID, excludeID *uuid.UUID) (int64, error) {
	var total int64
	q := r.db.Model(&model.TicketType{}).Where("event_id = ?", eventID)
	if excludeID != nil {
		q = q.Where("id <> ?", *excludeID)
	}
	// COALESCE keeps SUM non-null on empty sets.
	if err := q.Select("COALESCE(SUM(quantity_total), 0)").Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func dbOr(r *TicketRepository, db *gorm.DB) *gorm.DB {
	if db != nil {
		return db
	}
	return r.db
}
