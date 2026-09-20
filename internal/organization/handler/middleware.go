package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	"github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/repository"
)

type ctxKey string

const (
	ctxOrgID   ctxKey = "org.id"
	ctxOrgRole ctxKey = "org.role"
)

// RequireOrgContext resolves :id (UUID or slug), requires caller membership,
// and injects org ID + role. Non-members get 404 (stealth, no existence leak).
func RequireOrgContext(repo *repository.OrgRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, ok := authhandler.UserFromContext(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		ref := strings.TrimSpace(c.Param("id"))
		if ref == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "org id is required"})
			return
		}
		o, m, err := resolveMembership(repo, uid, ref)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "organization not found"})
			return
		}
		c.Set(string(ctxOrgID), o.ID)
		c.Set(string(ctxOrgRole), m.Role)
		c.Next()
	}
}

// OrgIDFromContext returns the resolved org ID.
func OrgIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(string(ctxOrgID))
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// OrgRef returns the raw :id param (UUID or slug) for service resolution.
func OrgRef(c *gin.Context) string {
	return strings.TrimSpace(c.Param("id"))
}

// OrgRoleFromContext returns the caller's membership role.
func OrgRoleFromContext(c *gin.Context) (model.MemberRole, bool) {
	v, ok := c.Get(string(ctxOrgRole))
	if !ok {
		return "", false
	}
	role, ok := v.(model.MemberRole)
	return role, ok
}

func resolveMembership(repo *repository.OrgRepository, uid uuid.UUID, ref string) (*model.Organization, *model.Membership, error) {
	var (
		o   *model.Organization
		err error
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		o, err = repo.GetOrgByID(id)
	} else {
		o, err = repo.GetOrgBySlug(strings.ToLower(ref))
	}
	if err != nil {
		return nil, nil, err
	}
	m, err := repo.GetMembership(o.ID, uid)
	if err != nil {
		return nil, nil, err
	}
	return o, m, nil
}
