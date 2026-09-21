package model

// AllModels returns every Organization domain model for AutoMigrate / tests.
func AllModels() []any {
	return []any{
		&Organization{},
		&Membership{},
		&Invite{},
		&RoleDefinition{},
		&MemberCustomRole{},
	}
}
