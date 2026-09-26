package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"

	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
)

// Billing event types routed from the payment webhook dispatcher.
const (
	eventCheckoutCompleted   = "checkout.session.completed"
	eventInvoicePaid         = "invoice.payment_succeeded"
	eventInvoiceFailed       = "invoice.payment_failed"
	eventSubscriptionUpdated = "customer.subscription.updated"
	eventSubscriptionDeleted = "customer.subscription.deleted"
)

// HandleBillingEvent applies one Stripe Billing event. It reports whether
// the event belonged to billing (false = caller should try other
// dispatch, e.g. one-off order fulfillment). Unknown rows ack silently
// (log, nil) — webhooks must never fail loudly on data they don't own.
func (s *SubscriptionService) HandleBillingEvent(evt *stripe.Event) (bool, error) {
	switch evt.Type {
	case eventCheckoutCompleted:
		return s.onCheckoutCompleted(evt)
	case eventInvoicePaid:
		return true, s.onInvoicePaid(evt)
	case eventInvoiceFailed:
		return true, s.onInvoiceFailed(evt)
	case eventSubscriptionUpdated:
		return true, s.onSubscriptionUpdated(evt)
	case eventSubscriptionDeleted:
		return true, s.onSubscriptionDeleted(evt)
	default:
		return false, nil
	}
}

// onCheckoutCompleted activates the subscription bought through Checkout.
// Payment-mode sessions (orders) carry no org metadata and return handled
// = false so order fulfillment still runs.
func (s *SubscriptionService) onCheckoutCompleted(evt *stripe.Event) (bool, error) {
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(evt.Data.Raw, &sess); err != nil {
		slog.Warn("billing: bad session payload", "event", evt.ID)
		return true, nil
	}
	if sess.Mode != stripe.CheckoutSessionModeSubscription {
		return false, nil
	}
	orgID, err := uuid.Parse(sess.Metadata["org_id"])
	if err != nil || sess.Metadata["org_id"] == "" {
		slog.Warn("billing: session without org metadata", "session", sess.ID)
		return true, nil
	}
	plan := submodel.Plan(sess.Metadata["plan"])
	if !plan.Paid() {
		slog.Warn("billing: session with bad plan", "session", sess.ID, "plan", sess.Metadata["plan"])
		return true, nil
	}
	subID := ""
	if sess.Subscription != nil {
		subID = sess.Subscription.ID
	}
	customerID := ""
	if sess.Customer != nil {
		customerID = sess.Customer.ID
	}
	row := &submodel.OrgSubscription{
		OrgID: orgID, Plan: plan, Status: submodel.StatusActive,
		StripeCustomerID: customerID, StripeSubscriptionID: subID,
	}
	if s.billing != nil && subID != "" {
		if info, err := s.billing.GetSubscription(context.Background(), subID); err == nil {
			row.CurrentPeriodEnd = info.CurrentPeriodEnd
			row.CancelAtPeriodEnd = info.CancelAtPeriodEnd
		}
	}
	if err := s.repo.Upsert(row); err != nil {
		return true, err
	}
	s.emitBilling(orgID, sess.Metadata["user_id"], "subscription.started", &row.ID, map[string]any{"plan": string(plan)})
	slog.Info("billing: subscription activated", "org", orgID, "plan", plan)
	return true, nil
}

// onInvoicePaid renews: ACTIVE, grace cleared, period end synced.
func (s *SubscriptionService) onInvoicePaid(evt *stripe.Event) error {
	subID, err := invoiceSubscriptionID(evt.Data.Raw)
	if err != nil || subID == "" {
		return nil
	}
	row, err := s.repo.GetBySubscriptionID(subID)
	if err != nil || row == nil {
		return err
	}
	row.Status = submodel.StatusActive
	row.GraceUntil = nil
	if s.billing != nil {
		if info, err := s.billing.GetSubscription(context.Background(), subID); err == nil {
			row.CurrentPeriodEnd = info.CurrentPeriodEnd
			row.CancelAtPeriodEnd = info.CancelAtPeriodEnd
		}
	}
	if err := s.repo.Save(row); err != nil {
		return err
	}
	s.emitBilling(row.OrgID, "", "subscription.renewed", &row.ID, map[string]any{"plan": string(row.Plan)})
	return nil
}

// onInvoiceFailed parks the org in PAST_DUE with a grace window (first
// failure stamps it; repeat failures never extend it).
func (s *SubscriptionService) onInvoiceFailed(evt *stripe.Event) error {
	subID, err := invoiceSubscriptionID(evt.Data.Raw)
	if err != nil || subID == "" {
		return nil
	}
	row, err := s.repo.GetBySubscriptionID(subID)
	if err != nil || row == nil {
		return err
	}
	if row.Status != submodel.StatusPastDue {
		grace := time.Now().AddDate(0, 0, PastDueGraceDays)
		row.GraceUntil = &grace
	}
	row.Status = submodel.StatusPastDue
	if err := s.repo.Save(row); err != nil {
		return err
	}
	s.emitBilling(row.OrgID, "", "subscription.past_due", &row.ID, map[string]any{"plan": string(row.Plan)})
	slog.Info("billing: past due, grace started", "org", row.OrgID)
	return nil
}

