package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/cmd/config"
	"github.com/nikhea/rallya/internal/auth/dto"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/notification/jobs"
)

const (
	bcryptCost                 = 12
	emailVerificationTTL       = 24 * time.Hour
	otpTTL                     = 10 * time.Minute
	passwordResetTTL           = time.Hour
	resendVerificationThrottle = 60 * time.Second
)

// LoginContext carries request metadata for sessions/attempts.
type LoginContext struct {
	IPAddress  string
	UserAgent  string
	DeviceName string
}

// AuthService orchestrates auth flows over AuthRepository.
// Email delivery goes through the jobs Enqueuer (River); welcome mail
// is enqueued post-commit, verification/reset mail transactionally.
type AuthService struct {
	repo     *repository.AuthRepository
	enqueuer jobs.Enqueuer
}

// NewAuthService builds the service.
func NewAuthService(repo *repository.AuthRepository) *AuthService {
	return &AuthService{repo: repo}
}

// SetEnqueuer wires job insertion (River in production, fake in tests).
// A nil enqueuer skips email delivery (tests without mail assertions).
func (s *AuthService) SetEnqueuer(e jobs.Enqueuer) {
	s.enqueuer = e
}

// GetUserByID satisfies the UserReader cross-domain contract.
func (s *AuthService) GetUserByID(id uuid.UUID) (*model.User, error) {
	return s.repo.GetUserByID(id)
}

// ---------- register / verify ----------

// Register creates user + profile + credential + verification artifacts,
// then transactionally enqueues the verify email (OTP code + 24h link).
func (s *AuthService) Register(in dto.Register) error {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if _, err := s.repo.GetUserByEmail(email); err == nil {
		return ErrUserExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcryptCost)
	if err != nil {
		return err
	}
	raw, err := token.GenerateRawToken(32)
	if err != nil {
		return err
	}
	otp, err := generateOTP()
	if err != nil {
		return err
	}
	now := time.Now()
	user := &model.User{Email: email, Status: model.UserStatusActive}
	profile := &model.UserProfile{FirstName: in.FirstName, LastName: in.LastName}
	cred := &model.Credential{PasswordHash: string(hash), PasswordChangedAt: &now}
	verif := &model.EmailVerification{TokenHash: token.HashToken(raw), ExpiresAt: now.Add(emailVerificationTTL)}
	code := &model.EmailOTPCode{CodeHash: token.HashToken(otp), ExpiresAt: now.Add(otpTTL)}

	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateUser(tx, user); err != nil {
			return err
		}
		profile.UserID = user.ID
		if err := s.repo.CreateProfile(tx, profile); err != nil {
			return err
		}
		cred.UserID = user.ID
		if err := s.repo.CreateCredential(tx, cred); err != nil {
			return err
		}
		verif.UserID = user.ID
		if err := s.repo.CreateEmailVerification(tx, verif); err != nil {
			return err
		}
		code.UserID = user.ID
		if err := s.repo.CreateOTPCode(tx, code); err != nil {
			return err
		}
		return s.enqueueTx(tx, jobs.SendVerificationEmailArgs{
			UserID:     user.ID,
			Email:      email,
			Name:       displayName(email, in.FirstName),
			OTP:        otp,
			VerifyLink: verificationLink(raw),
		})
	}); err != nil {
		return err
	}

	return nil
}

// VerifyEmail confirms ownership via the raw emailed token,
// then enqueues the welcome email.
func (s *AuthService) VerifyEmail(raw string) error {
	now := time.Now()
	v, err := s.repo.GetEmailVerificationByHash(token.HashToken(raw))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidToken
		}
		return err
	}
	if v.VerifiedAt != nil {
		return ErrTokenUsed
	}
	if now.After(v.ExpiresAt) {
		return ErrInvalidToken
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.SetEmailVerified(tx, v.UserID); err != nil {
			return err
		}
		return s.repo.MarkEmailVerificationUsed(tx, v.ID, now)
	}); err != nil {
		return err
	}
	s.sendWelcome(v.UserID)
	return nil
}

