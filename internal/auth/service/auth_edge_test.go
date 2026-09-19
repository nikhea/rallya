package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	auth "github.com/nikhea/rallya/internal/auth"
	"github.com/nikhea/rallya/internal/auth/dto"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
)

// AuthService must always satisfy the cross-domain UserReader contract.
var _ auth.UserReader = (*service.AuthService)(nil)

func TestResendVerificationSuccess(t *testing.T) {
	db, repo, svc, fake := testutil.SetupWithEnqueuer(t)
	mustRegister(t, svc)
	first := lastVerificationOTP(t, fake).OTP

	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	// Backdate past the 60s throttle.
	if err := db.Model(&model.EmailVerification{}).Where("user_id = ?", u.ID).
		Update("created_at", time.Now().Add(-2*time.Minute)).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if err := svc.ResendVerification(testEmail); err != nil {
		t.Fatalf("resend: %v", err)
	}
	second := lastVerificationOTP(t, fake).OTP
	if second == first {
		t.Fatal("expected rotated OTP")
	}
	// Old OTP invalidated.
	if err := svc.VerifyByCode(testEmail, first); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
	if err := svc.VerifyByCode(testEmail, second); err != nil {
		t.Fatalf("verify new: %v", err)
	}
}

func TestVerifyEmailExpired(t *testing.T) {
	_, repo, svc := testutil.Setup(t)
	mustRegister(t, svc)
	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	raw := "expired-link-token"
	if err := repo.CreateEmailVerification(nil, &model.EmailVerification{
		UserID: u.ID, TokenHash: token.HashToken(raw),
		ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.VerifyEmail(raw); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestMeUnknownUser(t *testing.T) {
	_, _, svc := testutil.Setup(t)
	if _, err := svc.Me(uuid.New()); err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestRefreshRevokedAndExpiredSession(t *testing.T) {
	db, repo, svc := testutil.Setup(t)
	pair := mustVerifiedLogin(t, repo, svc)

	uid, sid := mustParse(t, pair.AccessToken)
	_ = uid
	// Revoke only the session (refresh row still active).
	if err := repo.RevokeSession(nil, sid, time.Now()); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := svc.Refresh(pair.RefreshToken); !errors.Is(err, service.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked, got %v", err)
	}

	// Fresh login, then expire the refresh row itself.
	pair2 := mustLoginAgain(t, svc)
	rt, err := repo.GetRefreshByHash(token.HashToken(pair2.RefreshToken))
	if err != nil {
		t.Fatalf("load refresh: %v", err)
	}
	if err := db.Model(&model.RefreshToken{}).Where("id = ?", rt.ID).
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire refresh: %v", err)
	}
	if _, err := svc.Refresh(pair2.RefreshToken); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestLoginInactiveAndDeleted(t *testing.T) {
	db, repo, svc := testutil.Setup(t)
	mustVerifiedLogin(t, repo, svc)
	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	for status, want := range map[model.UserStatus]error{
		model.UserStatusInactive: service.ErrAccountInactive,
		model.UserStatusDeleted:  service.ErrAccountDeleted,
	} {
		if err := db.Model(&model.User{}).Where("id = ?", u.ID).
			Update("status", status).Error; err != nil {
			t.Fatalf("set status: %v", err)
		}
		if _, err := svc.Login(dto.Login{Email: testEmail, Password: testPassword}, service.LoginContext{}); !errors.Is(err, want) {
			t.Fatalf("status %s: expected %v, got %v", status, want, err)
		}
	}
}

func TestUserReaderDelegates(t *testing.T) {
	_, _, svc := testutil.Setup(t)
	mustRegister(t, svc)
	byEmail, err := svc.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("by email: %v", err)
	}
	byID, err := svc.GetUserByID(byEmail.ID)
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if byID.Email != testEmail {
		t.Fatalf("unexpected user: %+v", byID)
	}
}

// mustLoginAgain logs in an already-verified user.
func mustLoginAgain(t *testing.T, svc *service.AuthService) *dto.TokenPair {
	t.Helper()
	pair, err := svc.Login(dto.Login{Email: testEmail, Password: testPassword}, service.LoginContext{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return pair
}
