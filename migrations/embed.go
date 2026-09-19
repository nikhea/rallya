// Package migrations embeds the versioned SQL schema files so the
// migrate runner (cmd/migrate) and release jobs can apply them without
// a volume mount. These files are the single source of truth for the
// Postgres schema; GORM AutoMigrate is never used outside tests.
package migrations

import "embed"

// FS holds *.up.sql / *.down.sql, applied via golang-migrate iofs source.
//
// Naming: {version}_{name}.up.sql, e.g. 000001_auth.up.sql.
// Never edit an applied migration; add a new version instead.
var (
	//go:embed *.sql
	FS embed.FS
)