// VerifyByCode confirms ownership via the 6-digit emailed OTP.
// Wrong guesses increment attempts (max OTPMaxAttempts); expired or
// exhausted codes behave as invalid without revealing which.
func (s *AuthService) VerifyByCode(email, code string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.repo.GetUserByEmail(email)
	if err != nil {
		return ErrInvalidToken // enumeration-safe
	}
	if u.EmailVerified {
		return nil // idempotent: already verified
	}
	now := time.Now()
	c, err := s.repo.LatestPendingOTP(u.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidToken
		}
		return err
	}
	if now.After(c.ExpiresAt) || c.Attempts >= model.OTPMaxAttempts {
		return ErrInvalidToken
	}
	if token.HashToken(strings.TrimSpace(code)) != c.CodeHash {
		_ = s.repo.IncrementOTPAttempts(nil, c.ID)
		if c.Attempts+1 >= model.OTPMaxAttempts {
			return ErrOTPAttemptsExceeded
		}
		return ErrInvalidToken
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.SetEmailVerified(tx, u.ID); err != nil {
			return err
		}
		if err := s.repo.ConsumeOTP(tx, c.ID, now); err != nil {
			return err
		}
		return s.repo.InvalidateUserEmailVerifications(tx, u.ID, now)
	}); err != nil {
		return err
	}
	s.sendWelcome(u.ID)
	return nil
}

// ResendVerification invalidates pending tokens/codes and issues fresh ones.
func (s *AuthService) ResendVerification(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.repo.GetUserByEmail(email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // enumeration-safe: pretend success
		}
		return err
	}
	if u.EmailVerified {
		return nil
	}
	if at, err := s.repo.LatestEmailVerificationAt(u.ID); err == nil && at != nil {
		if time.Since(*at) < resendVerificationThrottle {
			return ErrTooManyRequests
		}
	}
	raw, err := token.GenerateRawToken(32)
	if err != nil {
		return err
	}
	otp, err := generateOTP()
	if err != nil {
		return err
	}
	now := time.Now()
	v := &model.EmailVerification{
		UserID: u.ID, TokenHash: token.HashToken(raw), ExpiresAt: now.Add(emailVerificationTTL),
	}
	code := &model.EmailOTPCode{
		UserID: u.ID, CodeHash: token.HashToken(otp), ExpiresAt: now.Add(otpTTL),
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.InvalidateUserEmailVerifications(tx, u.ID, now); err != nil {
			return err
		}
		if err := s.repo.InvalidateUserOTPs(tx, u.ID, now); err != nil {
			return err
		}
		if err := s.repo.CreateEmailVerification(tx, v); err != nil {
			return err
		}
		if err := s.repo.CreateOTPCode(tx, code); err != nil {
			return err
		}
		return s.enqueueTx(tx, jobs.SendVerificationEmailArgs{
			UserID:     u.ID,
			Email:      email,
			Name:       displayNameOf(u),
			OTP:        otp,
			VerifyLink: verificationLink(raw),
		})
	}); err != nil {
		return err
	}
	return nil
}

// ---------- login / refresh / logout / me ----------

