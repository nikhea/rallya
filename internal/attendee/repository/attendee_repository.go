package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nikhea/rallya/internal/attendee/model"
)

// AttendeeRepository is GORM persistence for the Attendees domain.
type AttendeeRepository struct {
	db *gorm.DB
}

// NewAttendeeRepository wraps db for attendee persistence.
func NewAttendeeRepository(db *gorm.DB) *AttendeeRepository {
	return &AttendeeRepository{db: db}
}

// DB exposes the handle for service-level transactions.
func (r *AttendeeRepository) DB() *gorm.DB { return r.db }

// CreateAttendee inserts a row.
func (r *AttendeeRepository) CreateAttendee(db *gorm.DB, a *model.Attendee) error {
	return dbOr(r, db).Create(a).Error
}

// GetAttendeeByID loads a row.
func (r *AttendeeRepository) GetAttendeeByID(id uuid.UUID) (*model.Attendee, error) {
	var a model.Attendee
	if err := r.db.First(&a, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// ExistsForOrder reports whether a unit row already exists (idempotent mint).
// Takes db because minting reads inside the caller's tx (read-your-write).
func (r *AttendeeRepository) ExistsForOrder(db *gorm.DB, orderID uuid.UUID, unit int) (bool, error) {
	var n int64
	if err := dbOr(r, db).Model(&model.Attendee{}).
		Where("order_id = ? AND unit_index = ?", orderID, unit).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListByEvent returns an event's roster, oldest first.
func (r *AttendeeRepository) ListByEvent(eventID uuid.UUID, query string, limit, offset int) ([]model.Attendee, int64, error) {
	var out []model.Attendee
	var total int64
	q := r.db.Model(&model.Attendee{}).Where("event_id = ?", eventID)
	if query != "" {
		like := "%" + query + "%"
		q = q.Where("LOWER(email) LIKE LOWER(?) OR LOWER(name) LIKE LOWER(?)", like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at ASC").Limit(limit).Offset(offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListByUser returns a user's rows, newest first.
func (r *AttendeeRepository) ListByUser(userID uuid.UUID, limit, offset int) ([]model.Attendee, int64, error) {
	var out []model.Attendee
	var total int64
	q := r.db.Model(&model.Attendee{}).Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// UpdateAttendee saves row changes.
func (r *AttendeeRepository) UpdateAttendee(db *gorm.DB, a *model.Attendee) error {
	return dbOr(r, db).Save(a).Error
}

// CancelForOrder flips an order's rows to CANCELLED (keeps history).
func (r *AttendeeRepository) CancelForOrder(db *gorm.DB, orderID uuid.UUID) error {
	return dbOr(r, db).Model(&model.Attendee{}).
		Where("order_id = ? AND status = ?", orderID, model.AttendeeStatusRegistered).
		Update("status", model.AttendeeStatusCancelled).Error
}

// GetForUpdate loads a row with a write lock: concurrent scans of the same
// QR serialize here so only the first flips to CHECKED_IN (the loser lands
// on ALREADY_CHECKED_IN). Must run inside the caller's tx.
func (r *AttendeeRepository) GetForUpdate(tx *gorm.DB, id uuid.UUID) (*model.Attendee, error) {
	var a model.Attendee
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&a, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// CountByStatus tallies an event's roster per status (door stats).
func (r *AttendeeRepository) CountByStatus(eventID uuid.UUID) (map[string]int64, error) {
	type row struct {
		Status string
		N      int64
	}
	var rows []row
	if err := r.db.Model(&model.Attendee{}).
		Select("status, COUNT(*) AS n").
		Where("event_id = ?", eventID).
		Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, row := range rows {
		out[row.Status] = row.N
	}
	return out, nil
}

func dbOr(r *AttendeeRepository, db *gorm.DB) *gorm.DB {
	if db != nil {
		return db
	}
	return r.db
}
