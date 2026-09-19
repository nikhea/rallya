package service_test

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/auth/testutil"
	ticketdto "github.com/nikhea/rallya/internal/ticketing/dto"
	ticketmodel "github.com/nikhea/rallya/internal/ticketing/model"
	"github.com/nikhea/rallya/internal/ticketing/repository"
	"github.com/nikhea/rallya/internal/ticketing/service"
)

// fakeEvents implements service.EventResolver with one published event.
type fakeEvents struct {
	events map[uuid.UUID]*eventInfo
	byRef  map[string]uuid.UUID
}

type eventInfo = service.EventInfo

func newFakeEvents() *fakeEvents {
	return &fakeEvents{events: map[uuid.UUID]*service.EventInfo{}, byRef: map[string]uuid.UUID{}}
}

func (f *fakeEvents) addEvent(slug string, capacity *int, published bool) uuid.UUID {
	id := uuid.New()
	status := "DRAFT"
	if published {
		status = "PUBLISHED"
	}
	f.events[id] = &service.EventInfo{ID: id, Status: status, Capacity: capacity}
	f.byRef[slug] = id
	return id
}

func (f *fakeEvents) GetEvent(_ *uuid.UUID, orgRef, eventRef string) (service.EventInfo, error) {
	_ = orgRef
	id, ok := f.byRef[eventRef]
	if !ok {
		return service.EventInfo{}, errNoEvent
	}
	return *f.events[id], nil
}

func (f *fakeEvents) GetPublicEvent(eventID uuid.UUID) (service.EventInfo, error) {
	e, ok := f.events[eventID]
	if !ok || e.Status != "PUBLISHED" {
		return service.EventInfo{}, errNoEvent
	}
	return *e, nil
}

var errNoEvent = errors.New("no event")

type ticketFixture struct {
	db     *gorm.DB
	svc    *service.TicketService
	repo   *repository.TicketRepository
	events *fakeEvents
	event  uuid.UUID
	user   uuid.UUID
}

func newTicketFixture(t *testing.T, capacity *int) *ticketFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	if err := db.AutoMigrate(ticketmodel.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewTicketRepository(db)
	events := newFakeEvents()
	svc := service.NewTicketService(repo, events)
	event := events.addEvent("fest", capacity, true)
	return &ticketFixture{db: db, svc: svc, repo: repo, events: events, event: event, user: uuid.New()}
}

func ptr[T any](v T) *T { return &v }

func TestCreateValidationAndCapacity(t *testing.T) {
	cap := 100
	f := newTicketFixture(t, &cap)

	if _, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: "  "}); !errors.Is(err, service.ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
	if _, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: "X", PriceCents: -1, QuantityTotal: 1}); !errors.Is(err, service.ErrInvalidPrice) {
		t.Fatalf("expected ErrInvalidPrice, got %v", err)
	}
	if _, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: "X", QuantityTotal: 0}); !errors.Is(err, service.ErrInvalidQty) {
		t.Fatalf("expected ErrInvalidQty, got %v", err)
	}
	// Over event capacity.
	if _, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: "Big", QuantityTotal: 101}); !errors.Is(err, service.ErrEventCapacity) {
		t.Fatalf("expected ErrEventCapacity, got %v", err)
	}
	// Bad window.
	past := time.Now().Add(-time.Hour)
	if _, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: "X", QuantityTotal: 1, SaleStartsAt: &past, SaleEndsAt: &past}); !errors.Is(err, service.ErrInvalidWindow) {
		t.Fatalf("expected ErrInvalidWindow, got %v", err)
	}
	// Unknown event.
	if _, err := f.svc.CreateType(&f.user, "acme", "ghost", service.CreateInput{Name: "X", QuantityTotal: 1}); !errors.Is(err, service.ErrEventUnresolved) {
		t.Fatalf("expected ErrEventUnresolved, got %v", err)
	}
	// Happy path with defaults (USD, DRAFT).
	tt, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: "GA", QuantityTotal: 60, PriceCents: 2500})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tt.Currency != "USD" || tt.Status != "DRAFT" || tt.SoldOut || tt.Remaining != 60 {
		t.Fatalf("unexpected type: %+v", tt)
	}
	// Second type fits headroom exactly (60 + 40 = 100).
	if _, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: "VIP", QuantityTotal: 40}); err != nil {
		t.Fatalf("exact fit: %v", err)
	}
	// Third exceeds.
	if _, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: "X", QuantityTotal: 1}); !errors.Is(err, service.ErrEventCapacity) {
		t.Fatalf("expected ErrEventCapacity, got %v", err)
	}
}