// Login validates credentials, enforces status + verification, then mints
// a session + refresh pair and records the attempt.
func (s *AuthService) Login(in dto.Login, ctx LoginContext) (*dto.TokenPair, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	u, err := s.repo.GetUserByEmail(email)
	if err != nil {
		s.recordAttempt(email, nil, ctx.IPAddress, false, "user_not_found")
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	cred, err := s.repo.GetCredentialByUserID(u.ID)
	if err != nil {
		s.recordAttempt(email, &u.ID, ctx.IPAddress, false, "no_credential")
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(in.Password)); err != nil {
		s.recordAttempt(email, &u.ID, ctx.IPAddress, false, "bad_password")
		return nil, ErrInvalidCredentials
	}
	switch u.Status {
	case model.UserStatusSuspended:
		s.recordAttempt(email, &u.ID, ctx.IPAddress, false, "suspended")
		return nil, ErrAccountSuspended
	case model.UserStatusDeleted:
		s.recordAttempt(email, &u.ID, ctx.IPAddress, false, "deleted")
		return nil, ErrAccountDeleted
	case model.UserStatusInactive:
		s.recordAttempt(email, &u.ID, ctx.IPAddress, false, "inactive")
		return nil, ErrAccountInactive
	}
	if !u.EmailVerified {
		s.recordAttempt(email, &u.ID, ctx.IPAddress, false, "unverified")
		return nil, ErrEmailNotVerified
	}

	now := time.Now()
	sess := &model.Session{
		UserID: u.ID, LastActiveAt: &now,
		ExpiresAt: ptrTime(now.Add(config.RefreshTTL())),
	}
	if ctx.IPAddress != "" {
		sess.IPAddress = &ctx.IPAddress
	}
	if ctx.UserAgent != "" {
		sess.UserAgent = &ctx.UserAgent
	}
	if ctx.DeviceName != "" {
		sess.DeviceName = &ctx.DeviceName
	}
	raw, err := token.GenerateRawToken(32)
	if err != nil {
		return nil, err
	}
	rt := &model.RefreshToken{
		TokenHash: token.HashToken(raw), ExpiresAt: now.Add(config.RefreshTTL()),
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateSession(tx, sess); err != nil {
			return err
		}
		rt.SessionID = sess.ID
		if err := s.repo.CreateRefreshToken(tx, rt); err != nil {
			return err
		}
		return s.repo.UpdateLastLogin(tx, u.ID, now)
	}); err != nil {
		return nil, err
	}
	access, err := token.GenerateAccessToken(config.JWTSecret(), config.AccessTTL(), u.ID, sess.ID)
	if err != nil {
		return nil, err
	}
	s.recordAttempt(email, &u.ID, ctx.IPAddress, true, "")
	return &dto.TokenPair{AccessToken: access, RefreshToken: raw}, nil
}

// Refresh rotates a refresh token: revoke old, issue new pair.
// Reuse of a revoked token revokes the whole session (theft signal).
func (s *AuthService) Refresh(raw string) (*dto.TokenPair, error) {
	now := time.Now()
	old, err := s.repo.GetRefreshByHash(token.HashToken(raw))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if old.RevokedAt != nil {
		_ = s.repo.RevokeSession(nil, old.SessionID, now)
		_ = s.repo.RevokeSessionTokens(nil, old.SessionID, now)
		return nil, ErrInvalidToken
	}
	if now.After(old.ExpiresAt) {
		return nil, ErrInvalidToken
	}
	sess, err := s.repo.GetSessionByID(old.SessionID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if sess.RevokedAt != nil {
		return nil, ErrSessionRevoked
	}
	if sess.ExpiresAt != nil && now.After(*sess.ExpiresAt) {
		return nil, ErrInvalidToken
	}
	u, err := s.repo.GetUserByID(sess.UserID)
	if err != nil || u.Status != model.UserStatusActive {
		return nil, ErrInvalidToken
	}

	next, err := token.GenerateRawToken(32)
	if err != nil {
		return nil, err
	}
	rt := &model.RefreshToken{
		SessionID: sess.ID, TokenHash: token.HashToken(next), ExpiresAt: now.Add(config.RefreshTTL()),
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.RevokeRefreshToken(tx, old.ID, now); err != nil {
			return err
		}
		if err := s.repo.CreateRefreshToken(tx, rt); err != nil {
			return err
		}
		return s.repo.TouchSession(tx, sess.ID, now)
	}); err != nil {
		return nil, err
	}
	access, err := token.GenerateAccessToken(config.JWTSecret(), config.AccessTTL(), u.ID, sess.ID)
	if err != nil {
		return nil, err
	}
	return &dto.TokenPair{AccessToken: access, RefreshToken: next}, nil
}

// Logout revokes a session and its refresh rows (idempotent).
func (s *AuthService) Logout(sessionID uuid.UUID) error {
	now := time.Now()
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.RevokeSession(tx, sessionID, now); err != nil {
			return err
		}
		return s.repo.RevokeSessionTokens(tx, sessionID, now)
	})
}

// Me returns the public identity + profile.
func (s *AuthService) Me(userID uuid.UUID) (*dto.Me, error) {
	u, err := s.repo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	out := &dto.Me{ID: u.ID.String(), Email: u.Email, EmailVerified: u.EmailVerified}
	if u.Profile != nil {
		out.Profile.FirstName = u.Profile.FirstName
		out.Profile.LastName = u.Profile.LastName
	}
	return out, nil
}

// ---------- password reset ----------

