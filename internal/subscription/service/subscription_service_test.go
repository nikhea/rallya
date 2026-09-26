package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"

	"github.com/nikhea/rallya/internal/auth/testutil"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
	"github.com/nikhea/rallya/internal/subscription/repository"
	"github.com/nikhea/rallya/internal/subscription/service"
)

type fakeOwners struct {
	owners map[uuid.UUID]uuid.UUID // org -> owner
}

func (f *fakeOwners) IsOwner(userID, orgID uuid.UUID) (bool, error) {
	return f.owners[orgID] == userID, nil
}

type fakeBilling struct {
	customer      string
	checkoutURL   string
	checkoutSess  string
	portalURL     string
	subInfo       service.SubInfo
	subInfoErr    error
	priceAmount   int64
	priceCurrency string
	priceErr      error
	lastCheckout  service.CheckoutArgs
}

func (f *fakeBilling) GetOrCreateCustomer(_ context.Context, _ uuid.UUID) (string, error) {
	return f.customer, nil
}

func (f *fakeBilling) CreateSubscriptionCheckout(_ context.Context, a service.CheckoutArgs) (string, string, error) {
	f.lastCheckout = a
	return f.checkoutURL, f.checkoutSess, nil
}

func (f *fakeBilling) CreatePortalSession(_ context.Context, _, _ string) (string, error) {
	return f.portalURL, nil
}

func (f *fakeBilling) GetSubscription(_ context.Context, _ string) (service.SubInfo, error) {
	return f.subInfo, f.subInfoErr
}

func (f *fakeBilling) GetPrice(_ context.Context, _ string) (int64, string, error) {
	return f.priceAmount, f.priceCurrency, f.priceErr
}

type subHarness struct {
	svc     *service.SubscriptionService
	repo    *repository.SubscriptionRepository
	owners  *fakeOwners
	billing *fakeBilling
	orgID   uuid.UUID
	ownerID uuid.UUID
}

