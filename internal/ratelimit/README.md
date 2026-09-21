# Rate limiting

Per-IP fixed-window limits as Gin middleware. Two tiers: `auth` (strict,
all `/auth/*` endpoints — login/register/refresh/verify/forgot/reset)
and `default` (generous, everything under `/api/v1`). `/health` and
`/swagger` never limit. Denials are `429` + `Retry-After` (seconds) in the
standard `{error}` envelope.

## Stores

- Redis (`NewRedisStore`) when the app connects — budgets shared across
  instances, atomic Lua `INCR`+`PEXPIRE`. Boot logs which store is active.
- Memory (`NewMemoryStore`) otherwise — single-instance only.

Fail-open: store errors allow the request (warn-logged). A cache outage
must never brick the platform; brute-force signal still lands in
`login_attempts` regardless.

## Tuning

`RATE_LIMIT_AUTH_PER_MIN` (default 10), `RATE_LIMIT_DEFAULT_PER_MIN`
(default 600), `TRUSTED_PROXIES` (comma CIDRs/IPs for `ClientIP` behind
load balancers; unset = `RemoteAddr` only).
