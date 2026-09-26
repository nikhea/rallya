package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	authmodel "github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/notification/jobs"
	ordermodel "github.com/nikhea/rallya/internal/order/model"
	"github.com/nikhea/rallya/internal/order/repository"
	"github.com/nikhea/rallya/internal/order/service"
	subservice "github.com/nikhea/rallya/internal/subscription/service"
	ticketdto "github.com/nikhea/rallya/internal/ticketing/dto"
	ticketservice "github.com/nikhea/rallya/internal/ticketing/service"
)

// fakeTickets implements service.TicketStore in-memory with real
// reserve/release accounting (no row locks on sqlite — logic only).
type fakeTickets struct {
	types map[uuid.UUID]*ticketdto.TicketType
	sold  map[uuid.UUID]int
}

func newFakeTickets() *fakeTickets {
	return &fakeTickets{types: map[uuid.UUID]*ticketdto.TicketType{}, sold: map[uuid.UUID]int{}}
}

func (f *fakeTickets) addType(id, event uuid.UUID, price, total int, max *int, published bool) {
	_ = published
	f.types[id] = &ticketdto.TicketType{
		ID: id.String(), EventID: event.String(), Name: "GA", PriceCents: price, Currency: "USD",
		QuantityTotal: total, MaxPerOrder: max, Status: "ACTIVE", ForSale: true,
	}
	f.sold[id] = 0
}

func (f *fakeTickets) InspectType(typeID uuid.UUID) (*ticketdto.TicketType, bool, error) {
	t, ok := f.types[typeID]
	if !ok {
		return nil, false, errNoType
	}
	cp := *t
	cp.QuantitySold = f.sold[typeID]
	cp.Remaining = t.QuantityTotal - f.sold[typeID]
	if cp.Remaining <= 0 {
		cp.ForSale = false
		r := "sold_out"
		cp.UnavailableReason = &r
	}
	return &cp, true, nil
}

func (f *fakeTickets) ReserveTx(_ *gorm.DB, typeID uuid.UUID, n int) error {
	t, ok := f.types[typeID]
	if !ok {
		return ticketservice.ErrTicketNotFound
	}
	if t.Status != "ACTIVE" {
		return ticketservice.ErrNotOnSale
	}
	if t.MaxPerOrder != nil && n > *t.MaxPerOrder {
		return ticketservice.ErrTooMany
	}
	if f.sold[typeID]+n > t.QuantityTotal {
		return ticketservice.ErrSoldOut
	}
	f.sold[typeID] += n
	return nil
}

func (f *fakeTickets) ReleaseTx(_ *gorm.DB, typeID uuid.UUID, n int) error {
	if _, ok := f.types[typeID]; !ok {
		return ticketservice.ErrTicketNotFound
	}
	f.sold[typeID] -= n
	if f.sold[typeID] < 0 {
		f.sold[typeID] = 0
	}
	return nil
}

var errNoType = errors.New("no type")

// fakeDeps implements EventLookup + OrgAccess.
type fakeDeps struct {
	orgs   map[uuid.UUID]uuid.UUID // event -> org
	admins map[uuid.UUID]bool
}

func (f *fakeDeps) EventTitle(eventID uuid.UUID) (string, error) {
	if _, ok := f.orgs[eventID]; !ok {
		if len(f.orgs) == 0 {
			return "Fest", nil
		}
		return "", errors.New("no event")
	}
	return "Fest", nil
}

func (f *fakeDeps) OrgOf(eventID uuid.UUID) (uuid.UUID, error) {
	o, ok := f.orgs[eventID]
	if !ok {
		return uuid.Nil, errors.New("no event")
	}
	return o, nil
}

func (f *fakeDeps) CanManage(userID, orgID uuid.UUID) bool {
	_ = orgID
	return f.admins[userID]
}

type orderFixture struct {
	db    *gorm.DB
	svc   *service.OrderService
	repo  *repository.OrderRepository
	ticks *fakeTickets
	deps  *fakeDeps
	event uuid.UUID
	org   uuid.UUID
	user  uuid.UUID
}

