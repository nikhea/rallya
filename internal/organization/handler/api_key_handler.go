package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authhandler "github.com/nikhea/rallya/internal/auth/handler"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	"github.com/nikhea/rallya/internal/organization/service"
)

// CreateApiKey POST /api/v1/orgs/:id/api-keys (ADMIN+, user JWT only).
// The raw secret is returned once; only its hash is stored.
//
// @Summary		Create API key
// @Description	Mints a per-org secret for server-to-server SDK use. Raw key shown once.
// @Tags			orgs
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string					true	"Org UUID or slug"
// @Param			request	body		orgdto.CreateApiKey	true	"Key payload"
// @Success		201		{object}	orgdto.ApiKeyCreated
// @Failure		400		{object}	orgdto.ErrorAlias	"Validation error"
// @Failure		401		{object}	orgdto.ErrorAlias
// @Failure		403		{object}	orgdto.ErrorAlias	"API keys cannot manage keys"
// @Failure		404		{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/api-keys [post]
func (h *Handler) CreateApiKey(c *gin.Context) {
	if authhandler.IsApiKeyAuth(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	uid, ok := authhandler.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var in orgdto.CreateApiKey
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var expiresAt *time.Time
	if in.ExpiresAt != nil && *in.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "expiresAt must be RFC3339"})
			return
		}
		expiresAt = &t
	}
	created, err := h.svc.CreateApiKey(uid, OrgRef(c), in.Name, in.Scopes, expiresAt)
	if err != nil {
		c.JSON(apiKeyErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

// ListApiKeys GET /api/v1/orgs/:id/api-keys (ADMIN+, user JWT only).
//
// @Summary		List API keys
// @Description	Key metadata (prefix/scopes, never hashes or raw secrets).
// @Tags			orgs
// @Produce		json
// @Security		BearerAuth
// @Param			id	path		string	true	"Org UUID or slug"
// @Success		200	{object}	orgdto.ApiKeysPage
// @Failure		401	{object}	orgdto.ErrorAlias
// @Failure		403	{object}	orgdto.ErrorAlias
// @Failure		404	{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/api-keys [get]
func (h *Handler) ListApiKeys(c *gin.Context) {
	if authhandler.IsApiKeyAuth(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	uid, ok := authhandler.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	limit, offset := page(c)
	keys, total, err := h.svc.ListApiKeys(uid, OrgRef(c), limit, offset)
	if err != nil {
		c.JSON(apiKeyErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	if keys == nil {
		keys = []orgdto.ApiKey{}
	}
	c.JSON(http.StatusOK, gin.H{"items": keys, "total": total})
}

// RevokeApiKey DELETE /api/v1/orgs/:id/api-keys/:keyId (ADMIN+, idempotent).
//
// @Summary		Revoke API key
// @Description	Soft-revokes a key (row kept for audit). Idempotent.
// @Tags			orgs
// @Produce		json
// @Security		BearerAuth
// @Param			id		path	string	true	"Org UUID or slug"
// @Param			keyId	path	string	true	"API key UUID"
// @Success		204
// @Failure		401	{object}	orgdto.ErrorAlias
// @Failure		403	{object}	orgdto.ErrorAlias
// @Failure		404	{object}	orgdto.ErrorAlias
// @Router			/orgs/{id}/api-keys/{keyId} [delete]
func (h *Handler) RevokeApiKey(c *gin.Context) {
	if authhandler.IsApiKeyAuth(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	uid, ok := authhandler.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	keyID, err := uuid.Parse(c.Param("keyId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	if err := h.svc.RevokeApiKey(uid, OrgRef(c), keyID); err != nil {
		c.JSON(apiKeyErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func apiKeyErrorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrApiKeyNotFound),
		errors.Is(err, service.ErrOrgNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, service.ErrInvalidApiKeyName),
		errors.Is(err, service.ErrInvalidApiKeyScope),
		errors.Is(err, service.ErrInvalidApiKeyExpiry):
		return http.StatusBadRequest
	default:
		return orgErrorStatus(err)
	}
}
