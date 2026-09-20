// Package repository owns checkin_logs persistence. Attendee rows are never
// touched here — the flip goes through the attendees domain seam.
package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/checkin/model"
)

// CheckinRepository persists scan attempts.
type CheckinRepository struct {
	db *gorm.DB
}

// NewCheckinRepository builds the repository on the shared handle.
func NewCheckinRepository(db *gorm.DB) *CheckinRepository {
	return &CheckinRepository{db: db}
}

// InsertTx logs one scan attempt inside the caller's tx (nil-safe: falls
// back to the shared handle, though callers should pass their tx).
func (r *CheckinRepository) InsertTx(tx *gorm.DB, log *model.CheckinLog) error {
	if tx == nil {
		tx = r.db
	}
	return tx.Create(log).Error
}

// CountByAttendee returns how many attempts reference an attendee row.
func (r *CheckinRepository) CountByAttendee(attendeeID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.Model(&model.CheckinLog{}).
		Where("attendee_id = ?", attendeeID).
		Count(&n).Error
	return n, err
}
