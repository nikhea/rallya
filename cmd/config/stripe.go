package config

import (
	"os"
	"strings"
)

// StripeConfig carries payment credentials. All values come from the
// environment — never hardcode, log, or commit them. Prefer restricted
// keys (rk_) over full secret keys where the dashboard allows.
type StripeConfig struct {
	// SecretKey authenticates server-side API calls (sk_test_... / sk_live_...).
	SecretKey string
	// WebhookSecret verifies event signatures (whsec_...).
	WebhookSecret string
	// PublishableKey is exposed to frontends (pk_test_...); unused server-side.
	PublishableKey string
	// PricePro/PriceScale are recurring Stripe Price IDs for org plans
	// (price_...). Empty = that tier cannot be purchased (503).
	PricePro   string
	PriceScale string
}

// StripeConfigFromEnv resolves Stripe settings.
func StripeConfigFromEnv() StripeConfig {
	return StripeConfig{
		SecretKey:      os.Getenv("STRIPE_SECRET_KEY"),
		WebhookSecret:  os.Getenv("STRIPE_WEBHOOKS_SIGNING_SECRET"),
		PublishableKey: os.Getenv("NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY"),
		PricePro:       os.Getenv("STRIPE_PRICE_PRO"),
		PriceScale:     os.Getenv("STRIPE_PRICE_SCALE"),
	}
}

// StripeEnabled reports whether server-side Stripe calls are possible.
// Webhook verification additionally needs WebhookSecret (checked at use).
func StripeEnabled() bool {
	return strings.TrimSpace(StripeConfigFromEnv().SecretKey) != ""
}