func newSubHarness(t *testing.T) *subHarness {
	t.Helper()
	t.Setenv("STRIPE_PRICE_PRO", "price_pro_test")
	t.Setenv("STRIPE_PRICE_SCALE", "price_scale_test")
	db := testutil.OpenTestDB(t)
	if err := db.AutoMigrate(submodel.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewSubscriptionRepository(db)
	owners := &fakeOwners{owners: map[uuid.UUID]uuid.UUID{}}
	billing := &fakeBilling{
		customer: "cus_1", checkoutURL: "https://checkout/session", checkoutSess: "cs_1",
		portalURL: "https://portal/session", priceAmount: 2900, priceCurrency: "usd",
	}
	svc := service.NewSubscriptionService(repo, owners)
	svc.SetBillingProvider(billing)
	orgID := uuid.New()
	ownerID := uuid.New()
	owners.owners[orgID] = ownerID
	return &subHarness{svc: svc, repo: repo, owners: owners, billing: billing, orgID: orgID, ownerID: ownerID}
}

func seedSub(t *testing.T, h *subHarness, plan submodel.Plan, status submodel.SubStatus, grace *time.Time) {
	t.Helper()
	if err := h.repo.Upsert(&submodel.OrgSubscription{
		OrgID: h.orgID, Plan: plan, Status: status,
		StripeCustomerID: "cus_1", StripeSubscriptionID: "sub_1", GraceUntil: grace,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestEntitlementDefaultsFree(t *testing.T) {
	h := newSubHarness(t)
	ent, err := h.svc.EntitlementFor(h.orgID)
	if err != nil {
		t.Fatalf("entitlement: %v", err)
	}
	if ent.Plan != submodel.PlanFree || ent.Limits.MaxEvents != 3 || ent.Can(submodel.FeatureKits) {
		t.Fatalf("absent row must resolve restrictive Free, got %+v", ent)
	}
}

func TestEntitlementMatrix(t *testing.T) {
	future := time.Now().Add(48 * time.Hour)
	past := time.Now().Add(-time.Hour)
	cases := []struct {
		name   string
		plan   submodel.Plan
		status submodel.SubStatus
		grace  *time.Time
		want   submodel.Plan
	}{
		{"active pro", submodel.PlanPro, submodel.StatusActive, nil, submodel.PlanPro},
		{"active scale", submodel.PlanScale, submodel.StatusActive, nil, submodel.PlanScale},
		{"past due in grace", submodel.PlanPro, submodel.StatusPastDue, &future, submodel.PlanPro},
		{"past due nil grace", submodel.PlanPro, submodel.StatusPastDue, nil, submodel.PlanPro},
		{"past due lapsed", submodel.PlanPro, submodel.StatusPastDue, &past, submodel.PlanFree},
		{"canceled", submodel.PlanPro, submodel.StatusCanceled, nil, submodel.PlanFree},
		{"unknown plan", "ULTRA", submodel.StatusActive, nil, submodel.PlanFree},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSubHarness(t)
			seedSub(t, h, tc.plan, tc.status, tc.grace)
			ent, err := h.svc.EntitlementFor(h.orgID)
			if err != nil {
				t.Fatalf("entitlement: %v", err)
			}
			if ent.Plan != tc.want {
				t.Fatalf("want %s, got %s", tc.want, ent.Plan)
			}
			if tc.want == submodel.PlanFree && (ent.Can(submodel.FeatureAPIKeys) || ent.Limits.MaxKitsPerEvent != 0) {
				t.Fatalf("free must gate features: %+v", ent)
			}
			if tc.want == submodel.PlanScale && ent.Limits.MaxKitsPerEvent != -1 {
				t.Fatalf("scale kits must be unlimited: %+v", ent)
			}
		})
	}
}

func TestCatalogLimits(t *testing.T) {
	h := newSubHarness(t)
	tiers, err := h.svc.Catalog(context.Background())
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if len(tiers) != 3 {
		t.Fatalf("want 3 tiers, got %d", len(tiers))
	}
	byPlan := map[submodel.Plan]service.TierView{}
	for _, tr := range tiers {
		byPlan[tr.Plan] = tr
	}
	if byPlan[submodel.PlanFree].Limits.MaxEvents != 3 || len(byPlan[submodel.PlanFree].Features) != 0 {
		t.Fatalf("bad free tier: %+v", byPlan[submodel.PlanFree])
	}
	if byPlan[submodel.PlanPro].MonthlyCents != 2900 || byPlan[submodel.PlanPro].Currency != "usd" {
		t.Fatalf("amounts must resolve from provider: %+v", byPlan[submodel.PlanPro])
	}
}

func TestCheckoutGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("unconfigured billing", func(t *testing.T) {
		h := newSubHarness(t)
		h.svc.SetBillingProvider(nil)
		if _, _, err := h.svc.StartCheckout(ctx, h.ownerID, h.orgID, submodel.PlanPro, "s", "c"); err != service.ErrBillingUnavailable {
			t.Fatalf("want ErrBillingUnavailable, got %v", err)
		}
	})
	t.Run("free plan rejected", func(t *testing.T) {
		h := newSubHarness(t)
		if _, _, err := h.svc.StartCheckout(ctx, h.ownerID, h.orgID, submodel.PlanFree, "s", "c"); err != service.ErrInvalidPlan {
			t.Fatalf("want ErrInvalidPlan, got %v", err)
		}
	})
	t.Run("non-owner forbidden", func(t *testing.T) {
		h := newSubHarness(t)
		if _, _, err := h.svc.StartCheckout(ctx, uuid.New(), h.orgID, submodel.PlanPro, "s", "c"); err != service.ErrForbidden {
			t.Fatalf("want ErrForbidden, got %v", err)
		}
	})
	t.Run("already subscribed", func(t *testing.T) {
		h := newSubHarness(t)
		seedSub(t, h, submodel.PlanPro, submodel.StatusActive, nil)
		if _, _, err := h.svc.StartCheckout(ctx, h.ownerID, h.orgID, submodel.PlanPro, "s", "c"); err != service.ErrAlreadySubscribed {
			t.Fatalf("want ErrAlreadySubscribed, got %v", err)
		}
	})
	t.Run("happy path writes nothing", func(t *testing.T) {
		h := newSubHarness(t)
		url, sess, err := h.svc.StartCheckout(ctx, h.ownerID, h.orgID, submodel.PlanScale, "s", "c")
		if err != nil {
			t.Fatalf("checkout: %v", err)
		}
		if url == "" || sess == "" {
			t.Fatalf("want session handle, got %q %q", url, sess)
		}
		if h.billing.lastCheckout.Plan != submodel.PlanScale || h.billing.lastCheckout.OrgID != h.orgID {
			t.Fatalf("bad provider args: %+v", h.billing.lastCheckout)
		}
		if row, _ := h.repo.GetByOrg(h.orgID); row != nil {
			t.Fatal("checkout start must not write paid state (webhook confirms)")
		}
	})
}

func TestPortalGuards(t *testing.T) {
	ctx := context.Background()
	h := newSubHarness(t)
	if _, err := h.svc.CreatePortalSession(ctx, h.ownerID, h.orgID, "r"); err != service.ErrNoSubscription {
		t.Fatalf("no row: want ErrNoSubscription, got %v", err)
	}
	seedSub(t, h, submodel.PlanPro, submodel.StatusActive, nil)
	if _, err := h.svc.CreatePortalSession(ctx, uuid.New(), h.orgID, "r"); err != service.ErrForbidden {
		t.Fatalf("non-owner: want ErrForbidden, got %v", err)
	}
	url, err := h.svc.CreatePortalSession(ctx, h.ownerID, h.orgID, "r")
	if err != nil || url == "" {
		t.Fatalf("portal: %v %q", err, url)
	}
}

func stripeEvent(t *testing.T, typ string, obj any) *stripe.Event {
	t.Helper()
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &stripe.Event{ID: "evt_1", Type: stripe.EventType(typ), Data: &stripe.EventData{Raw: raw}}
}

func TestWebhookTransitions(t *testing.T) {
	newSession := func(mode, org, plan string) map[string]any {
		return map[string]any{
			"id":           "cs_1",
			"mode":         mode,
			"metadata":     map[string]string{"org_id": org, "plan": plan},
			"customer":     map[string]string{"id": "cus_1"},
			"subscription": map[string]string{"id": "sub_1"},
		}
	}

	t.Run("payment mode ignored by billing", func(t *testing.T) {
		h := newSubHarness(t)
		handled, err := h.svc.HandleBillingEvent(stripeEvent(t, "checkout.session.completed",
			newSession("payment", h.orgID.String(), "")))
		if err != nil || handled {
			t.Fatalf("payment session must fall through to orders: %v %v", handled, err)
		}
	})
	t.Run("subscription checkout activates", func(t *testing.T) {
		h := newSubHarness(t)
		handled, err := h.svc.HandleBillingEvent(stripeEvent(t, "checkout.session.completed",
			newSession("subscription", h.orgID.String(), "PRO")))
		if err != nil || !handled {
			t.Fatalf("want handled activation: %v %v", handled, err)
		}
		ent, _ := h.svc.EntitlementFor(h.orgID)
		if ent.Plan != submodel.PlanPro {
			t.Fatalf("want PRO, got %s", ent.Plan)
		}
	})
	t.Run("past due and renew cycle", func(t *testing.T) {
		h := newSubHarness(t)
		seedSub(t, h, submodel.PlanPro, submodel.StatusActive, nil)
		inv := map[string]any{"id": "in_1", "parent": map[string]any{
			"type":                 "subscription_details",
			"subscription_details": map[string]any{"subscription": "sub_1"},
		}}
		if _, err := h.svc.HandleBillingEvent(stripeEvent(t, "invoice.payment_failed", inv)); err != nil {
			t.Fatalf("failed: %v", err)
		}
		ent, _ := h.svc.EntitlementFor(h.orgID)
		if ent.Plan != submodel.PlanPro {
			t.Fatalf("grace must keep tier, got %s", ent.Plan)
		}
		row, _ := h.repo.GetByOrg(h.orgID)
		if row.Status != submodel.StatusPastDue || row.GraceUntil == nil {
			t.Fatalf("past due must stamp grace: %+v", row)
		}
		first := *row.GraceUntil
		if _, err := h.svc.HandleBillingEvent(stripeEvent(t, "invoice.payment_failed", inv)); err != nil {
			t.Fatalf("repeat failed: %v", err)
		}
		row, _ = h.repo.GetByOrg(h.orgID)
		if !row.GraceUntil.Equal(first) {
			t.Fatal("repeat failures must never extend grace")
		}
		if _, err := h.svc.HandleBillingEvent(stripeEvent(t, "invoice.payment_succeeded", inv)); err != nil {
			t.Fatalf("renew: %v", err)
		}
		row, _ = h.repo.GetByOrg(h.orgID)
		if row.Status != submodel.StatusActive || row.GraceUntil != nil {
			t.Fatalf("renew must clear grace: %+v", row)
		}
	})
	t.Run("update syncs cancel flag then delete drops", func(t *testing.T) {
		h := newSubHarness(t)
		seedSub(t, h, submodel.PlanScale, submodel.StatusActive, nil)
		sub := map[string]any{
			"id": "sub_1", "status": "active", "cancel_at_period_end": true,
			"metadata": map[string]string{"plan": "SCALE"},
			"items":    map[string]any{"data": []any{map[string]any{"current_period_end": 1893456000}}},
		}
		if _, err := h.svc.HandleBillingEvent(stripeEvent(t, "customer.subscription.updated", sub)); err != nil {
			t.Fatalf("update: %v", err)
		}
		row, _ := h.repo.GetByOrg(h.orgID)
		if !row.CancelAtPeriodEnd || row.CurrentPeriodEnd == nil {
			t.Fatalf("update must sync flag+period: %+v", row)
		}
		if _, err := h.svc.HandleBillingEvent(stripeEvent(t, "customer.subscription.deleted", sub)); err != nil {
			t.Fatalf("delete: %v", err)
		}
		ent, _ := h.svc.EntitlementFor(h.orgID)
		if ent.Plan != submodel.PlanFree {
			t.Fatalf("deleted must resolve Free, got %s", ent.Plan)
		}
	})
	t.Run("unknown rows ack silently", func(t *testing.T) {
		h := newSubHarness(t)
		inv := map[string]any{"id": "in_9", "parent": map[string]any{
			"type":                 "subscription_details",
			"subscription_details": map[string]any{"subscription": "sub_unknown"},
		}}
		if _, err := h.svc.HandleBillingEvent(stripeEvent(t, "invoice.payment_succeeded", inv)); err != nil {
			t.Fatalf("unknown must ack: %v", err)
		}
		if _, err := h.svc.HandleBillingEvent(&stripe.Event{ID: "e", Type: "nope.unknown"}); err != nil {
			t.Fatalf("unknown type must ack: %v", err)
		}
	})
}
