package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikhea/rallya/cmd/config"
	"github.com/nikhea/rallya/internal/auth/dto"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
)

const (
	testEmail    = "john@test.com"
	testPassword = "Str0ngP@ssw0rd!"
)

func TestRegisterAndDuplicate(t *testing.T) {
	_, _, svc := testutil.Setup(t)

	if err := svc.Register(dto.Register{
		Email:     testEmail,
		Password:  testPassword,
		FirstName: testutil.StrPtr("John"),
		LastName:  testutil.StrPtr("Doe"),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := svc.Register(dto.Register{Email: testEmail, Password: testPassword}); !errors.Is(err, service.ErrUserExists) {
		t.Fatalf("expected ErrUserExists, got %v", err)
	}
	// Case-insensitive duplicate.
	if err := svc.Register(dto.Register{Email: "JOHN@test.com", Password: testPassword}); !errors.Is(err, service.ErrUserExists) {
		t.Fatalf("expected ErrUserExists (case-insensitive), got %v", err)
	}
}

func TestLoginBlockedUntilVerified(t *testing.T) {
	_, _, svc := testutil.Setup(t)
	mustRegister(t, svc)

	_, err := svc.Login(dto.Login{Email: testEmail, Password: testPassword}, dto.LoginContext{})
	if !errors.Is(err, service.ErrEmailNotVerified) {
		t.Fatalf("expected ErrEmailNotVerified, got %v", err)
	}
}

func TestVerifyThenLoginAndMe(t *testing.T) {
	_, repo, svc := testutil.Setup(t)
	mustRegister(t, svc)

	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	raw := "verify-raw-token-001"
	mustSeedVerification(t, repo, u.ID, raw)

	if err := svc.VerifyEmail(raw); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// Reuse must fail.
	if err := svc.VerifyEmail(raw); !errors.Is(err, service.ErrTokenUsed) {
		t.Fatalf("expected ErrTokenUsed, got %v", err)
	}
	if err := svc.VerifyEmail("bogus-token"); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}

	pair, err := svc.Login(dto.Login{Email: testEmail, Password: testPassword}, dto.LoginContext{
		IPAddress: "127.0.0.1", UserAgent: "test", DeviceName: "laptop",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected token pair")
	}

	me, err := svc.Me(mustUserID(t, pair.AccessToken))
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if me.Email != testEmail || !me.EmailVerified {
		t.Fatalf("unexpected me: %+v", me)
	}
	if me.Profile.FirstName == nil || *me.Profile.FirstName != "John" {
		t.Fatalf("expected profile first name, got %+v", me.Profile)
	}

	// Wrong password.
	if _, err := svc.Login(dto.Login{Email: testEmail, Password: "wrong"}, dto.LoginContext{}); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	// Unknown email.
	if _, err := svc.Login(dto.Login{Email: "nobody@test.com", Password: "x"}, dto.LoginContext{}); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	_, repo, svc := testutil.Setup(t)
	pair := mustVerifiedLogin(t, repo, svc)

	next, err := svc.Refresh(pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if next.RefreshToken == pair.RefreshToken {
		t.Fatal("expected rotated refresh token")
	}

	// Old token reuse -> rejected, session revoked (theft signal).
	if _, err := svc.Refresh(pair.RefreshToken); err == nil {
		t.Fatal("expected error on reused refresh token")
	}
	// Even the fresh token is now dead because the session was revoked.
	if _, err := svc.Refresh(next.RefreshToken); err == nil {
		t.Fatal("expected error after session revocation")
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	_, repo, svc := testutil.Setup(t)
	pair := mustVerifiedLogin(t, repo, svc)

	_, sessionID := mustParse(t, pair.AccessToken)
	if err := svc.Logout(sessionID); err != nil {
		t.Fatalf("logout: %v", err)
	}
	// Idempotent second logout.
	if err := svc.Logout(sessionID); err != nil {
		t.Fatalf("second logout: %v", err)
	}
	// Refresh after logout fails.
	if _, err := svc.Refresh(pair.RefreshToken); err == nil {
		t.Fatal("expected refresh to fail after logout")
	}
}

func TestResendVerificationThrottle(t *testing.T) {
	_, _, svc := testutil.Setup(t)
	mustRegister(t, svc)

	// Register just created a verification row -> immediate resend throttled.
	if err := svc.ResendVerification(testEmail); !errors.Is(err, service.ErrTooManyRequests) {
		t.Fatalf("expected ErrTooManyRequests, got %v", err)
	}
	// Unknown email stays silent (nil) to avoid enumeration.
	if err := svc.ResendVerification("ghost@test.com"); err != nil {
		t.Fatalf("expected nil for unknown email, got %v", err)
	}
}

func TestForgotPasswordEnumerationSafe(t *testing.T) {
	_, _, svc := testutil.Setup(t)
	mustRegister(t, svc)

	if err := svc.ForgotPassword("ghost@test.com"); err != nil {
		t.Fatalf("expected nil for unknown email, got %v", err)
	}
	if err := svc.ForgotPassword(testEmail); err != nil {
		t.Fatalf("forgot: %v", err)
	}
}

func TestResetPasswordInvalidatesSessions(t *testing.T) {
	_, repo, svc := testutil.Setup(t)
	pair := mustVerifiedLogin(t, repo, svc)

	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	raw := "reset-raw-token-001"
	if err := repo.CreatePasswordReset(nil, &model.PasswordReset{
		UserID:    u.ID,
		TokenHash: token.HashToken(raw),
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed reset: %v", err)
	}

	if err := svc.ResetPassword(raw, "N3wStr0ngP@ss!"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	// Token reuse fails.
	if err := svc.ResetPassword(raw, "Another1!x"); !errors.Is(err, service.ErrTokenUsed) {
		t.Fatalf("expected ErrTokenUsed, got %v", err)
	}
	// Old session dead.
	if _, err := svc.Refresh(pair.RefreshToken); err == nil {
		t.Fatal("expected refresh to fail after reset")
	}
	// Old password dead, new password works.
	if _, err := svc.Login(dto.Login{Email: testEmail, Password: testPassword}, dto.LoginContext{}); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for old password, got %v", err)
	}
	pair2, err := svc.Login(dto.Login{Email: testEmail, Password: "N3wStr0ngP@ss!"}, dto.LoginContext{})
	if err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if pair2.AccessToken == "" {
		t.Fatal("expected access token")
	}
}

func TestSuspendedUserBlocked(t *testing.T) {
	db, repo, svc := testutil.Setup(t)
	mustVerifiedLogin(t, repo, svc)

	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", u.ID).Update("status", model.UserStatusSuspended).Error; err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if _, err := svc.Login(dto.Login{Email: testEmail, Password: testPassword}, dto.LoginContext{}); !errors.Is(err, service.ErrAccountSuspended) {
		t.Fatalf("expected ErrAccountSuspended, got %v", err)
	}
}

// ---------- helpers ----------

// mustRegister registers the standard test user.
func mustRegister(t *testing.T, svc *service.AuthService) {
	t.Helper()
	if err := svc.Register(dto.Register{
		Email:     testEmail,
		Password:  testPassword,
		FirstName: testutil.StrPtr("John"),
		LastName:  testutil.StrPtr("Doe"),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
}

// mustSeedVerification mints a verification row with a known raw token.
// Register only delivers its raw token by email, so tests seed their own.
func mustSeedVerification(t *testing.T, repo *repository.AuthRepository, userID uuid.UUID, raw string) {
	t.Helper()
	if err := repo.CreateEmailVerification(nil, &model.EmailVerification{
		UserID:    userID,
		TokenHash: token.HashToken(raw),
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed verification: %v", err)
	}
}

// mustVerifiedLogin registers, verifies, and logs in, returning the pair.
func mustVerifiedLogin(t *testing.T, repo *repository.AuthRepository, svc *service.AuthService) *dto.TokenPair {
	t.Helper()
	mustRegister(t, svc)
	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	raw := "verify-raw-token-login"
	mustSeedVerification(t, repo, u.ID, raw)
	if err := svc.VerifyEmail(raw); err != nil {
		t.Fatalf("verify: %v", err)
	}
	pair, err := svc.Login(dto.Login{Email: testEmail, Password: testPassword}, dto.LoginContext{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return pair
}

func mustParse(t *testing.T, access string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	userID, sessionID, err := token.ParseAccessToken(config.JWTSecret(), access)
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	return userID, sessionID
}

func mustUserID(t *testing.T, access string) uuid.UUID {
	t.Helper()
	userID, _ := mustParse(t, access)
	return userID
}
