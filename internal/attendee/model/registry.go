package model

// AllModels returns every Attendees domain model for AutoMigrate / tests.
func AllModels() []any {
	return []any{
		&Attendee{},
	}
}
