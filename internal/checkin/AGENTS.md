# AGENTS.md — Check-in domain (`internal/checkin/`)

Door scans over the attendee seam. Refusals are data (200 + outcome),
never errors.

## Rules

- Flip via `CheckinApplier` seam (`ApplyCheckin`/`ApplyCheckinManual`,
  `CountByStatus`) — never touch the attendees table here. Verdicts carry
  business refusals; only unknown rows error (`gorm.ErrRecordNotFound`).
- QR: `VerifyPayload` with the boot-injected secret (`SetQRSecret`);
  unset secret fails the scan (`ErrQRSecretUnset` → 503), never os.Exit.
- Outcomes: single stealth `INVALID_CODE` bucket; genuine-ticket wrong-door
  reports `WRONG_EVENT`. Rescans never move `checked_in_at`.
- Batch cap 50 (`MaxBatchSize` → 413); uniform 200 envelope, no fail-fast.
- New Casbin objects need matrix rows + backfill migration (`000016` pattern).
- DTOs live in `dto/`; service stays wire-shape free.

## Tests

`go test ./internal/checkin/... -count=1` — outcome matrix, batch mix/cap,
manual fallback, stats, concurrent double-scan convergence; HTTP suite on
the full stack (permission gates + stealth shapes).
