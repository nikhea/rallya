package repository

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/auth/model"
)

// AuthRepository is GORM persistence for the Auth domain.
// No business logic here; hashing, TTLs, and flows live in service.
type AuthRepository struct {
	db *gorm.DB
}

// NewAuthRepository wraps db for auth persistence.
func NewAuthRepository(db *gorm.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

// DB exposes the handle for service-level transactions.
func (r *AuthRepository) DB() *gorm.DB { return r.db }

// ---------- users ----------

// CreateUser inserts a user within an optional tx.
func (r *AuthRepository) CreateUser(db *gorm.DB, u *model.User) error {
	return dbOr(r, db).Create(u).Error
}

// GetUserByID loads a user with profile (implements UserReader).
func (r *AuthRepository) GetUserByID(id uuid.UUID) (*model.User, error) {
	var u model.User
	if err := r.db.Preload("Profile").First(&u, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByEmail loads a user with profile by email.
func (r *AuthRepository) GetUserByEmail(email string) (*model.User, error) {
	var u model.User
	if err := r.db.Preload("Profile").First(&u, "email = ?", email).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// SearchUsers finds users by email fragment, newest first (platform reads).
func (r *AuthRepository) SearchUsers(query string, limit, offset int) ([]model.User, int64, error) {
	var out []model.User
	var total int64
	q := r.db.Model(&model.User{}).Preload("Profile")
	if query != "" {
		like := "%" + strings.ToLower(query) + "%"
		q = q.Where("LOWER(email) LIKE ?", like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// SetEmailVerified flips the flag.
func (r *AuthRepository) SetEmailVerified(db *gorm.DB, userID uuid.UUID) error {
	return dbOr(r, db).Model(&model.User{}).
		Where("id = ?", userID).
		Update("email_verified", true).Error
}

// UpdateLastLogin stamps last_login_at.
func (r *AuthRepository) UpdateLastLogin(db *gorm.DB, userID uuid.UUID, t time.Time) error {
	return dbOr(r, db).Model(&model.User{}).
		Where("id = ?", userID).
		Update("last_login_at", t).Error
}

// ---------- profiles / credentials ----------

// CreateProfile inserts the display profile.
func (r *AuthRepository) CreateProfile(db *gorm.DB, p *model.UserProfile) error {
	return dbOr(r, db).Create(p).Error
}

// CreateCredential inserts the password hash row.
func (r *AuthRepository) CreateCredential(db *gorm.DB, c *model.Credential) error {
	return dbOr(r, db).Create(c).Error
}

// GetCredentialByUserID loads the hash row for comparison.
func (r *AuthRepository) GetCredentialByUserID(userID uuid.UUID) (*model.Credential, error) {
	var c model.Credential
	if err := r.db.First(&c, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateCredentialHash replaces the hash and stamps password_changed_at.
func (r *AuthRepository) UpdateCredentialHash(db *gorm.DB, userID uuid.UUID, hash string, at time.Time) error {
	return dbOr(r, db).Model(&model.Credential{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{"password_hash": hash, "password_changed_at": at}).Error
}

// ---------- sessions ----------

// CreateSession inserts a device session.
func (r *AuthRepository) CreateSession(db *gorm.DB, s *model.Session) error {
	return dbOr(r, db).Create(s).Error
}

// GetSessionByID loads a session by id.
func (r *AuthRepository) GetSessionByID(id uuid.UUID) (*model.Session, error) {
	var s model.Session
	if err := r.db.First(&s, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// TouchSession updates last_active_at.
func (r *AuthRepository) TouchSession(db *gorm.DB, id uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.Session{}).
		Where("id = ?", id).
		Update("last_active_at", at).Error
}

// RevokeSession soft-revokes one session.
func (r *AuthRepository) RevokeSession(db *gorm.DB, id uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.Session{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", at).Error
}

// RevokeAllUserSessions revokes every active session (password reset).
func (r *AuthRepository) RevokeAllUserSessions(db *gorm.DB, userID uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.Session{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", at).Error
}

// ---------- refresh tokens ----------

// CreateRefreshToken inserts a hashed refresh row.
func (r *AuthRepository) CreateRefreshToken(db *gorm.DB, t *model.RefreshToken) error {
	return dbOr(r, db).Create(t).Error
}

// GetRefreshByHash loads a refresh row by its hash.
func (r *AuthRepository) GetRefreshByHash(hash string) (*model.RefreshToken, error) {
	var t model.RefreshToken
	if err := r.db.First(&t, "token_hash = ?", hash).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// RevokeRefreshToken soft-revokes one refresh row.
func (r *AuthRepository) RevokeRefreshToken(db *gorm.DB, id uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.RefreshToken{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", at).Error
}

// RevokeSessionTokens revokes all active refresh rows for a session.
func (r *AuthRepository) RevokeSessionTokens(db *gorm.DB, sessionID uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.RefreshToken{}).
		Where("session_id = ? AND revoked_at IS NULL", sessionID).
		Update("revoked_at", at).Error
}

// RevokeAllUserTokens revokes refresh rows across the user's sessions.
func (r *AuthRepository) RevokeAllUserTokens(db *gorm.DB, userID uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.RefreshToken{}).
		Where("revoked_at IS NULL AND session_id IN (?)",
			dbOr(r, db).Model(&model.Session{}).Select("id").Where("user_id = ?", userID),
		).Update("revoked_at", at).Error
}

// ---------- api keys ----------

// CreateApiKey inserts a hashed org API key row.
func (r *AuthRepository) CreateApiKey(db *gorm.DB, k *model.ApiKey) error {
	return dbOr(r, db).Create(k).Error
}

// GetApiKeyByHash loads a key row by its SHA-256 hash.
func (r *AuthRepository) GetApiKeyByHash(hash string) (*model.ApiKey, error) {
	var k model.ApiKey
	if err := r.db.First(&k, "key_hash = ?", hash).Error; err != nil {
		return nil, err
	}
	return &k, nil
}

// GetApiKeyByID loads a key row by id.
func (r *AuthRepository) GetApiKeyByID(id uuid.UUID) (*model.ApiKey, error) {
	var k model.ApiKey
	if err := r.db.First(&k, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &k, nil
}

// ListApiKeysByOrg returns keys for an org, newest first (hashes excluded
// by the caller's DTO mapping; raw secrets are never stored).
func (r *AuthRepository) ListApiKeysByOrg(orgID uuid.UUID, limit, offset int) ([]model.ApiKey, int64, error) {
	var out []model.ApiKey
	var total int64
	q := r.db.Model(&model.ApiKey{}).Where("org_id = ?", orgID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// RevokeApiKey soft-revokes a key (idempotent; row kept for audit).
func (r *AuthRepository) RevokeApiKey(db *gorm.DB, id uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.ApiKey{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", at).Error
}

// TouchApiKeyLastUsed stamps last_used_at (best-effort; caller ignores errors).
func (r *AuthRepository) TouchApiKeyLastUsed(id uuid.UUID, at time.Time) error {
	return r.db.Model(&model.ApiKey{}).
		Where("id = ?", id).
		Update("last_used_at", at).Error
}

// ---------- email verifications ----------

// CreateEmailVerification inserts a verification row.
func (r *AuthRepository) CreateEmailVerification(db *gorm.DB, v *model.EmailVerification) error {
	return dbOr(r, db).Create(v).Error
}

// GetEmailVerificationByHash loads a pending verification by hash.
func (r *AuthRepository) GetEmailVerificationByHash(hash string) (*model.EmailVerification, error) {
	var v model.EmailVerification
	if err := r.db.First(&v, "token_hash = ?", hash).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

// MarkEmailVerificationUsed stamps verified_at.
func (r *AuthRepository) MarkEmailVerificationUsed(db *gorm.DB, id uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.EmailVerification{}).
		Where("id = ?", id).
		Update("verified_at", at).Error
}

// InvalidateUserEmailVerifications marks pending rows verified so they
// can't be reused (used on resend).
func (r *AuthRepository) InvalidateUserEmailVerifications(db *gorm.DB, userID uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.EmailVerification{}).
		Where("user_id = ? AND verified_at IS NULL", userID).
		Update("verified_at", at).Error
}

// LatestEmailVerificationAt returns the newest row time for throttling.
func (r *AuthRepository) LatestEmailVerificationAt(userID uuid.UUID) (*time.Time, error) {
	var v model.EmailVerification
	if err := r.db.Order("created_at DESC").First(&v, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &v.CreatedAt, nil
}

// ---------- password resets ----------

// CreatePasswordReset inserts a reset row.
func (r *AuthRepository) CreatePasswordReset(db *gorm.DB, p *model.PasswordReset) error {
	return dbOr(r, db).Create(p).Error
}

// GetPasswordResetByHash loads a reset row by hash.
func (r *AuthRepository) GetPasswordResetByHash(hash string) (*model.PasswordReset, error) {
	var p model.PasswordReset
	if err := r.db.First(&p, "token_hash = ?", hash).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// MarkPasswordResetUsed stamps used_at.
func (r *AuthRepository) MarkPasswordResetUsed(db *gorm.DB, id uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.PasswordReset{}).
		Where("id = ?", id).
		Update("used_at", at).Error
}

// InvalidateUserPasswordResets marks pending rows used (single-use chain).
func (r *AuthRepository) InvalidateUserPasswordResets(db *gorm.DB, userID uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.PasswordReset{}).
		Where("user_id = ? AND used_at IS NULL", userID).
		Update("used_at", at).Error
}

// ---------- email OTP codes ----------

// CreateOTPCode inserts a hashed OTP row.
func (r *AuthRepository) CreateOTPCode(db *gorm.DB, c *model.EmailOTPCode) error {
	return dbOr(r, db).Create(c).Error
}

// LatestPendingOTP returns the newest unconsumed OTP for a user,
// or gorm.ErrRecordNotFound when none exists.
func (r *AuthRepository) LatestPendingOTP(userID uuid.UUID) (*model.EmailOTPCode, error) {
	var c model.EmailOTPCode
	if err := r.db.Order("created_at DESC").
		First(&c, "user_id = ? AND consumed_at IS NULL", userID).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// IncrementOTPAttempts bumps the guess counter.
func (r *AuthRepository) IncrementOTPAttempts(db *gorm.DB, id uuid.UUID) error {
	return dbOr(r, db).Model(&model.EmailOTPCode{}).
		Where("id = ?", id).
		Update("attempts", gorm.Expr("attempts + 1")).Error
}

// ConsumeOTP stamps consumed_at (single use).
func (r *AuthRepository) ConsumeOTP(db *gorm.DB, id uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.EmailOTPCode{}).
		Where("id = ?", id).
		Update("consumed_at", at).Error
}

// InvalidateUserOTPs consumes all pending codes (resend rotates).
func (r *AuthRepository) InvalidateUserOTPs(db *gorm.DB, userID uuid.UUID, at time.Time) error {
	return dbOr(r, db).Model(&model.EmailOTPCode{}).
		Where("user_id = ? AND consumed_at IS NULL", userID).
		Update("consumed_at", at).Error
}

// ---------- login attempts ----------

// RecordLoginAttempt appends telemetry (never fails the auth flow).
func (r *AuthRepository) RecordLoginAttempt(a *model.LoginAttempt) {
	_ = r.db.Create(a).Error
}

func dbOr(r *AuthRepository, db *gorm.DB) *gorm.DB {
	if db != nil {
		return db
	}
	return r.db
}
