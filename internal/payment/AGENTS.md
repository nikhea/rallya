# AGENTS.md — Payments domain (`internal/payment/`)

Stripe Checkout + webhooks for priced orders. Free orders never enter.
Fulfillment runs ONLY in the webhook handler.

## Rules

- Checkout Sessions API, payment mode, dynamic methods (never pass
  `payment_method_types`); `integration_identifier` on create; idempotency
  key per order. `StripeClient` instance — global key pattern is banned.
- Webhook: verify signature FIRST (raw body + header), then dispatch.
  Fulfill only `payment_status == paid` across `completed` +
  `async_payment_succeeded`; `async_payment_failed` and unknown types ack
  silently. Bad signature 400, handled outcomes 200 (no retry storms).
- `MarkPaid` idempotent (redelivery-safe); wrong states rejected. Session
  recorded at creation; priced-only gate before any Stripe call.
- Seams: `OrderStore` + `CheckoutProvider` interfaces (fakes in tests).
  Payments never touches order tables; never log secrets.
- Test webhook payloads must carry the SDK-pinned `api_version`
  (`2026-08-26.dahlia`) or `ConstructEvent` rejects them — real Stripe
  events always include it.

## Tests

`go test ./internal/payment/... -count=1` — checkout guards, locally-signed
webhook vectors (bad sig, unpaid defer, fulfill, redelivery, unknown order,
async-failed), HTTP shapes incl. 503 when unconfigured.
