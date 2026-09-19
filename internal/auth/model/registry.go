package model

// AllModels returns every Auth domain model in dependency order for
// AutoMigrate / seeding. Order matters for FK creation.
func AllModels() []any {
	return []any{
		&User{},
		&UserProfile{},
		&Credential{},
		&Session{},
		&RefreshToken{},
		&EmailVerification{},
		&EmailOTPCode{},
		&PasswordReset{},
		&OAuthAccount{},
		&MFAFactor{},
		&LoginAttempt{},
	}
}
