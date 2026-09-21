package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	attendeeutils "github.com/nikhea/rallya/internal/attendee/utils"
	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	auth "github.com/nikhea/rallya/internal/auth"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orderdto "github.com/nikhea/rallya/internal/order/dto"
	"github.com/nikhea/rallya/internal/order/model"
	"github.com/nikhea/rallya/internal/order/repository"
	"github.com/nikhea/rallya/internal/order/utils"
	ticketdto "github.com/nikhea/rallya/internal/ticketing/dto"
	ticketservice "github.com/nikhea/rallya/internal/ticketing/service"
)

// HoldTTL bounds PENDING inventory holds before the sweeper releases them.
const HoldTTL = 15 * time.Minute

// TicketStore is implemented by the ticketing service: type reads plus
// the row-locked inventory mutations. Orders never touch ticket tables.
type TicketStore interface {
	InspectType(typeID uuid.UUID) (*ticketdto.TicketType, bool, error)
	ReserveTx(tx *gorm.DB, typeID uuid.UUID, n int) error
	ReleaseTx(tx *gorm.DB, typeID uuid.UUID, n int) error
}

// EventLookup resolves an event's owning org for admin checks.
type EventLookup interface {
	OrgOf(eventID uuid.UUID) (uuid.UUID, error)
	EventTitle(eventID uuid.UUID) (string, error)
}

// OrgAccess answers management rights for support actions.
type OrgAccess interface {
	CanManage(userID, orgID uuid.UUID) bool
}

// AttendeeMinter mints door records for confirmed orders. Declared here
// (consumer side); attendee implements it. Nil-safe: skipped when unwired.
type AttendeeMinter interface {
	MintForOrder(tx *gorm.DB, orderID, userID, eventID uuid.UUID, email, name string, quantity int) ([]MintedAttendee, error)
	CancelForOrder(tx *gorm.DB, orderID uuid.UUID) error
}

// MintedAttendee is one minted row plus its raw token (email-embed only).
type MintedAttendee struct {
	ID      uuid.UUID
	QRToken string
}

// OrderService orchestrates order flows.
type OrderService struct {
	repo     *repository.OrderRepository
	tickets  TicketStore
	events   EventLookup
	orgs     OrgAccess
	users    auth.UserReader
	minter   AttendeeMinter
	enqueuer jobs.Enqueuer
	qrSecret []byte
	auditor  auditsvc.Emitter
}

// NewOrderService builds the service. tickets/events/orgs/users required;
// minter/enqueuer wired via setters, nil-safe when absent (tests).
func NewOrderService(repo *repository.OrderRepository, tickets TicketStore, events EventLookup, orgs OrgAccess, users auth.UserReader) *OrderService {
	return &OrderService{repo: repo, tickets: tickets, events: events, orgs: orgs, users: users}
}

// SetAttendeeMinter wires door-record minting.
func (s *OrderService) SetAttendeeMinter(m AttendeeMinter) { s.minter = m }

// SetEnqueuer wires River job insertion (confirmation emails).
func (s *OrderService) SetEnqueuer(e jobs.Enqueuer) { s.enqueuer = e }

// SetQRSecret wires the QR HMAC secret (resolved once at boot; fail-closed
// there, never per-request — os.Exit in a handler would kill the process).
func (s *OrderService) SetQRSecret(secret []byte) { s.qrSecret = secret }

// SetAuditEmitter wires audit-trail emission (nil-safe when absent).
func (s *OrderService) SetAuditEmitter(a auditsvc.Emitter) { s.auditor = a }

// emit records one audit entry (nil-safe when unwired). Call inside the
// action's tx so entry and mutation commit atomically.
func (s *OrderService) emit(tx *gorm.DB, e auditsvc.Entry) error {
	if s.auditor == nil {
		return nil
	}
	return s.auditor.EmitTx(tx, e)
}

// orgOf resolves the order's org for audit scope (best-effort: audit must
// never fail the order when the event lookup hiccups — callers decide).
func (s *OrderService) orgOf(eventID uuid.UUID) (uuid.UUID, error) {
	return s.events.OrgOf(eventID)
}

// CreateInput carries the claim request.
type CreateInput struct {
	TicketTypeID   uuid.UUID
	Quantity       int
	IdempotencyKey string
}

