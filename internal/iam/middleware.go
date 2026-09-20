package iam

import (
	"log/slog"
	"net/http"

	"github.com/casbin/casbin/v3"
	"github.com/gin-gonic/gin"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
)

// RequirePermission enforces (user, org, obj, act) via Casbin.
// Chain after auth + RequireOrgContext. Denials are 403; unresolvable
// context is 401/404 from the earlier middlewares, never here.
func RequirePermission(e *casbin.Enforcer, obj, act string) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, ok := authhandler.UserFromContext(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		orgID, ok := orghandler.OrgIDFromContext(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "organization not found"})
			return
		}
		allowed, err := e.Enforce(uid.String(), orgID.String(), obj, act)
		if err != nil {
			slog.Warn("casbin enforce error (deny)", "error", err)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}

// RequireSuperAdmin gates platform routes (/api/v1/admin/* when they land).
// No org context needed: superadmin is domain-less by design.
func RequireSuperAdmin(e *casbin.Enforcer) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, ok := authhandler.UserFromContext(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if !IsSuperAdmin(e, uid) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}
