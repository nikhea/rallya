package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/nikhea/rallya/cmd/config"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/auth/token"
)

type ctxKey string

const (
	ctxUserID    ctxKey = "auth.userID"
	ctxSessionID ctxKey = "auth.sessionID"
	// ctxApiKeyID, when present, marks API-key auth (server-to-server).
	// The key acts as its creator for membership/Casbin checks; scopes
	// snapshot-intersect in the IAM middleware.
	ctxApiKeyID     ctxKey = "auth.apiKeyID"
	ctxApiKeyOrgID  ctxKey = "auth.apiKeyOrgID"
	ctxApiKeyScopes ctxKey = "auth.apiKeyScopes"
)

// RequireAuth validates either an org API key (X-API-Key) or the Bearer
// access token, checks the identity is still active, and sets IDs in
// context. Identical 401 envelope either way (no oracle).
func RequireAuth(repo *repository.AuthRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		if raw := strings.TrimSpace(c.GetHeader("X-API-Key")); raw != "" {
			key, ok := validateApiKey(repo, raw)
			if !ok {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
			c.Set(string(ctxUserID), key.CreatedBy)
			c.Set(string(ctxApiKeyID), key.ID)
			c.Set(string(ctxApiKeyOrgID), key.OrgID)
			c.Set(string(ctxApiKeyScopes), key.Scopes())
			_ = repo.TouchApiKeyLastUsed(key.ID, time.Now())
			c.Next()
			return
		}
		raw := c.GetHeader("Authorization")
		raw = strings.TrimPrefix(raw, "Bearer ")
		raw = strings.TrimSpace(raw)
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		userID, sessionID, err := token.ParseAccessToken(config.JWTSecret(), raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		sess, err := repo.GetSessionByID(sessionID)
		if err != nil || sess.RevokedAt != nil || sess.UserID != userID {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		u, err := repo.GetUserByID(userID)
		if err != nil || u.Status != model.UserStatusActive {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set(string(ctxUserID), userID)
		c.Set(string(ctxSessionID), sessionID)
		c.Next()
	}
}

// validateApiKey checks hash, revocation, expiry, and creator ACTIVE.
// Fail-closed on every error; no reason is leaked to the caller.
func validateApiKey(repo *repository.AuthRepository, raw string) (*model.ApiKey, bool) {
	if !token.IsApiKeyShape(raw) {
		return nil, false
	}
	key, err := repo.GetApiKeyByHash(token.HashToken(strings.TrimSpace(raw)))
	if err != nil {
		return nil, false
	}
	if key.RevokedAt != nil {
		return nil, false
	}
	if key.ExpiresAt != nil && !time.Now().Before(*key.ExpiresAt) {
		return nil, false
	}
	u, err := repo.GetUserByID(key.CreatedBy)
	if err != nil || u.Status != model.UserStatusActive {
		return nil, false
	}
	return key, true
}

// UserFromContext returns the authenticated user ID (creator for API keys).
func UserFromContext(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(string(ctxUserID))
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// SessionFromContext returns the authenticated session ID (absent for keys).
func SessionFromContext(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(string(ctxSessionID))
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// ApiKeyFromContext returns the authenticating API key ID, if key auth.
func ApiKeyFromContext(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(string(ctxApiKeyID))
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// IsApiKeyAuth reports whether the request authenticated via X-API-Key.
func IsApiKeyAuth(c *gin.Context) bool {
	_, ok := ApiKeyFromContext(c)
	return ok
}

// ApiKeyOrgIDFromContext returns the org the authenticating key is bound to.
func ApiKeyOrgIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(string(ctxApiKeyOrgID))
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// ApiKeyScopesFromContext returns the key's snapshotted scopes.
// Empty/nil means inherit the creator's live role (no extra restriction).
func ApiKeyScopesFromContext(c *gin.Context) []string {
	v, ok := c.Get(string(ctxApiKeyScopes))
	if !ok || v == nil {
		return nil
	}
	scopes, _ := v.([]string)
	return scopes
}