// CreateOrder claims inventory: validates availability, reserves units, and
// creates the order atomically. Free (0-cent) orders confirm immediately;
// priced orders land in PENDING_PAYMENT for the payments module. Replays
// with the same idempotency key return the original order. The ticket type
// must belong to eventID (path scope) or the request 404s.
func (s *OrderService) CreateOrder(ctx context.Context, userID, eventID uuid.UUID, in CreateInput) (*orderdto.Order, error) {
	if in.Quantity <= 0 {
		return nil, ErrInvalidQty
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if key != "" {
		if existing, err := s.repo.GetOrderByIdempotency(userID, key); err == nil {
			return toOrder(existing), nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	tt, published, err := s.tickets.InspectType(in.TicketTypeID)
	if err != nil {
		return nil, ErrTicketGone
	}
	if tt.EventID != eventID.String() {
		return nil, ErrTicketGone
	}
	if !published {
		return nil, ErrTicketGone
	}
	if !tt.ForSale {
		reason := ""
		if tt.UnavailableReason != nil {
			reason = *tt.UnavailableReason
		}
		return nil, unavailableErr(reason)
	}
	if tt.MaxPerOrder != nil && in.Quantity > *tt.MaxPerOrder {
		return nil, ErrTooMany
	}
	now := time.Now()
	o := &model.Order{
		UserID: userID, EventID: eventID, TicketTypeID: mustParseUUID(tt.ID),
		Quantity: in.Quantity, PriceCents: tt.PriceCents, Currency: tt.Currency,
	}
	if key != "" {
		o.IdempotencyKey = &key
	}
	if tt.PriceCents == 0 {
		o.Status = model.OrderStatusConfirmed
	} else {
		o.Status = model.OrderStatusPendingPayment
		o.ExpiresAt = &[]time.Time{now.Add(HoldTTL)}[0]
	}
	orgID, err := s.orgOf(eventID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		// Hold joins the order tx: a failed insert rolls the hold back —
		// no phantom inventory.
		if err := s.tickets.ReserveTx(tx, mustParseUUID(tt.ID), in.Quantity); err != nil {
			return morphReserveErr(err)
		}
		if err := s.repo.CreateOrder(tx, o); err != nil {
			return err
		}
		if err := s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: &userID,
			Action: "order.created", ObjectType: auditmodel.ObjectOrder, ObjectID: &o.ID,
			After: map[string]any{"status": string(o.Status), "quantity": o.Quantity, "priceCents": o.PriceCents},
		}); err != nil {
			return err
		}
		// Free orders confirm immediately: mint door records + queue the
		// buyer email atomically with the order.
		if o.Status == model.OrderStatusConfirmed {
			minted, err := s.mintTx(tx, o, userID)
			if err != nil {
				return err
			}
			if err := s.enqueueConfirmation(tx, o, userID, minted); err != nil {
				return err
			}
			return s.emit(tx, auditsvc.Entry{
				OrgID: &orgID, ActorID: &userID,
				Action: "order.confirmed", ObjectType: auditmodel.ObjectOrder, ObjectID: &o.ID,
				After: map[string]any{"status": string(o.Status)},
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return toOrder(o), nil
}

// mintTx mints attendee rows inside the confirmation tx (nil-safe).
// Returns minted rows with raw tokens for email embedding.
func (s *OrderService) mintTx(tx *gorm.DB, o *model.Order, userID uuid.UUID) ([]MintedAttendee, error) {
	if s.minter == nil {
		return nil, nil
	}
	u, err := s.users.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	var name string
	if u.Profile != nil && u.Profile.FirstName != nil {
		name = *u.Profile.FirstName
	}
	if name == "" {
		name = u.Email
	}
	return s.minter.MintForOrder(tx, o.ID, userID, o.EventID, u.Email, name, o.Quantity)
}

// enqueueConfirmation builds QR payloads from freshly minted tokens and
// queues the buyer email in-tx (nil-safe without minter/enqueuer/secret;
// boot guarantees the secret in production).
func (s *OrderService) enqueueConfirmation(tx *gorm.DB, o *model.Order, userID uuid.UUID, minted []MintedAttendee) error {
	if s.enqueuer == nil || len(minted) == 0 || len(s.qrSecret) == 0 {
		return nil
	}
	u, err := s.users.GetUserByID(userID)
	if err != nil {
		return err
	}
	title, err := s.events.EventTitle(o.EventID)
	if err != nil {
		return err
	}
	items := make([]jobs.OrderConfirmationItem, 0, len(minted))
	for _, m := range minted {
		items = append(items, jobs.OrderConfirmationItem{
			QRPayload: attendeeutils.BuildPayload(m.ID, m.QRToken, s.qrSecret),
		})
	}
	name := u.Email
	if u.Profile != nil && u.Profile.FirstName != nil && *u.Profile.FirstName != "" {
		name = *u.Profile.FirstName
	}
	return s.enqueuer.EnqueueTx(context.Background(), tx, jobs.SendOrderConfirmationEmailArgs{
		OrderID: o.ID, Email: u.Email, Name: name,
		EventTitle: title, Items: items,
	})
}

// GetOrder returns an order to its owner (or org ADMIN+ via admin path).
func (s *OrderService) GetOrder(userID, orderID uuid.UUID) (*orderdto.Order, error) {
	o, err := s.repo.GetOrderByID(orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	if o.UserID != userID && !s.manageable(userID, o) {
		return nil, ErrOrderNotFound // stealth: hide others' orders
	}
	return toOrder(o), nil
}

// FindOrder is the unguarded internal lookup for the payments domain
// (webhook fulfillment has no user context). Never exposed over HTTP.
func (s *OrderService) FindOrder(orderID uuid.UUID) (*model.Order, error) {
	return s.repo.GetOrderByID(orderID)
}

// CheckoutDetail bundles order + display names for checkout creation.
// Owner-only; priced PENDING_PAYMENT orders only (free needs no checkout).
func (s *OrderService) CheckoutDetail(userID, orderID uuid.UUID) (*CheckoutDetail, error) {
	o, err := s.repo.GetOrderByID(orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	if o.UserID != userID {
		return nil, ErrOrderNotFound
	}
	if o.Status != model.OrderStatusPendingPayment {
		return nil, ErrInvalidStatus
	}
	tt, _, err := s.tickets.InspectType(o.TicketTypeID)
	if err != nil {
		return nil, ErrOrderNotFound
	}
	title, err := s.events.EventTitle(o.EventID)
	if err != nil {
		return nil, ErrOrderNotFound
	}
	return &CheckoutDetail{
		Order: o, TicketName: tt.Name, EventTitle: title,
	}, nil
}

// CheckoutDetail is the payments-facing order view (internal use only).
type CheckoutDetail struct {
	Order      *model.Order
	TicketName string
	EventTitle string
}

// MarkPaid transitions PENDING_PAYMENT -> CONFIRMED after a verified Stripe
// webhook. Idempotent: already-CONFIRMED orders succeed silently (webhook
// redelivery). Any other status is rejected. Records session/intent + paid_at.
func (s *OrderService) MarkPaid(orderID uuid.UUID, sessionID, paymentIntentID string) (*orderdto.Order, error) {
	o, err := s.repo.GetOrderByID(orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	if o.Status == model.OrderStatusConfirmed {
		return toOrder(o), nil
	}
	if o.Status != model.OrderStatusPendingPayment {
		return nil, ErrInvalidStatus
	}
	now := time.Now()
	before := string(o.Status)
	o.Status = model.OrderStatusConfirmed
	o.StripeSessionID = &sessionID
	if paymentIntentID != "" {
		o.StripePaymentIntentID = &paymentIntentID
	}
	o.PaidAt = &now
	orgID, err := s.orgOf(o.EventID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpdateOrder(tx, o); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID:  &orgID, // webhook: system actor (nil)
			Action: "order.confirmed", ObjectType: auditmodel.ObjectOrder, ObjectID: &o.ID,
			Before: map[string]any{"status": before},
			After:  map[string]any{"status": string(o.Status)},
		})
	}); err != nil {
		return nil, err
	}
	return toOrder(o), nil
}

// SetStripeSession records the checkout session on a payable order
// (PENDING_PAYMENT at creation; CONFIRMED overwrites are harmless no-ops
// for late replays). Other states reject: nothing should link sessions
// to dead orders.
func (s *OrderService) SetStripeSession(orderID uuid.UUID, sessionID string) error {
	o, err := s.repo.GetOrderByID(orderID)
	if err != nil {
		return err
	}
	if o.Status != model.OrderStatusPendingPayment && o.Status != model.OrderStatusConfirmed {
		return ErrInvalidStatus
	}
	o.StripeSessionID = &sessionID
	return s.repo.UpdateOrder(nil, o)
}

// ListMyOrders returns the caller's history, newest first.
func (s *OrderService) ListMyOrders(userID uuid.UUID, limit, offset int) ([]orderdto.Order, int64, error) {
	os_, total, err := s.repo.ListUserOrders(userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]orderdto.Order, 0, len(os_))
	for _, o := range os_ {
		out = append(out, *toOrder(&o))
	}
	return out, total, nil
}

// CancelOrder cancels by owner, or by org ADMIN+ (support path).
// Cancelling releases held inventory.
func (s *OrderService) CancelOrder(ctx context.Context, callerID, orderID uuid.UUID) (*orderdto.Order, error) {
	o, err := s.repo.GetOrderByID(orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	if o.UserID != callerID && !s.manageable(callerID, o) {
		return nil, ErrOrderNotFound
	}
	if o.Status.Terminal() && o.Status != model.OrderStatusConfirmed {
		return nil, ErrInvalidStatus
	}
	// Any live order (holds and confirmed-free alike) releases inventory.
	before := string(o.Status)
	orgID, err := s.orgOf(o.EventID)
	if err != nil {
		return nil, err
	}
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.tickets.ReleaseTx(tx, o.TicketTypeID, o.Quantity); err != nil {
			return morphReleaseErr(err)
		}
		o.Status = model.OrderStatusCancelled
		if err := s.repo.UpdateOrder(tx, o); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: &callerID,
			Action: "order.cancelled", ObjectType: auditmodel.ObjectOrder, ObjectID: &o.ID,
			Before: map[string]any{"status": before},
			After:  map[string]any{"status": string(o.Status)},
		})
	})
	if err != nil {
		slog.Warn("order cancel failed", "order", o.ID, "error", err)
		return nil, err
	}
	return toOrder(o), nil
}

// SweepExpired expires held orders past their deadline and releases
// inventory: PENDING and PENDING_PAYMENT alike (unpaid holds must never
// pin inventory forever; payments charges only unexpired PENDING_PAYMENT).
// Called by the periodic River job; batch-capped per run.
func (s *OrderService) SweepExpired(ctx context.Context, batch int) (int, error) {
	if batch <= 0 || batch > 500 {
		batch = 100
	}
	expired, err := s.repo.ExpiredPending(time.Now(), batch)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, o := range expired {
		if err := s.expireOne(ctx, o); err != nil {
			slog.Warn("sweep: expire failed", "order", o.ID, "error", err)
			continue
		}
		done++
	}
	return done, nil
}

func (s *OrderService) expireOne(ctx context.Context, o model.Order) error {
	_ = ctx
	before := string(o.Status)
	orgID, err := s.orgOf(o.EventID)
	if err != nil {
		return err
	}
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.tickets.ReleaseTx(tx, o.TicketTypeID, o.Quantity); err != nil {
			return err
		}
		o.Status = model.OrderStatusExpired
		if err := s.repo.UpdateOrder(tx, &o); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID:  &orgID, // sweeper: system actor (nil)
			Action: "order.expired", ObjectType: auditmodel.ObjectOrder, ObjectID: &o.ID,
			Before: map[string]any{"status": before},
			After:  map[string]any{"status": string(o.Status)},
		})
	})
}

