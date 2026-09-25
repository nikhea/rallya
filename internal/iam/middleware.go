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
		// API keys are scope-intersected: a non-empty snapshot must
		// contain "object:action" for this call (empty = inherit the
		// creator's live role). Keys can never manage keys or touch
		// platform admin — those routes require user JWT only.
		if scopes := authhandler.ApiKeyScopesFromContext(c); len(scopes) > 0 && !apiKeyScopeAllows(scopes, obj, act) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		if authhandler.IsApiKeyAuth(c) && (obj == ObjApikey || obj == "admin") {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
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

// apiKeyScopeAllows reports whether a snapshotted "object:action" grant
// covers this call. "*" wildcards either side, mirroring Casbin's OWNER
// wildcard convention.
func apiKeyScopeAllows(scopes []string, obj, act string) bool {
	for _, s := range scopes {
		o, a, ok := splitScope(s)
		if !ok {
			continue
		}
		if (o == "*" || o == obj) && (a == "*" || a == act) {
			return true
		}
	}
	return false
}

// ApiKeyScopeAllows is the exported scope check for services/validators.
func ApiKeyScopeAllows(scopes []string, obj, act string) bool {
	return apiKeyScopeAllows(scopes, obj, act)
}

func splitScope(s string) (obj, act string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i], s[i+1:], s[:i] != "" && s[i+1:] != ""
		}
	}
	return "", "", false
}

// RequireSuperAdmin gates platform routes (/api/v1/admin/*).
// No org context needed: superadmin is domain-less by design.
// API keys can never pass: platform admin requires user JWT.
func RequireSuperAdmin(e *casbin.Enforcer) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, ok := authhandler.UserFromContext(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if authhandler.IsApiKeyAuth(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		if !IsSuperAdmin(e, uid) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}