func TestLifecycleAndGuards(t *testing.T) {
	f := newTicketFixture(t, nil)
	tt, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: "GA", QuantityTotal: 10})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Activate -> pause -> activate.
	if _, err := f.svc.Activate(&f.user, "acme", "fest", tt.ID); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := f.svc.Activate(&f.user, "acme", "fest", tt.ID); !errors.Is(err, service.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	if _, err := f.svc.Pause(&f.user, "acme", "fest", tt.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, err := f.svc.Activate(&f.user, "acme", "fest", tt.ID)
	if err != nil || got.Status != "ACTIVE" {
		t.Fatalf("reactivate: %+v %v", got, err)
	}
	// Update guards: cut below sold, bad price, bad window.
	if err := f.svc.Reserve(mustParseTT(t, tt.ID), 4); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := f.svc.UpdateType(&f.user, "acme", "fest", tt.ID, service.UpdateInput{QuantityTotal: ptr(2)}); !errors.Is(err, service.ErrHasSales) {
		t.Fatalf("expected ErrHasSales, got %v", err)
	}
	bad := -5
	if _, err := f.svc.UpdateType(&f.user, "acme", "fest", tt.ID, service.UpdateInput{PriceCents: &bad}); !errors.Is(err, service.ErrInvalidPrice) {
		t.Fatalf("expected ErrInvalidPrice, got %v", err)
	}
	// Grow within headroom (no event cap).
	grown, err := f.svc.UpdateType(&f.user, "acme", "fest", tt.ID, service.UpdateInput{QuantityTotal: ptr(20)})
	if err != nil || grown.QuantityTotal != 20 {
		t.Fatalf("grow: %+v %v", grown, err)
	}
	// Delete with sales -> 409; release then delete works.
	if err := f.svc.DeleteType(&f.user, "acme", "fest", tt.ID); !errors.Is(err, service.ErrHasSales) {
		t.Fatalf("expected ErrHasSales, got %v", err)
	}
	if err := f.svc.Release(mustParseTT(t, tt.ID), 4); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := f.svc.Release(mustParseTT(t, tt.ID), 1); !errors.Is(err, service.ErrInvalidQty) {
		t.Fatalf("expected ErrInvalidQty (below zero), got %v", err)
	}
	if err := f.svc.DeleteType(&f.user, "acme", "fest", tt.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := f.svc.GetType(&f.user, "acme", "fest", tt.ID); !errors.Is(err, service.ErrTicketNotFound) {
		t.Fatalf("expected ErrTicketNotFound, got %v", err)
	}
}

func TestAvailabilityMatrix(t *testing.T) {
	f := newTicketFixture(t, nil)
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-2 * time.Hour)
	pastEnd := time.Now().Add(-time.Hour)
	mk := func(name string, qty int, status ticketmodel.TicketStatus, start, end *time.Time) string {
		tt, err := f.svc.CreateType(&f.user, "acme", "fest", service.CreateInput{Name: name, QuantityTotal: qty})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		f.setStatus(t, tt.ID, status)
		if start != nil || end != nil {
			if _, err := f.svc.UpdateType(&f.user, "acme", "fest", tt.ID, service.UpdateInput{SaleStartsAt: start, SaleEndsAt: end}); err != nil {
				t.Fatalf("window: %v", err)
			}
		}
		return tt.ID
	}
	draftID := mk("Draft", 10, ticketmodel.TicketStatusDraft, nil, nil)
	activeID := mk("Live", 10, ticketmodel.TicketStatusActive, nil, nil)
	mustReserve(t, f, activeID, 10) // sold out
	mk("Early", 10, ticketmodel.TicketStatusActive, &future, nil)
	mk("Old", 10, ticketmodel.TicketStatusActive, &past, &pastEnd)

	items, err := f.svc.ListPublicTypes(f.event)
	if err != nil {
		t.Fatalf("public list: %v", err)
	}
	// Draft excluded from public list entirely.
	byName := map[string]ticketdto.TicketType{}
	for _, it := range items {
		byName[it.Name] = it
	}
	if _, ok := byName["Draft"]; ok {
		t.Fatal("draft must not list publicly")
	}
	check := func(name string, forSale bool, reason string) {
		it, ok := byName[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		gotReason := ""
		if it.UnavailableReason != nil {
			gotReason = *it.UnavailableReason
		}
		if it.ForSale != forSale || gotReason != reason {
			t.Fatalf("%s: forSale=%v reason=%q", name, it.ForSale, gotReason)
		}
		_ = draftID
	}
	check("Live", false, "sold_out")
	check("Early", false, "not_started")
	check("Old", false, "ended")
	// Paused type: hidden publicly, visible to members as not for sale.
	pausedID := mk("Paused", 10, ticketmodel.TicketStatusActive, nil, nil)
	if _, err := f.svc.Pause(&f.user, "acme", "fest", pausedID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	items, _ = f.svc.ListPublicTypes(f.event)
	for _, it := range items {
		if it.Name == "Paused" {
			t.Fatal("paused must not list publicly")
		}
	}
	memberItems, err := f.svc.ListTypes(&f.user, "acme", "fest")
	if err != nil {
		t.Fatalf("member list: %v", err)
	}
	found := false
	for _, it := range memberItems {
		if it.Name == "Paused" {
			found = true
			if it.ForSale || it.UnavailableReason == nil || *it.UnavailableReason != "not_active" {
				t.Fatalf("paused member view: %+v", it)
			}
		}
	}
	if !found {
		t.Fatal("paused must list for members")
	}
}

func mustParseTT(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return id
}

func (f *ticketFixture) setStatus(t *testing.T, id string, status ticketmodel.TicketStatus) {
	t.Helper()
	tid, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := f.db.Model(&ticketmodel.TicketType{}).Where("id = ?", tid).Update("status", status).Error; err != nil {
		t.Fatalf("set status: %v", err)
	}
}

func mustReserve(t *testing.T, f *ticketFixture, id string, n int) {
	t.Helper()
	if err := f.svc.Reserve(mustParseTT(t, id), n); err != nil {
		t.Fatalf("reserve: %v", err)
	}
}

// TestReserveConcurrencyPG hammers Reserve from many goroutines against real
// Postgres: sold must land exactly on total, never overshoot. Requires
// DATABASE_URL_TEST; skipped otherwise (SQLite ignores FOR UPDATE, so the
// guarantee is untestable there).
func TestReserveConcurrencyPG(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL_TEST")
	if dsn == "" {
		t.Skip("DATABASE_URL_TEST unset")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	_ = db.Migrator().DropTable(&ticketmodel.TicketType{})
	if err := db.AutoMigrate(&ticketmodel.TicketType{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer func() { _ = db.Migrator().DropTable(&ticketmodel.TicketType{}) }()

	repo := repository.NewTicketRepository(db)
	events := newFakeEvents()
	events.addEvent("fest", nil, true)
	svc := service.NewTicketService(repo, events)
	total := 50
	tt, err := svc.CreateType(&uuid.Nil, "acme", "fest", service.CreateInput{Name: "GA", QuantityTotal: total})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Activate(&uuid.Nil, "acme", "fest", tt.ID); err != nil {
		t.Fatalf("activate: %v", err)
	}
	typeID := mustParseTT(t, tt.ID)

	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				if err := svc.Reserve(typeID, 1); err == nil {
					mu.Lock()
					succeeded++
					mu.Unlock()
				} else if !errors.Is(err, service.ErrSoldOut) {
					t.Errorf("unexpected reserve error: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if succeeded != total {
		t.Fatalf("succeeded=%d, want exactly %d (oversell or lost sales)", succeeded, total)
	}
	final, err := repo.GetTypeByID(typeID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if final.QuantitySold != total {
		t.Fatalf("sold=%d, want %d", final.QuantitySold, total)
	}
}
