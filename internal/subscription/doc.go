// Package subscription implements organization subscription plans.
// Three tiers (FREE/PRO/SCALE) gate organizer-side quotas and features via
// Stripe Billing. Attendee order checkout stays one-off and never reads
// this domain.
package subscription
