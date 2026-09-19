package model

// AllModels returns every Ticketing domain model for AutoMigrate / tests.
func AllModels() []any {
	return []any{
		&TicketType{},
	}
}
