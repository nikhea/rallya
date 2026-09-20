package jobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/order/model"
	"github.com/nikhea/rallya/internal/order/repository"
	"github.com/nikhea/rallya/internal/order/service"
	ticketdto "github.com/nikhea/rallya/internal/ticketing/dto"
)

// stubTickets backs the sweeper test: availability always holds,
// Reserve/Release are no-ops (sweep path under test is expiry+update).
type stubTickets struct{}

func (stubTickets) InspectType(uuid.UUID) (*ticketdto.TicketType, bool, error) {
	return nil, false, nil
}

func (stubTickets) ReserveTx(_ *gorm.DB, _ uuid.UUID, _ int) error { return nil }
func (stubTickets) ReleaseTx(_ *gorm.DB, _ uuid.UUID, _ int) error { return nil }

func TestSweeperWorkerNilSafe(t *testing.T) {
	w := &SweepExpiredOrdersWorker{}
	job := &river.Job[SweepExpiredOrdersArgs]{
		JobRow: &rivertype.JobRow{ID: 1},
		Args:   SweepExpiredOrdersArgs{},
	}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("nil service must not fail, got %v", err)
	}
}

func TestSweeperWorkerSweeps(t *testing.T) {
	db := testutil.OpenTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewOrderRepository(db)
	users := testutil.NewFakeUserReader()
	svc := service.NewOrderService(repo, stubTickets{}, stubDeps{}, stubDeps{}, users)
	w := &SweepExpiredOrdersWorker{Svc: svc}
	job := &river.Job[SweepExpiredOrdersArgs]{
		JobRow: &rivertype.JobRow{ID: 2},
		Args:   SweepExpiredOrdersArgs{},
	}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("work: %v", err)
	}
}

// stubDeps implements EventLookup + OrgAccess with zero values.
type stubDeps struct{}

func (stubDeps) OrgOf(uuid.UUID) (uuid.UUID, error)   { return uuid.Nil, nil }
func (stubDeps) EventTitle(uuid.UUID) (string, error) { return "Fest", nil }
func (stubDeps) CanManage(_, _ uuid.UUID) bool        { return false }
