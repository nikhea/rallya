# AGENTS.md — `cmd/` (entrypoints)

Thin wiring binaries only — no domain logic lives here.

## Rules

- `cmd/api/main.go`: `LoadEnv → InitLogger → ConnectDatabase → ConnectRedis`
  → build River client → construct repo/service/handler per domain → routes →
  serve with graceful shutdown (River `Stop` + HTTP `Shutdown` on SIGTERM).
  Never migrate here — `make migrate-up` (`cmd/migrate`) is the release step
  that must succeed first, dev and prod alike.
- `cmd/migrate/main.go`: golang-migrate (app tables) then `rivermigrate`
  (queue tables) in one command; `version` reports both engines. Dirty DB:
  fix manually, then `force`.
- `cmd/config/`: env-driven handles only. DB failure is fatal; Redis and the
  River listener pool degrade gracefully (nil = polling/disabled). JWT secret
  fail-closes (32+ bytes). `SUPERADMIN_EMAILS` comma-separated, lowercased.
- New domain = new block in `main.go` (repo → service → setters → handler →
  routes) + enforcer/membership wiring where the domain declares seams.
