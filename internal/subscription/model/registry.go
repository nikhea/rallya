package model

// AllModels returns every Subscription domain model for test AutoMigrate.
func AllModels() []any {
	return []any{&OrgSubscription{}}
}
