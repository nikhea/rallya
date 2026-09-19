package handler

import (
	"net/http"
	"strings"

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
)

// RequireAuth validates the Bearer access token, checks the session is
// still active, and the user is ACTIVE. Sets IDs in context.
func RequireAuth(repo *repository.AuthRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
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

// UserFromContext returns the authenticated user ID.
func UserFromContext(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(string(ctxUserID))
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// SessionFromContext returns the authenticated session ID.
func SessionFromContext(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(string(ctxSessionID))
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok && id != uuid.Nil
}