func newOrderFixture(t *testing.T) *orderFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	if err := db.AutoMigrate(ordermodel.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewOrderRepository(db)
	ticks := newFakeTickets()
	deps := &fakeDeps{orgs: map[uuid.UUID]uuid.UUID{}, admins: map[uuid.UUID]bool{}}
	users := testutil.NewFakeUserReader()
	userID := uuid.New()
	users.Add(&authmodel.User{ID: userID, Email: "buyer@test.com", EmailVerified: true, Status: authmodel.UserStatusActive})
	svc := service.NewOrderService(repo, ticks, deps, deps, users)
	event := uuid.New()
	org := uuid.New()
	deps.orgs[event] = org
	return &orderFixture{db: db, svc: svc, repo: repo, ticks: ticks, deps: deps, event: event, org: org, user: userID}
}

func TestCreateFreeConfirmsImmediately(t *testing.T) {
	f := newOrderFixture(t)
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 0, 10, nil, true)

	o, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{
		TicketTypeID: typeID, Quantity: 2, IdempotencyKey: "req-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if o.Status != "CONFIRMED" || o.Quantity != 2 || o.PriceCents != 0 {
		t.Fatalf("unexpected order: %+v", o)
	}
	if f.ticks.sold[typeID] != 2 {
		t.Fatalf("expected hold of 2, got %d", f.ticks.sold[typeID])
	}
	// Replay same key returns original (idempotent, no double hold).
	o2, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{
		TicketTypeID: typeID, Quantity: 2, IdempotencyKey: "req-1",
	})
	if err != nil || o2.ID != o.ID {
		t.Fatalf("replay: %+v %v", o2, err)
	}
	if f.ticks.sold[typeID] != 2 {
		t.Fatalf("double hold! sold=%d", f.ticks.sold[typeID])
	}
}

func TestCreatePricedWaitsForPayment(t *testing.T) {
	f := newOrderFixture(t)
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 2500, 10, nil, true)

	o, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{
		TicketTypeID: typeID, Quantity: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if o.Status != "PENDING_PAYMENT" || o.PriceCents != 2500 || o.ExpiresAt == nil {
		t.Fatalf("unexpected order: %+v", o)
	}
}

func TestCreateGuards(t *testing.T) {
	f := newOrderFixture(t)
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 0, 2, intPtrO(1), true)

	if _, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 0, TicketTypeID: typeID}); !errors.Is(err, service.ErrInvalidQty) {
		t.Fatalf("expected ErrInvalidQty, got %v", err)
	}
	if _, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 2, TicketTypeID: typeID}); !errors.Is(err, service.ErrTooMany) {
		t.Fatalf("expected ErrTooMany, got %v", err)
	}
	if _, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 1, TicketTypeID: uuid.New()}); !errors.Is(err, service.ErrTicketGone) {
		t.Fatalf("expected ErrTicketGone, got %v", err)
	}
	// Fill up, then sold out.
	if _, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 1, TicketTypeID: typeID}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 1, TicketTypeID: typeID}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if _, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 1, TicketTypeID: typeID}); !errors.Is(err, service.ErrSoldOut) {
		t.Fatalf("expected ErrSoldOut, got %v", err)
	}
}

func TestCancelReleasesAndGuards(t *testing.T) {
	f := newOrderFixture(t)
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 0, 5, nil, true)

	o, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 2, TicketTypeID: typeID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Stranger cannot see or cancel (stealth 404).
	stranger := uuid.New()
	if _, err := f.svc.GetOrder(stranger, mustParseO(t, o.ID)); !errors.Is(err, service.ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
	if _, err := f.svc.CancelOrder(context.Background(), stranger, mustParseO(t, o.ID)); !errors.Is(err, service.ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
	// Owner cancels: status flips, hold released.
	cancelled, err := f.svc.CancelOrder(context.Background(), f.user, mustParseO(t, o.ID))
	if err != nil || cancelled.Status != "CANCELLED" {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}
	if f.ticks.sold[typeID] != 0 {
		t.Fatalf("hold not released: %d", f.ticks.sold[typeID])
	}
	// Terminal: second cancel rejected.
	if _, err := f.svc.CancelOrder(context.Background(), f.user, mustParseO(t, o.ID)); !errors.Is(err, service.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	// Admin support path cancels others' orders.
	admin := uuid.New()
	f.deps.admins[admin] = true
	o2, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 1, TicketTypeID: typeID})
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	if _, err := f.svc.CancelOrder(context.Background(), admin, mustParseO(t, o2.ID)); err != nil {
		t.Fatalf("admin cancel: %v", err)
	}
}

