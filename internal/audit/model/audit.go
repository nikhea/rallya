// Package model holds the Audit domain persistence schema.
// Append-only: rows are never updated or deleted by the app (no purge in
// v1 — the (org_id, created_at) index keeps a future ranged purge cheap).
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Action namespaced as <object>.<verb> (checkin outcomes reuse the wire
// outcome as the verb: checkin.checked_in, checkin.invalid_code, ...).
const (
	ObjectOrg        = "org"
	ObjectMember     = "member"
	ObjectInvite     = "invite"
	ObjectEvent      = "event"
	ObjectTicket     = "ticket"
	ObjectOrder      = "order"
	ObjectAttendee   = "attendee"
	ObjectCheckinLog = "checkin_log"
	ObjectPolicy     = "policy"
)

// AuditEvent is one recorded action. OrgID NULL = platform scope;
// ActorID NULL = system (sweepers, backfills). Before/After carry
// changed-fields-only diffs — never tokens, hashes, or secrets.
type AuditEvent struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	OrgID      *uuid.UUID `gorm:"type:uuid;index" json:"orgId,omitempty"`
	ActorID    *uuid.UUID `gorm:"type:uuid;index" json:"actorId,omitempty"`
	Action     string     `gorm:"size:64;not null" json:"action"`
	ObjectType string     `gorm:"size:32;not null" json:"objectType"`
	ObjectID   *uuid.UUID `gorm:"type:uuid" json:"objectId,omitempty"`
	Before     *string    `gorm:"type:jsonb" json:"before,omitempty"`
	After      *string    `gorm:"type:jsonb" json:"after,omitempty"`
	IP         *string    `gorm:"size:64" json:"-"`
	UserAgent  *string    `gorm:"size:255" json:"-"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func (AuditEvent) TableName() string { return "audit_events" }

func (e *AuditEvent) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
