package service

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	authmodel "github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/token"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgutils "github.com/nikhea/rallya/internal/organization/utils"
)

// ApiKeyStore is the consumer-declared persistence seam for org API keys.
// Implemented by the auth repository (auth owns secrets); wired via
// SetApiKeyStore so the dependency stays org -> auth-interface.
type ApiKeyStore interface {
	CreateApiKey(db *gorm.DB, k *authmodel.ApiKey) error
	GetApiKeyByID(id uuid.UUID) (*authmodel.ApiKey, error)
	ListApiKeysByOrg(orgID uuid.UUID, limit, offset int) ([]authmodel.ApiKey, int64, error)
	RevokeApiKey(db *gorm.DB, id uuid.UUID, at time.Time) error
}

// SetApiKeyStore wires hashed-key persistence (nil-unsafe: required for
// key endpoints; handlers are never registered without it in main).
func (s *OrgService) SetApiKeyStore(store ApiKeyStore) { s.apiKeys = store }

// apiKeyScope vocabulary mirrors the IAM matrix objects/actions plus "*"
// wildcards. Duplicated here (not imported) so org never imports iam —
// the seam direction stays org -> iam via wiring only.
var apiKeyObjects = map[string]bool{
	"org": true, "member": true, "invite": true, "event": true,
	"ticket": true, "attendee": true, "checkin": true, "audit": true,
	"role": true, "*": true,
}

var apiKeyActions = map[string]bool{
	"read": true, "create": true, "update": true, "delete": true,
	"manage": true, "publish": true, "*": true,
}

// CreateApiKey mints a per-org secret (ADMIN+). The raw secret is returned
// once and never stored; only its SHA-256 hash hits the database.
func (s *OrgService) CreateApiKey(creatorID uuid.UUID, ref, name string, scopes []string, expiresAt *time.Time) (*orgdto.ApiKeyCreated, error) {
	if s.apiKeys == nil {
		return nil, errors.New("api key store not wired")
	}
	o, _, err := s.requireRole(creatorID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, ErrInvalidApiKeyName
	}
	scopes = normalizeApiKeyScopes(scopes)
	if err := validateApiKeyScopes(scopes); err != nil {
		return nil, err
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return nil, ErrInvalidApiKeyExpiry
	}
	raw, hash, prefix, err := token.GenerateApiKey()
	if err != nil {
		return nil, err
	}
	key := &authmodel.ApiKey{
		OrgID: o.ID, CreatedBy: creatorID,
		Name: name, Prefix: prefix, KeyHash: hash,
		ExpiresAt: expiresAt,
	}
	if err := key.SetScopes(scopes); err != nil {
		return nil, err
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.apiKeys.CreateApiKey(tx, key); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &o.ID, ActorID: &creatorID,
			Action: "apikey.created", ObjectType: auditmodel.ObjectMember, ObjectID: &key.ID,
			After: map[string]any{"name": name, "prefix": prefix, "scopes": scopes},
		})
	}); err != nil {
		return nil, err
	}
	out := toApiKeyDTO(key)
	return &orgdto.ApiKeyCreated{ApiKey: *out, Key: raw}, nil
}

// ListApiKeys returns key metadata for an org (ADMIN+). Hashes/raw never leave.
func (s *OrgService) ListApiKeys(userID uuid.UUID, ref string, limit, offset int) ([]orgdto.ApiKey, int64, error) {
	if s.apiKeys == nil {
		return nil, 0, errors.New("api key store not wired")
	}
	o, _, err := s.requireRole(userID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := s.apiKeys.ListApiKeysByOrg(o.ID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]orgdto.ApiKey, 0, len(rows))
	for i := range rows {
		out = append(out, *toApiKeyDTO(&rows[i]))
	}
	return out, total, nil
}

// RevokeApiKey soft-revokes a key (ADMIN+, idempotent; row kept for audit).
func (s *OrgService) RevokeApiKey(userID uuid.UUID, ref string, keyID uuid.UUID) error {
	if s.apiKeys == nil {
		return errors.New("api key store not wired")
	}
	o, _, err := s.requireRole(userID, ref, orgmodel.MemberRoleAdmin)
	if err != nil {
		return err
	}
	key, err := s.apiKeys.GetApiKeyByID(keyID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrApiKeyNotFound
		}
		return err
	}
	if key.OrgID != o.ID {
		return ErrApiKeyNotFound
	}
	if key.RevokedAt != nil {
		return nil
	}
	now := time.Now()
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.apiKeys.RevokeApiKey(tx, key.ID, now); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &o.ID, ActorID: &userID,
			Action: "apikey.revoked", ObjectType: auditmodel.ObjectMember, ObjectID: &key.ID,
			Before: map[string]any{"prefix": key.Prefix, "name": key.Name},
		})
	})
}

func toApiKeyDTO(k *authmodel.ApiKey) *orgdto.ApiKey {
	return &orgdto.ApiKey{
		ID: k.ID.String(), Name: k.Name, Prefix: k.Prefix,
		Scopes:    k.Scopes(),
		ExpiresAt: optTime(k.ExpiresAt), LastUsedAt: optTime(k.LastUsedAt),
		RevokedAt: optTime(k.RevokedAt),
		CreatedBy: k.CreatedBy.String(),
		CreatedAt: orgutils.FormatTime(k.CreatedAt),
	}
}

func optTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := orgutils.FormatTime(*t)
	return &s
}

func normalizeApiKeyScopes(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func validateApiKeyScopes(scopes []string) error {
	for _, s := range scopes {
		obj, act, ok := splitApiKeyScope(s)
		if !ok || !apiKeyObjects[obj] || !apiKeyActions[act] {
			return ErrInvalidApiKeyScope
		}
	}
	return nil
}

func splitApiKeyScope(s string) (obj, act string, ok bool) {
	i := strings.Index(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}