func TestSweepExpired(t *testing.T) {
	f := newOrderFixture(t)
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 100, 10, nil, true)

	o, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 3, TicketTypeID: typeID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Fresh hold: nothing to sweep.
	if n, err := f.svc.SweepExpired(context.Background(), 100); err != nil || n != 0 {
		t.Fatalf("sweep fresh: n=%d err=%v", n, err)
	}
	// Backdate past TTL: swept, released, expired.
	past := time.Now().Add(-time.Minute)
	if err := f.db.Model(&ordermodel.Order{}).Where("id = ?", mustParseO(t, o.ID)).
		Update("expires_at", past).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}
	n, err := f.svc.SweepExpired(context.Background(), 100)
	if err != nil || n != 1 {
		t.Fatalf("sweep: n=%d err=%v", n, err)
	}
	if f.ticks.sold[typeID] != 0 {
		t.Fatalf("hold not released: %d", f.ticks.sold[typeID])
	}
	got, err := f.svc.GetOrder(f.user, mustParseO(t, o.ID))
	if err != nil || got.Status != "EXPIRED" {
		t.Fatalf("status: %+v %v", got, err)
	}
	// Second sweep finds nothing.
	if n, err := f.svc.SweepExpired(context.Background(), 100); err != nil || n != 0 {
		t.Fatalf("resweep: n=%d err=%v", n, err)
	}
}

func TestListMyOrders(t *testing.T) {
	f := newOrderFixture(t)
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 0, 10, nil, true)

	for i := 0; i < 3; i++ {
		if _, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 1, TicketTypeID: typeID}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	items, total, err := f.svc.ListMyOrders(f.user, 20, 0)
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("list: %d %+v %v", total, items, err)
	}
	items, total, err = f.svc.ListMyOrders(f.user, 2, 0)
	if err != nil || total != 3 || len(items) != 2 {
		t.Fatalf("paged: %d %+v %v", total, items, err)
	}
	other := uuid.New()
	_, total, err = f.svc.ListMyOrders(other, 20, 0)
	if err != nil || total != 0 {
		t.Fatalf("other user sees orders: %d %v", total, err)
	}
}

func intPtrO(n int) *int { return &n }

func mustParseO(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return id
}

