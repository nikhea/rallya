package service

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/audit/model"
	"github.com/nikhea/rallya/internal/audit/repository"
)

// Entry is one auditable action, built by the emitting domain. OrgID nil =
// platform scope; ActorID nil = system. Before/After are changed-fields
// diffs only — emitting domains must never put tokens or secrets here.
type Entry struct {
	OrgID      *uuid.UUID
	ActorID    *uuid.UUID
	Action     string
	ObjectType string
	ObjectID   *uuid.UUID
	Before     map[string]any
	After      map[string]any
	IP         *string
	UserAgent  *string
}

// Emitter is the consumer-declared seam every audited domain implements
// against (domains declare `audit.Emitter`, wiring injects *AuditService).
// EmitTx inserts inside the caller's tx: the entry commits atomically with
// the action, and emit failure fails the action (loud beats gappy).
type Emitter interface {
	EmitTx(tx *gorm.DB, e Entry) error
}

// AuditService records and reads audit events.
type AuditService struct {
	repo *repository.AuditRepository
}

// NewAuditService builds the service.
func NewAuditService(repo *repository.AuditRepository) *AuditService {
	return &AuditService{repo: repo}
}

// EmitTx implements Emitter.
func (s *AuditService) EmitTx(tx *gorm.DB, e Entry) error {
	before, err := marshalDiff(e.Before)
	if err != nil {
		return err
	}
	after, err := marshalDiff(e.After)
	if err != nil {
		return err
	}
	return s.repo.InsertTx(tx, &model.AuditEvent{
		OrgID: e.OrgID, ActorID: e.ActorID, Action: e.Action,
		ObjectType: e.ObjectType, ObjectID: e.ObjectID,
		Before: before, After: after, IP: e.IP, UserAgent: e.UserAgent,
	})
}

func marshalDiff(m map[string]any) (*string, error) {
	if len(m) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	s := string(raw)
	return &s, nil
}

// ListFilter mirrors repository.Filter with service-level pagination guards.
type ListFilter struct {
	OrgID      *uuid.UUID
	Action     string
	ActorID    *uuid.UUID
	ObjectType string
	ObjectID   *uuid.UUID
	Since      *time.Time
	Until      *time.Time
	Page       int
	PerPage    int
}

// ListResult is a paginated read (service stays wire-shape free).
type ListResult struct {
	Items []model.AuditEvent
	Total int64
}

// List reads events newest-first (org-scoped when OrgID set; platform-wide
// when nil — superadmin path only).
func (s *AuditService) List(f ListFilter) (*ListResult, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 || f.PerPage > 100 {
		f.PerPage = 20
	}
	items, total, err := s.repo.List(repository.Filter{
		OrgID: f.OrgID, Action: f.Action, ActorID: f.ActorID,
		ObjectType: f.ObjectType, ObjectID: f.ObjectID,
		Since: f.Since, Until: f.Until,
		Limit: f.PerPage, Offset: (f.Page - 1) * f.PerPage,
	})
	if err != nil {
		return nil, err
	}
	return &ListResult{Items: items, Total: total}, nil
}