// manageable reports org ADMIN+ over the order's event org (support path).
func (s *OrderService) manageable(userID uuid.UUID, o *model.Order) bool {
	orgID, err := s.events.OrgOf(o.EventID)
	if err != nil {
		return false
	}
	return s.orgs.CanManage(userID, orgID)
}

func unavailableErr(reason string) error {
	switch reason {
	case "sold_out":
		return ErrSoldOut
	default:
		return ErrTicketGone
	}
}

// morphReserveErr maps ticket-domain failures to order-domain sentinels
// (compared by identity against the provider's exported sentinels).
// Handlers only ever see order errors.
func morphReserveErr(err error) error {
	switch {
	case errors.Is(err, ticketservice.ErrSoldOut):
		return ErrSoldOut
	case errors.Is(err, ticketservice.ErrInvalidQty):
		return ErrInvalidQty
	case errors.Is(err, ticketservice.ErrTicketNotFound),
		errors.Is(err, ticketservice.ErrNotOnSale):
		return ErrTicketGone
	case errors.Is(err, ticketservice.ErrTooMany):
		return ErrTooMany
	default:
		return ErrTicketGone
	}
}

func morphReleaseErr(err error) error { return ErrTicketGone }

func mustParseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func toOrder(o *model.Order) *orderdto.Order {
	return &orderdto.Order{
		ID: o.ID.String(), EventID: o.EventID.String(), TicketTypeID: o.TicketTypeID.String(),
		Quantity: o.Quantity, PriceCents: o.PriceCents, Currency: o.Currency,
		Status: string(o.Status), ExpiresAt: utils.FormatTimePtr(o.ExpiresAt),
		CreatedAt: utils.FormatTime(o.CreatedAt),
	}
}