func TestCheckoutDetailGuards(t *testing.T) {
	f := newOrderFixture(t)
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 100, 10, nil, true)

	o, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 1, TicketTypeID: typeID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Stranger hidden.
	if _, err := f.svc.CheckoutDetail(uuid.New(), mustParseO(t, o.ID)); !errors.Is(err, service.ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
	// Owner gets display bundle.
	d, err := f.svc.CheckoutDetail(f.user, mustParseO(t, o.ID))
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.TicketName == "" || d.EventTitle == "" {
		t.Fatalf("missing display names: %+v", d)
	}
	// Unknown order.
	if _, err := f.svc.CheckoutDetail(f.user, uuid.New()); !errors.Is(err, service.ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
}

func TestMarkPaidIdempotent(t *testing.T) {
	f := newOrderFixture(t)
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 100, 10, nil, true)

	o, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 1, TicketTypeID: typeID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	oid := mustParseO(t, o.ID)
	paid, err := f.svc.MarkPaid(oid, "cs_test_1", "pi_test_1")
	if err != nil || paid.Status != "CONFIRMED" {
		t.Fatalf("mark paid: %+v %v", paid, err)
	}
	// Redelivery succeeds silently.
	if _, err := f.svc.MarkPaid(oid, "cs_test_1", "pi_test_1"); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	// Unknown order 404s.
	if _, err := f.svc.MarkPaid(uuid.New(), "cs_x", ""); !errors.Is(err, service.ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
	// Wrong state (cancelled) rejected.
	freeID := uuid.New()
	f.ticks.addType(freeID, f.event, 0, 10, nil, true)
	o2, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 1, TicketTypeID: freeID})
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	if _, err := f.svc.CancelOrder(context.Background(), f.user, mustParseO(t, o2.ID)); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := f.svc.MarkPaid(mustParseO(t, o2.ID), "cs_x", ""); !errors.Is(err, service.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	// Session recorded.
	if err := f.svc.SetStripeSession(oid, "cs_test_9"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	got, err := f.svc.GetOrder(f.user, oid)
	if err != nil || got.Status != "CONFIRMED" {
		t.Fatalf("get: %+v %v", got, err)
	}
}

// stubMinter records mint calls.
type stubMinter struct {
	calls   int
	lastQty int
}

func (s *stubMinter) MintForOrder(_ *gorm.DB, _, _, _ uuid.UUID, _, _ string, qty int) ([]service.MintedAttendee, error) {
	s.calls++
	out := make([]service.MintedAttendee, 0, qty)
	for i := 0; i < qty; i++ {
		out = append(out, service.MintedAttendee{ID: uuid.New(), QRToken: "tok"})
	}
	s.lastQty = qty
	return out, nil
}

func (s *stubMinter) CancelForOrder(_ *gorm.DB, _ uuid.UUID) error { return nil }

func TestFreeConfirmMintsAndEmails(t *testing.T) {
	f := newOrderFixture(t)
	minter := &stubMinter{}
	fakeMail := &jobs.FakeEnqueuer{}
	f.svc.SetAttendeeMinter(minter)
	f.svc.SetEnqueuer(fakeMail)
	f.svc.SetQRSecret([]byte("test-qr-secret-32-bytes-long-abcdef"))
	_ = fakeMail
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 0, 10, nil, true)

	if _, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{Quantity: 2, TicketTypeID: typeID}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if minter.calls != 1 || minter.lastQty != 2 {
		t.Fatalf("expected 1 mint of 2, got %+v", minter)
	}
	mail := fakeMail.OfKind("send_order_confirmation_email")
	if len(mail) != 1 {
		t.Fatalf("expected 1 confirmation job, got %d", len(mail))
	}
	args, ok := mail[0].(jobs.SendOrderConfirmationEmailArgs)
	if !ok || len(args.Items) != 2 || args.Email != "buyer@test.com" {
		t.Fatalf("bad job args: %+v", mail[0])
	}
}

type fakeCounter struct{ n int64 }

func (f *fakeCounter) CountRoster(_ uuid.UUID) (int64, error) { return f.n, nil }

func TestOrderAttendeeCapacityFree(t *testing.T) {
	f := newOrderFixture(t)
	typeID := uuid.New()
	f.ticks.addType(typeID, f.event, 0, 1000, nil, true)
	// No counter wired: cap skipped (existing tests prove this path).
	// Wire a full roster at the Free cap (200): one more seat trips 409.
	f.svc.SetAttendeeCounter(&fakeCounter{n: 200})
	_, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{
		TicketTypeID: typeID, Quantity: 1,
	})
	if err != subservice.ErrEventAtCapacity {
		t.Fatalf("over cap: want ErrEventAtCapacity, got %v", err)
	}
	// Room for exactly the headroom still succeeds.
	f.svc.SetAttendeeCounter(&fakeCounter{n: 199})
	if _, err := f.svc.CreateOrder(context.Background(), f.user, f.event, service.CreateInput{
		TicketTypeID: typeID, Quantity: 1,
	}); err != nil {
		t.Fatalf("headroom: %v", err)
	}
}