// onSubscriptionUpdated syncs status/plan/period/cancel flag (portal
// cancels land here with cancel_at_period_end; the drop to Free happens
// at period end via deleted).
func (s *SubscriptionService) onSubscriptionUpdated(evt *stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(evt.Data.Raw, &sub); err != nil {
		return nil
	}
	row, err := s.repo.GetBySubscriptionID(sub.ID)
	if err != nil || row == nil {
		return err
	}
	switch sub.Status {
	case stripe.SubscriptionStatusActive, stripe.SubscriptionStatusTrialing:
		row.Status = submodel.StatusActive
		row.GraceUntil = nil
	case stripe.SubscriptionStatusPastDue:
		if row.Status != submodel.StatusPastDue {
			grace := time.Now().AddDate(0, 0, PastDueGraceDays)
			row.GraceUntil = &grace
		}
		row.Status = submodel.StatusPastDue
	case stripe.SubscriptionStatusCanceled, stripe.SubscriptionStatusUnpaid, stripe.SubscriptionStatusIncompleteExpired:
		row.Status = submodel.StatusCanceled
		row.GraceUntil = nil
	default:
		// Incomplete/paused: keep current status, still sync the rest.
	}
	if plan := submodel.Plan(sub.Metadata["plan"]); plan.Paid() {
		row.Plan = plan
	}
	if end := periodEndOf(&sub); end != nil {
		row.CurrentPeriodEnd = end
	}
	row.CancelAtPeriodEnd = sub.CancelAtPeriodEnd
	if err := s.repo.Save(row); err != nil {
		return err
	}
	s.emitBilling(row.OrgID, "", "subscription.updated", &row.ID, map[string]any{
		"plan": string(row.Plan), "status": string(row.Status),
	})
	return nil
}

// onSubscriptionDeleted drops the org to CANCELED (sent after period end
// for portal cancels — entitlement resolves Free from here).
func (s *SubscriptionService) onSubscriptionDeleted(evt *stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(evt.Data.Raw, &sub); err != nil {
		return nil
	}
	row, err := s.repo.GetBySubscriptionID(sub.ID)
	if err != nil || row == nil {
		return err
	}
	row.Status = submodel.StatusCanceled
	row.GraceUntil = nil
	if err := s.repo.Save(row); err != nil {
		return err
	}
	s.emitBilling(row.OrgID, "", "subscription.canceled", &row.ID, map[string]any{"plan": string(row.Plan)})
	slog.Info("billing: subscription canceled", "org", row.OrgID)
	return nil
}

func (s *SubscriptionService) emitBilling(orgID uuid.UUID, actor string, action string, objectID *uuid.UUID, after map[string]any) {
	if s.auditor == nil {
		return
	}
	e := auditsvc.Entry{
		OrgID: &orgID, Action: action, ObjectType: auditmodel.ObjectSubscription,
		ObjectID: objectID, After: after,
	}
	if uid, err := uuid.Parse(actor); err == nil {
		e.ActorID = &uid
	}
	_ = s.auditor.EmitTx(nil, e)
}

// newCustomerRow seeds a row holding just the customer ID (plan resolves
// when the checkout completes via webhook).
func newCustomerRow(orgID uuid.UUID, customerID string) *submodel.OrgSubscription {
	return &submodel.OrgSubscription{
		OrgID: orgID, Plan: submodel.PlanFree, Status: submodel.StatusActive,
		StripeCustomerID: customerID,
	}
}

// periodEndOf reads the current period end off the first subscription item
// (the modern API keeps periods on items, not the subscription).
func periodEndOf(sub *stripe.Subscription) *time.Time {
	if sub.Items == nil {
		return nil
	}
	for _, item := range sub.Items.Data {
		if item != nil && item.CurrentPeriodEnd != 0 {
			t := time.Unix(item.CurrentPeriodEnd, 0).UTC()
			return &t
		}
	}
	return nil
}

// subInfoFrom snapshots a retrieved subscription for service sync.
func subInfoFrom(sub *stripe.Subscription) SubInfo {
	info := SubInfo{
		Status: string(sub.Status), CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
		SubscriptionID: sub.ID, CurrentPeriodEnd: periodEndOf(sub),
	}
	if plan := submodel.Plan(sub.Metadata["plan"]); plan.Paid() {
		info.Plan = plan
	}
	if sub.Customer != nil {
		info.CustomerID = sub.Customer.ID
	}
	return info
}

// invoiceRef is the minimal shape to link an invoice to its subscription.
type invoiceRef struct {
	Parent *struct {
		Type                string `json:"type"`
		SubscriptionDetails *struct {
			Subscription json.RawMessage `json:"subscription"`
		} `json:"subscription_details"`
	} `json:"parent"`
}

type idHolder struct {
	ID string `json:"id"`
}

// invoiceSubscriptionID extracts the subscription ID from an invoice event
// payload (string or expanded object form).
func invoiceSubscriptionID(raw json.RawMessage) (string, error) {
	var ref invoiceRef
	if err := json.Unmarshal(raw, &ref); err != nil {
		return "", err
	}
	if ref.Parent == nil || ref.Parent.SubscriptionDetails == nil {
		return "", nil
	}
	rawSub := ref.Parent.SubscriptionDetails.Subscription
	var id string
	if err := json.Unmarshal(rawSub, &id); err == nil {
		return id, nil
	}
	var obj idHolder
	if err := json.Unmarshal(rawSub, &obj); err != nil {
		return "", err
	}
	return obj.ID, nil
}
