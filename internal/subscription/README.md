# Subscription domain (`internal/subscription/`)

Organization tiers (FREE/PRO/SCALE) over Stripe Billing. Quotas and feature
flags gate organizer-side writes; attendee order checkout stays one-off and
never reads this domain.

## Tiers

| | Free $0 | Pro | Scale |
|---|---|---|---|
| Active events/org | 3 | 25 | 200 |
| Members/org | 5 | 25 | 200 |
| Attendees/event | 200 | 2,000 | 20,000 |
| Custom roles / API keys / kits | off | on | on (kits unlimited) |

Amounts resolve live from the configured Stripe Prices (`STRIPE_PRICE_PRO`,
`STRIPE_PRICE_SCALE`); unconfigured prices read zero and cannot be
purchased (503, checkout precedent).

## Flows

- **Catalog**: `GET /subscription/plans` (public) — tiers, limits, features.
- **Upgrade**: `POST /orgs/:id/subscription/checkout {plan}` (OWNER) →
  Stripe subscription-mode Checkout (idempotency-keyed per org+plan).
  Fulfillment lands via webhook; no row is written at checkout start.
- **Manage/cancel**: `POST /orgs/:id/subscription/portal` (OWNER) →
  Customer Portal; downgrades/cancels land at period end via webhook.
- **Webhooks** (existing `/webhooks/stripe` route, payment dispatcher
  forwards): `checkout.session.completed` (subscription mode only;
  payment mode still fulfills orders) → ACTIVE; `invoice.payment_succeeded`
  → ACTIVE + grace cleared; `invoice.payment_failed` → PAST_DUE + 7-day
  grace (first failure stamps, repeats never extend); `updated` → sync
  status/plan/period/cancel flag; `deleted` → CANCELED (sent after period
  end for portal cancels).
- **Reads**: `GET /orgs/:id/subscription` (members) — absent row reads FREE.

## Rules

- Entitlement resolution is fail-closed: absent rows, canceled plans,
  unknown plans, and lapsed grace all resolve Free. Nil provider means
  billing is unconfigured (entitlements still work).
- Enforcement (organizer calls → `402 UPGRADE_REQUIRED`, code-carrying):
  event create (non-cancelled count), member add + invite accept, custom
  role define + API key mint (feature flags), kit define (flag + per-event
  count). Buyer flow → `409 EVENT_FULL` (fullness, never billing state).
  Existing usage is grandfathered — only new writes trip limits.
- Seams, not imports: consumers declare `EntitlementProvider` (types from
  `model` only); org implements `OwnerChecker`; payment consumes
  `BillingHandler`. No service-to-service imports.
- Audit taxonomy: `subscription.checkout_started/started/renewed/
  past_due/updated/canceled` (`ObjectSubscription`).
- GORM tags mirror `migrations/000022_subscriptions`.

## Tests

`go test ./internal/subscription/... -count=1` — entitlement matrix
(grace math), checkout/portal guards (owner/plan/provider), webhook
transitions (activate/renew/past-due/update/delete, unknown rows ack);
enforcement suites in each consuming domain; HTTP shapes (402/409/403/503,
public catalog).
