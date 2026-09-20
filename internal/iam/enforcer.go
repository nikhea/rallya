package iam

import (
	"fmt"
	"sync"

	"github.com/casbin/casbin/v3"
	casbinmodel "github.com/casbin/casbin/v3/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

// mu serializes ALL enforcer access process-wide. Casbin's management-API
// reads (GetFilteredPolicy et al.) don't self-synchronize against writes,
// so concurrent SyncMembership calls race without this. Reads take RLock,
// mutations take Lock. Single-enforcer assumption holds: main builds one.
var mu sync.RWMutex

// NewEnforcer builds the Casbin enforcer over the shared GORM handle.
// The adapter's own AutoMigrate stays OFF: migrations/*.sql owns the
// casbin_rule table. Policies load from the DB at boot.
func NewEnforcer(db *gorm.DB) (*casbin.Enforcer, error) {
	gormadapter.TurnOffAutoMigrate(db)
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		return nil, fmt.Errorf("casbin adapter: %w", err)
	}
	m, err := casbinmodel.NewModelFromString(ModelText())
	if err != nil {
		return nil, fmt.Errorf("casbin model: %w", err)
	}
	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("casbin enforcer: %w", err)
	}
	if err := e.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("casbin load policy: %w", err)
	}
	return e, nil
}

// Enforce reports whether sub may act on obj in dom.
// All inputs are strings (UUIDs stringified); errors AND blank inputs
// fail closed (deny) so malformed requests never match wildcards.
func Enforce(e *casbin.Enforcer, sub, dom, obj, act string) bool {
	if sub == "" || dom == "" || obj == "" || act == "" {
		return false
	}
	mu.RLock()
	defer mu.RUnlock()
	ok, err := e.Enforce(sub, dom, obj, act)
	if err != nil {
		return false
	}
	return ok
}

// LockWrite serializes a mutation closure against all other enforcer
// access. Internal use by sync/seed paths.
func LockWrite(fn func() error) error {
	mu.Lock()
	defer mu.Unlock()
	return fn()
}
