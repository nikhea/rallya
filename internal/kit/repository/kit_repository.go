// Package repository owns kit persistence. Attendee rows are never
// touched here — eligibility reads go through the attendees domain seam.
package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nikhea/rallya/internal/kit/model"
)

// KitRepository persists kit types and handout records.
type KitRepository struct {
	db *gorm.DB
}

// NewKitRepository builds the repository on the shared handle.
func NewKitRepository(db *gorm.DB) *KitRepository {
	return &KitRepository{db: db}
}

// CreateKit inserts one kit type inside the caller's tx.
func (r *KitRepository) CreateKit(tx *gorm.DB, k *model.Kit) error {
	return tx.Create(k).Error
}

// GetKitByID loads one kit type by ID inside the caller's tx (never the
// shared handle: tx callers on a single-connection pool would deadlock).
func (r *KitRepository) GetKitByID(tx *gorm.DB, id uuid.UUID) (*model.Kit, error) {
	var k model.Kit
	if err := tx.Where("id = ?", id).First(&k).Error; err != nil {
		return nil, err
	}
	return &k, nil
}

// GetKitForUpdate locks one kit row inside the caller's tx (concurrent
// collects serialize; the oversell guard holds).
func (r *KitRepository) GetKitForUpdate(tx *gorm.DB, id uuid.UUID) (*model.Kit, error) {
	var k model.Kit
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&k).Error; err != nil {
		return nil, err
	}
	return &k, nil
}

// ListKitsByEvent returns an event's kit types, oldest first.
func (r *KitRepository) ListKitsByEvent(eventID uuid.UUID) ([]model.Kit, error) {
	var out []model.Kit
	if err := r.db.Where("event_id = ?", eventID).Order("created_at ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateKit persists kit edits inside the caller's tx.
func (r *KitRepository) UpdateKit(tx *gorm.DB, k *model.Kit) error {
	return tx.Save(k).Error
}

// DeleteKit removes one kit type inside the caller's tx (same handle:
// the row is locked FOR UPDATE by then, and a second connection would
// block on it forever).
func (r *KitRepository) DeleteKit(tx *gorm.DB, id uuid.UUID) error {
	return tx.Where("id = ?", id).Delete(&model.Kit{}).Error
}

// CountsByKit tallies non-voided handouts per kit for one event:
// map[kitID]map[status]count.
func (r *KitRepository) CountsByKit(eventID uuid.UUID) (map[uuid.UUID]map[string]int64, error) {
	type row struct {
		KitID  uuid.UUID
		Status string
		N      int64
	}
	var rows []row
	if err := r.db.Model(&model.KitCollection{}).
		Select("kit_id, status, COUNT(*) AS n").
		Where("event_id = ?", eventID).
		Group("kit_id, status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uuid.UUID]map[string]int64{}
	for _, rw := range rows {
		m, ok := out[rw.KitID]
		if !ok {
			m = map[string]int64{}
			out[rw.KitID] = m
		}
		m[rw.Status] = rw.N
	}
	return out, nil
}

// CountActive counts non-voided handouts for one kit (reservation +
// collected units both hold stock).
func (r *KitRepository) CountActive(tx *gorm.DB, kitID uuid.UUID) (int64, error) {
	var n int64
	if err := tx.Model(&model.KitCollection{}).
		Where("kit_id = ? AND status <> ?", kitID, model.CollectionStatusVoided).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// HasActiveCollections reports whether an attendee holds any non-voided
// handout (check-in revert guard).
func (r *KitRepository) HasActiveCollections(tx *gorm.DB, attendeeID uuid.UUID) (bool, error) {
	var n int64
	db := tx
	if db == nil {
		db = r.db
	}
	if err := db.Model(&model.KitCollection{}).
		Where("attendee_id = ? AND status <> ?", attendeeID, model.CollectionStatusVoided).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// FindByIdempotency returns the record minted under an idempotency key
// (tablet retry convergence). Nil, nil when no such row exists.
func (r *KitRepository) FindByIdempotency(tx *gorm.DB, kitID, attendeeID uuid.UUID, key string) (*model.KitCollection, error) {
	var c model.KitCollection
	if err := tx.Where("kit_id = ? AND attendee_id = ? AND idempotency_key = ?", kitID, attendeeID, key).
		First(&c).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// CreateCollection inserts one handout record inside the caller's tx.
func (r *KitRepository) CreateCollection(tx *gorm.DB, c *model.KitCollection) error {
	return tx.Create(c).Error
}

// GetCollectionForUpdate locks one handout row inside the caller's tx.
func (r *KitRepository) GetCollectionForUpdate(tx *gorm.DB, id uuid.UUID) (*model.KitCollection, error) {
	var c model.KitCollection
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateCollection persists handout edits inside the caller's tx.
func (r *KitRepository) UpdateCollection(tx *gorm.DB, c *model.KitCollection) error {
	return tx.Save(c).Error
}

// CollectionFilter scopes handout reads (all fields optional except event).
type CollectionFilter struct {
	KitID      *uuid.UUID
	Status     string
	AttendeeID *uuid.UUID
}

// ListCollections returns an event's handouts, newest first.
func (r *KitRepository) ListCollections(eventID uuid.UUID, f CollectionFilter) ([]model.KitCollection, int64, error) {
	q := r.db.Model(&model.KitCollection{}).Where("event_id = ?", eventID)
	if f.KitID != nil {
		q = q.Where("kit_id = ?", *f.KitID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.AttendeeID != nil {
		q = q.Where("attendee_id = ?", *f.AttendeeID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.KitCollection
	if err := q.Order("created_at DESC").Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
