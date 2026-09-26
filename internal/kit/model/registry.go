package model

// AllModels returns every Kit domain model for test AutoMigrate.
func AllModels() []any {
	return []any{&Kit{}, &KitCollection{}}
}
