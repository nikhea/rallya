package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/notification/jobs"
)

func lastVerificationOTP(t *testing.T, fake *jobs.FakeEnqueuer) jobs.SendVerificationEmailArgs {
	t.Helper()
	got := fake.OfKind("send_verification_email")
	if len(got) == 0 {
		t.Fatal("expected a verification email job")
	}
	args, ok := got[len(got)-1].(jobs.SendVerificationEmailArgs)
	if !ok {
		t.Fatalf("unexpected args type %T", got[len(got)-1])
	}
	if len(args.OTP) != 6 || args.VerifyLink == "" || args.Email != testEmail {
		t.Fatalf("unexpected verification args: %+v", args)
	}
	return args
}

func TestRegisterEnqueuesVerificationWithOTP(t *testing.T) {
	_, repo, svc, fake := testutil.SetupWithEnqueuer(t)
	mustRegister(t, svc)

	args := lastVerificationOTP(t, fake)

	// OTP row pending in DB with matching hash.
	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	c, err := repo.LatestPendingOTP(u.ID)
	if err != nil {
		t.Fatalf("pending otp: %v", err)
	}
	if c.CodeHash != token.HashToken(args.OTP) {
		t.Fatal("otp row hash does not match enqueued code")
	}
}

func TestVerifyByCodeFlowAndWelcome(t *testing.T) {
	t.Setenv("APP_URL", "https://app.test.com")
	_, _, svc, fake := testutil.SetupWithEnqueuer(t)
	mustRegister(t, svc)
	otp := lastVerificationOTP(t, fake).OTP

	if err := svc.VerifyByCode(testEmail, otp); err != nil {
		t.Fatalf("verify by code: %v", err)
	}
	// Welcome enqueued post-verification.
	welcomes := fake.OfKind("send_welcome_email")
	if len(welcomes) != 1 {
		t.Fatalf("expected 1 welcome job, got %d", len(welcomes))
	}
	w, ok := welcomes[0].(jobs.SendWelcomeEmailArgs)
	if !ok || w.Email != testEmail || w.Name != "John" || w.LoginLink != "https://app.test.com" {
		t.Fatalf("unexpected welcome args: %+v", welcomes[0])
	}
	// Code single-use.
	if err := svc.VerifyByCode(testEmail, otp); err != nil {
		t.Fatalf("re-verify after success should be idempotent nil, got %v", err)
	}
}

func TestVerifyByCodeWrongAttemptsLock(t *testing.T) {
	_, _, svc, fake := testutil.SetupWithEnqueuer(t)
	mustRegister(t, svc)
	otp := lastVerificationOTP(t, fake).OTP

	for i := 0; i < 4; i++ {
		if err := svc.VerifyByCode(testEmail, "000000"); !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("attempt %d: expected ErrInvalidToken, got %v", i+1, err)
		}
	}
	if err := svc.VerifyByCode(testEmail, "000000"); !errors.Is(err, service.ErrOTPAttemptsExceeded) {
		t.Fatalf("attempt 5: expected ErrOTPAttemptsExceeded, got %v", err)
	}
	// Even the right code is dead now.
	if err := svc.VerifyByCode(testEmail, otp); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken after lock, got %v", err)
	}
}

func TestVerifyByCodeExpired(t *testing.T) {
	db, repo, svc, _ := testutil.SetupWithEnqueuer(t)
	mustRegister(t, svc)

	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	_ = db.Model(&model.EmailOTPCode{}).
		Where("user_id = ?", u.ID).
		Update("expires_at", time.Now().Add(-time.Minute)).Error

	if err := svc.VerifyByCode(testEmail, "123456"); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
	// Unknown email stays enumeration-safe.
	if err := svc.VerifyByCode("ghost@test.com", "123456"); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestWelcomeEnqueuedOnLinkVerify(t *testing.T) {
	_, repo, svc, fake := testutil.SetupWithEnqueuer(t)
	mustRegister(t, svc)

	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	raw := "welcome-link-token-001"
	mustSeedVerification(t, repo, u.ID, raw)
	if err := svc.VerifyEmail(raw); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if n := len(fake.OfKind("send_welcome_email")); n != 1 {
		t.Fatalf("expected 1 welcome job, got %d", n)
	}
}

func TestForgotEnqueuesResetJob(t *testing.T) {
	_, _, svc, fake := testutil.SetupWithEnqueuer(t)
	mustRegister(t, svc)

	if err := svc.ForgotPassword(testEmail); err != nil {
		t.Fatalf("forgot: %v", err)
	}
	got := fake.OfKind("send_password_reset_email")
	if len(got) != 1 {
		t.Fatalf("expected 1 reset job, got %d", len(got))
	}
	args, ok := got[0].(jobs.SendPasswordResetEmailArgs)
	if !ok || args.Email != testEmail || args.ResetLink == "" {
		t.Fatalf("unexpected reset args: %+v", got[0])
	}
}

func TestResendRotatesOTP(t *testing.T) {
	db, repo, svc, fake := testutil.SetupWithEnqueuer(t)
	mustRegister(t, svc)
	firstOTP := lastVerificationOTP(t, fake).OTP

	// Backdate past the 60s throttle.
	u, err := repo.GetUserByEmail(testEmail)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	_ = db.Model(&model.EmailVerification{}).
		Where("user_id = ?", u.ID).
		Update("created_at", time.Now().Add(-2*time.Minute)).Error

	if err := svc.ResendVerification(testEmail); err != nil {
		t.Fatalf("resend: %v", err)
	}
	secondOTP := lastVerificationOTP(t, fake).OTP
	if secondOTP == firstOTP {
		t.Fatal("expected rotated OTP")
	}
	// Old code invalidated.
	if err := svc.VerifyByCode(testEmail, firstOTP); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken for old code, got %v", err)
	}
	// New code works.
	if err := svc.VerifyByCode(testEmail, secondOTP); err != nil {
		t.Fatalf("verify new code: %v", err)
	}
}
