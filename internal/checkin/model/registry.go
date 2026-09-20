package model

// AllModels returns every Check-in domain model for AutoMigrate / tests.
func AllModels() []any {
	return []any{
		&CheckinLog{},
	}
}
