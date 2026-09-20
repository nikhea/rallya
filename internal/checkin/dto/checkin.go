// Package checkindto holds the Check-in domain wire shapes.
package checkindto

// ScanRequest scans one code: raw QR string or a roster-resolved attendee
// ID (QR-less fallback for lost codes / dead phones). Exactly one required.
type ScanRequest struct {
	Code       string `json:"code,omitempty" example:"<uuid>.<token>.<hmac>"`
	AttendeeID string `json:"attendeeId,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// BatchRequest scans up to MaxBatchSize QR codes (QR-only; manual batch
// makes no sense — IDs resolve one by one through roster search).
type BatchRequest struct {
	Codes []string `json:"codes" example:"<uuid>.<token>.<hmac>"`
}

// ScanResult is one scan outcome. Refusals return 200 with their outcome.
type ScanResult struct {
	Outcome     string  `json:"outcome" example:"CHECKED_IN"`
	Method      string  `json:"method" example:"qr"`
	AttendeeID  *string `json:"attendeeId,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
	CheckedInAt *string `json:"checkedInAt,omitempty" example:"2026-09-20T10:00:00Z"`
}

// BatchResult is the uniform per-item envelope (no fail-fast, no 207).
type BatchResult struct {
	Results []ScanResult `json:"results"`
}

// StatsResponse is the door-dashboard aggregate.
type StatsResponse struct {
	Registered int64 `json:"registered" example:"120"`
	CheckedIn  int64 `json:"checkedIn" example:"45"`
	Cancelled  int64 `json:"cancelled" example:"3"`
	Total      int64 `json:"total" example:"168"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"event not found"`
}

// ErrorAlias names ErrorResponse for swagger annotations.
type ErrorAlias = ErrorResponse
