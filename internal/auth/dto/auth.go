package dto

import "github.com/google/uuid"

// Register is POST /api/v1/auth/register.
type Register struct {
	Email     string  `json:"email" binding:"required,email,max=255" example:"john@test.com"`
	Password  string  `json:"password" binding:"required,min=8,max=128" example:"Str0ngP@ssw0rd!"`
	FirstName *string `json:"firstName" binding:"omitempty,max=100" example:"John"`
	LastName  *string `json:"lastName" binding:"omitempty,max=100" example:"Doe"`
}

// Login is POST /api/v1/auth/login.
type Login struct {
	Email    string `json:"email" binding:"required,email,max=255" example:"john@test.com"`
	Password string `json:"password" binding:"required,min=1,max=128" example:"Str0ngP@ssw0rd!"`
}

// Refresh is POST /api/v1/auth/refresh (JSON body transport).
type Refresh struct {
	RefreshToken string `json:"refreshToken" binding:"required,min=1" example:"a3f1c9e2b4d60718293a4b5c6d7e8f09a1b2c3d4e5f60718293a4b5c6d7e8f0"`
}

// VerifyEmail is POST /api/v1/auth/verify-email.
type VerifyEmail struct {
	Token string `json:"token" binding:"required,min=1" example:"b7e2d1c0a9f84620c3d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3"`
}

// VerifyByCode is POST /api/v1/auth/verify-code (6-digit OTP).
type VerifyByCode struct {
	Email string `json:"email" binding:"required,email,max=255" example:"john@test.com"`
	Code  string `json:"code" binding:"required,len=6" example:"482910"`
}

// ResendVerification is POST /api/v1/auth/resend-verification.
type ResendVerification struct {
	Email string `json:"email" binding:"required,email,max=255" example:"john@test.com"`
}

// ForgotPassword is POST /api/v1/auth/forgot-password.
// Always returns 200 to avoid account enumeration.
type ForgotPassword struct {
	Email string `json:"email" binding:"required,email,max=255" example:"john@test.com"`
}

// ResetPassword is POST /api/v1/auth/reset-password.
type ResetPassword struct {
	Token       string `json:"token" binding:"required,min=1" example:"c8f3e2d1b0a94731d4e5f6a708192b3c4d5e6f708192a3b4c5d6e7f8091b2"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=128" example:"N3wStr0ngP@ss!"`
}

// TokenPair is the login/refresh response shape.
type TokenPair struct {
	AccessToken  string `json:"accessToken" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxYTIzYiJ9.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"`
	RefreshToken string `json:"refreshToken" example:"a3f1c9e2b4d60718293a4b5c6d7e8f09a1b2c3d4e5f60718293a4b5c6d7e8f0"`
}

// Profile is the public profile subset in Me responses.
type Profile struct {
	FirstName *string `json:"firstName,omitempty" example:"John"`
	LastName  *string `json:"lastName,omitempty" example:"Doe"`
}

// OrgMembership is the read shape organization exposes per membership.
// Defined here (leaf package) so both auth handlers and the organization
// domain can use it without import cycles.
type OrgMembership struct {
	ID       string `json:"id" example:"2b3c4d5e-6f70-8a9b-0c1d-2e3f4a5b6c7d"`
	Slug     string `json:"slug" example:"acme"`
	Name     string `json:"name" example:"Acme Inc"`
	Role     string `json:"role" example:"ADMIN"`
	JoinedAt string `json:"joinedAt,omitempty" example:"2026-09-19T12:00:00Z"`
}

// MembershipLister is implemented by the organization domain so auth can
// embed org context (GET /auth/me). Nil means "no org integration".
type MembershipLister interface {
	ListMemberships(userID uuid.UUID) ([]OrgMembership, error)
}

// Me is GET /api/v1/auth/me.
type Me struct {
	ID            string  `json:"id" example:"1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"`
	Email         string  `json:"email" example:"john@test.com"`
	EmailVerified bool    `json:"emailVerified" example:"true"`
	Profile       Profile `json:"profile"`
	// Organizations is omitted when no membership integration is wired.
	Organizations []OrgMembership `json:"organizations,omitempty"`
}

// Message is a generic ack response.
type Message struct {
	Message string `json:"message" example:"Verification email sent"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error" example:"invalid email or password"`
}

// EmailNotVerifiedResponse is returned when login is blocked pending verification.
type EmailNotVerifiedResponse struct {
	Error string `json:"error" example:"email not verified"`
	Code  string `json:"code" example:"EMAIL_NOT_VERIFIED"`
}
