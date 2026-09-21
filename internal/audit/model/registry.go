package model

// AllModels returns every Audit domain model for AutoMigrate / tests.
func AllModels() []any {
	return []any{
		&AuditEvent{},
	}
}
