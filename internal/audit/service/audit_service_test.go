package service_test

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	"github.com/nikhea/rallya/internal/audit/repository"
	"github.com/nikhea/rallya/internal/audit/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
)

func newAuditSvc(t *testing.T) (*service.AuditService, *gorm.DB) {
	t.Helper()
	db, _, _ := testutil.Setup(t)
	if err := db.AutoMigrate(auditmodel.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return service.NewAuditService(repository.NewAuditRepository(db)), db
}

func TestEmitAndListFilters(t *testing.T) {
	svc, db := newAuditSvc(t)
	org := uuid.New()
	actor := uuid.New()
	other := uuid.New()
	obj := uuid.New()

	emit := func(orgID *uuid.UUID, actorID *uuid.UUID, action string) {
		t.Helper()
		if err := svc.EmitTx(db, service.Entry{
			OrgID: orgID, ActorID: actorID, Action: action,
			ObjectType: auditmodel.ObjectOrder, ObjectID: &obj,
			After: map[string]any{"status": "CONFIRMED"},
		}); err != nil {
			t.Fatalf("emit %s: %v", action, err)
		}
	}
	emit(&org, &actor, "order.created")
	emit(&org, &actor, "order.confirmed")
	emit(&other, &actor, "order.created")
	emit(nil, nil, "policy.seeded")

	// Unfiltered platform read sees everything.
	all, err := svc.List(service.ListFilter{})
	if err != nil || all.Total != 4 {
		t.Fatalf("want 4 total, got %d, %v", all.Total, err)
	}
	// Newest first.
	if all.Items[0].Action != "policy.seeded" {
		t.Fatalf("want newest first, got %s", all.Items[0].Action)
	}

	// Org scope.
	scoped, err := svc.List(service.ListFilter{OrgID: &org})
	if err != nil || scoped.Total != 2 {
		t.Fatalf("want 2 scoped, got %d, %v", scoped.Total, err)
	}

	// Action + actor + object filters.
	byAction, _ := svc.List(service.ListFilter{Action: "order.created"})
	if byAction.Total != 2 {
		t.Fatalf("want 2 created, got %d", byAction.Total)
	}
	byActor, _ := svc.List(service.ListFilter{ActorID: &actor})
	if byActor.Total != 3 {
		t.Fatalf("want 3 by actor, got %d", byActor.Total)
	}
	byObject, _ := svc.List(service.ListFilter{ObjectType: auditmodel.ObjectOrder, ObjectID: &obj})
	if byObject.Total != 4 {
		t.Fatalf("want 4 by object, got %d", byObject.Total)
	}

	// Pagination.
	p1, _ := svc.List(service.ListFilter{PerPage: 2, Page: 1})
	p2, _ := svc.List(service.ListFilter{PerPage: 2, Page: 2})
	if len(p1.Items) != 2 || len(p2.Items) != 2 || p1.Items[0].ID == p2.Items[0].ID {
		t.Fatalf("bad pages: %d / %d", len(p1.Items), len(p2.Items))
	}
}
