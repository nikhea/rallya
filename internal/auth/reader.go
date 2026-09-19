// Package auth is the Auth domain boundary.
//
// Auth owns User identity only. Organization membership and
// permissions live in the organization and iam domains.
//
// Downstream domains must depend on the UserReader interface below,
// never on auth internals (service/repository internals may evolve
// or be extracted to a standalone service).
package auth

import (
	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/auth/model"
)

// UserReader is the cross-domain contract for resolving identity.
// Organization / IAM / Events consume this, e.g.:
//
//	Auth -> (User identity) -> Organization -> IAM -> Events
type UserReader interface {
	GetUserByID(id uuid.UUID) (*model.User, error)
	GetUserByEmail(email string) (*model.User, error)
}

// Ensure the model satisfies the shape downstream expects.
var _ = (*model.User)(nil)
