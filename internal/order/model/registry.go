package model

// AllModels returns every Orders domain model for AutoMigrate / tests.
func AllModels() []any {
	return []any{
		&Order{},
	}
}
