package model

// AllModels returns every Events domain model for AutoMigrate / tests.
func AllModels() []any {
	return []any{
		&Event{},
	}
}