// ForgotPassword is enumeration-safe: always nil unless DB fails. Existing
// users get prior rows invalidated plus a fresh 1h token, with the reset
// email enqueued in the same transaction.
func (s *AuthService) ForgotPassword(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.repo.GetUserByEmail(email)
	if err != nil {
		return nil
	}
	raw, err := token.GenerateRawToken(32)
	if err != nil {
		return err
	}
	now := time.Now()
	p := &model.PasswordReset{
		UserID: u.ID, TokenHash: token.HashToken(raw), ExpiresAt: now.Add(passwordResetTTL),
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.InvalidateUserPasswordResets(tx, u.ID, now); err != nil {
			return err
		}
		if err := s.repo.CreatePasswordReset(tx, p); err != nil {
			return err
		}
		return s.enqueueTx(tx, jobs.SendPasswordResetEmailArgs{
			UserID:    u.ID,
			Email:     email,
			Name:      displayNameOf(u),
			ResetLink: resetLink(raw),
		})
	}); err != nil {
		return err
	}
	return nil
}

// ResetPassword consumes a reset token, sets the new hash, and revokes
// every session + refresh token so the user re-logs in everywhere.
func (s *AuthService) ResetPassword(raw, newPassword string) error {
	now := time.Now()
	p, err := s.repo.GetPasswordResetByHash(token.HashToken(raw))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidToken
		}
		return err
	}
	if p.UsedAt != nil {
		return ErrTokenUsed
	}
	if now.After(p.ExpiresAt) {
		return ErrInvalidToken
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpdateCredentialHash(tx, p.UserID, string(hash), now); err != nil {
			return err
		}
		if err := s.repo.MarkPasswordResetUsed(tx, p.ID, now); err != nil {
			return err
		}
		if err := s.repo.InvalidateUserPasswordResets(tx, p.UserID, now); err != nil {
			return err
		}
		if err := s.repo.RevokeAllUserSessions(tx, p.UserID, now); err != nil {
			return err
		}
		return s.repo.RevokeAllUserTokens(tx, p.UserID, now)
	})
}

func (s *AuthService) recordAttempt(email string, userID *uuid.UUID, ip string, success bool, reason string) {
	a := &model.LoginAttempt{Success: success}
	if email != "" {
		a.Email = &email
	}
	if userID != nil {
		a.UserID = userID
	}
	if ip != "" {
		a.IPAddress = &ip
	}
	if reason != "" {
		a.FailureReason = &reason
	}
	s.repo.RecordLoginAttempt(a)
}

func ptrTime(t time.Time) *time.Time { return &t }

// ---------- email queue helpers ----------

// generateOTP returns a zero-padded 6-digit code from crypto/rand.
func generateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// displayName prefers the profile first name, else the email local part.
func displayName(email string, firstName *string) string {
	if firstName != nil && strings.TrimSpace(*firstName) != "" {
		return strings.TrimSpace(*firstName)
	}
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return email
}

// displayNameOf derives the greeting name from a loaded user (profile preloaded).
func displayNameOf(u *model.User) string {
	var first *string
	if u.Profile != nil {
		first = u.Profile.FirstName
	}
	return displayName(u.Email, first)
}

// enqueueTx inserts a job inside tx; skips silently without an enqueuer.
func (s *AuthService) enqueueTx(tx *gorm.DB, args river.JobArgs) error {
	if s.enqueuer == nil {
		return nil
	}
	return s.enqueuer.EnqueueTx(context.Background(), tx, args)
}

// sendWelcome enqueues the post-verification welcome email (best-effort:
// failures are logged, never fail the verification that already committed).
func (s *AuthService) sendWelcome(userID uuid.UUID) {
	if s.enqueuer == nil {
		return
	}
	u, err := s.repo.GetUserByID(userID)
	if err != nil {
		slog.Warn("welcome email skipped: user load failed", "error", err)
		return
	}
	err = s.enqueuer.Enqueue(context.Background(), jobs.SendWelcomeEmailArgs{
		UserID:    u.ID,
		Email:     u.Email,
		Name:      displayNameOf(u),
		LoginLink: config.AppURL(),
	})
	if err != nil {
		slog.Warn("welcome email enqueue failed", "error", err)
	}
}
