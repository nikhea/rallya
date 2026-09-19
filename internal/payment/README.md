# Payments Domain

Stripe Checkout for priced orders + signature-verified webhooks. Free orders
never touch this domain. Fulfillment runs **only** in the webhook handler —
never on success pages.

```
POST /orders/:id/checkout ──Stripe hosted page──▶ buyer pays
POST /webhooks/stripe (public, HMAC-verified)
  ├─ checkout.session.completed (paid) ──▶ MarkPaid → CONFIRMED
  ├─ checkout.session.async_payment_succeeded (paid) ──▶ MarkPaid
  └─ async_payment_failed / unknown ──▶ ack + log (no retry storm)
```

## Rules

- Checkout Sessions API in payment mode with **dynamic payment methods**
  (never `payment_method_types`); `integration_identifier` tags flows.
- `StripeClient` instance only — never the deprecated global key pattern.
- Provider behind `CheckoutProvider` interface (fakes in tests); nil provider
  fails closed with 503, boot never fails for missing keys.
- Idempotency-keyed session creation per order; replays mint fresh links but
  keep one stored session id. Free/non-payable states 400 before any Stripe call.
- Webhooks: raw body + `Stripe-Signature` verified first; bad signatures 400,
  handled/irrelevant events 200 (Stripe must not retry deliberate outcomes).
  Fulfill gated on `payment_status == paid` (async methods complete later).
- Seams only: `OrderStore` (`CheckoutDetail`, `MarkPaid`, `SetStripeSession`)
  implemented by orders; payments never touches order tables. Handlers see
  payment sentinels + order `ErrOrderNotFound` only.
- Secrets (`STRIPE_SECRET_KEY`, `STRIPE_WEBHOOKS_SIGNING_SECRET`) live in
  gitignored `.env`; restricted keys (`rk_`) preferred when the dashboard
  allows. Never logged.

## Testing

```bash
go test ./internal/payment/... -count=1
```

Fake provider + stub orders: checkout building, free/stranger/provider-down
guards, webhook verify/fulfill/defer/idempotent-redelivery/unknown-order with
locally HMAC-signed payloads (test vectors carry the pinned API version —
the SDK rejects version drift). Live: real test-mode sessions + signed
webhook POSTs (see payment smoke notes in session history).
