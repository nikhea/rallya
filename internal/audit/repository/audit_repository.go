// Package repository owns audit_events persistence: append-only inserts
// plus the filtered reads behind both audit endpoints.
package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/audit/model"
)

// AuditRepository persists and queries audit events.
type AuditRepository struct {
	db *gorm.DB
}

// NewAuditRepository builds the repository on the shared handle.
func NewAuditRepository(db *gorm.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// DB exposes the handle for service-level transactions.
func (r *AuditRepository) DB() *gorm.DB { return r.db }

// InsertTx records one event inside the caller's tx (nil-safe fallback to
// the shared handle, though emits should pass their tx for atomicity).
func (r *AuditRepository) InsertTx(tx *gorm.DB, e *model.AuditEvent) error {
	if tx == nil {
		tx = r.db
	}
	return tx.Create(e).Error
}

// Filter scopes list queries. Zero values mean unfiltered.
type Filter struct {
	OrgID      *uuid.UUID
	Action     string
	ActorID    *uuid.UUID
	ObjectType string
	ObjectID   *uuid.UUID
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
}

// List returns events newest-first with the total count.
func (r *AuditRepository) List(f Filter) ([]model.AuditEvent, int64, error) {
	var out []model.AuditEvent
	var total int64
	q := r.db.Model(&model.AuditEvent{})
	if f.OrgID != nil {
		q = q.Where("org_id = ?", *f.OrgID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.ActorID != nil {
		q = q.Where("actor_id = ?", *f.ActorID)
	}
	if f.ObjectType != "" {
		q = q.Where("object_type = ?", f.ObjectType)
	}
	if f.ObjectID != nil {
		q = q.Where("object_id = ?", *f.ObjectID)
	}
	if f.Since != nil {
		q = q.Where("created_at >= ?", *f.Since)
	}
	if f.Until != nil {
		q = q.Where("created_at <= ?", *f.Until)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at DESC").Limit(f.Limit).Offset(f.Offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
