package iam_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/casbin/casbin/v3"
	casbinmodel "github.com/casbin/casbin/v3/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/iam"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
)

// fileEnforcer builds an enforcer on a file-backed sqlite DB so a second
// instance can prove policy persistence across reloads.
func fileEnforcer(t *testing.T, path string) (*casbin.Enforcer, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	m, err := casbinmodel.NewModelFromString(iam.ModelText())
	if err != nil {
		t.Fatalf("model: %v", err)
	}
	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}
	if err := e.LoadPolicy(); err != nil {
		t.Fatalf("load: %v", err)
	}
	return e, db
}

func TestPersistenceAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "casbin.db")
	e1, _ := fileEnforcer(t, path)
	org := uuid.New()
	user := uuid.New()
	mustSeed(t, e1, org)
	syncRole(t, e1, user, org, ptrRole(orgmodel.MemberRoleAdmin))

	// Fresh instance, fresh adapter: rights must survive.
	e2, _ := fileEnforcer(t, path)
	if !iam.Enforce(e2, user.String(), org.String(), iam.ObjInvite, iam.ActCreate) {
		t.Fatal("admin invite right lost across reload")
	}
	if iam.Enforce(e2, user.String(), org.String(), iam.ObjOrg, iam.ActDelete) {
		t.Fatal("admin must not delete org after reload")
	}
	// Mutation through the second instance works.
	syncRole(t, e2, user, org, nil)
	if iam.Enforce(e2, user.String(), org.String(), iam.ObjOrg, iam.ActRead) {
		t.Fatal("expected deny after removal via reloaded instance")
	}
}

func TestSeedAndSyncIdempotentRowCounts(t *testing.T) {
	e := newTestEnforcer(t)
	org := uuid.New()
	user := uuid.New()

	countRules := func() int64 {
		pols, err := e.GetPolicy()
		if err != nil {
			t.Fatalf("get policy: %v", err)
		}
		groups, err := e.GetGroupingPolicy()
		if err != nil {
			t.Fatalf("get grouping: %v", err)
		}
		return int64(len(pols) + len(groups))
	}

	mustSeed(t, e, org)
	mustSeed(t, e, org)
	mustSeed(t, e, org)
	// 1 owner wildcard + 20 admin + 4 member = 25 policy rows, no dupes.
	if n := countRules(); n != 25 {
		t.Fatalf("expected 25 policy rows after reseeds, got %d", n)
	}
	syncRole(t, e, user, org, ptrRole(orgmodel.MemberRoleAdmin))
	syncRole(t, e, user, org, ptrRole(orgmodel.MemberRoleAdmin))
	if n := countRules(); n != 26 {
		t.Fatalf("expected 26 rows after duplicate syncs, got %d", n)
	}
	syncRole(t, e, user, org, nil)
	syncRole(t, e, user, org, nil)
	if n := countRules(); n != 25 {
		t.Fatalf("expected 25 rows after removals, got %d", n)
	}
}

// TestConcurrentEnforce hammers one enforcer from many goroutines while
// policies mutate underneath. Run with -race: any data race in Casbin's
// locking or our wrappers fails the suite.
func TestConcurrentEnforce(t *testing.T) {
	e := newTestEnforcer(t)
	org := uuid.New()
	mustSeed(t, e, org)
	users := make([]uuid.UUID, 8)
	for i := range users {
		users[i] = uuid.New()
		syncRole(t, e, users[i], org, ptrRole(orgmodel.MemberRoleMember))
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			u := users[g].String()
			for i := 0; i < 200; i++ {
				if !iam.Enforce(e, u, org.String(), iam.ObjOrg, iam.ActRead) {
					t.Errorf("goroutine %d: expected allow", g)
					return
				}
				if iam.Enforce(e, u, org.String(), iam.ObjOrg, iam.ActDelete) {
					t.Errorf("goroutine %d: expected deny", g)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestConcurrentMutations interleaves grouping writes with reads. The
// sqlite handle is single-connection so no busy errors; -race watches
// the Go side. Final state must converge: all synced ADMIN.
func TestConcurrentMutations(t *testing.T) {
	e := newTestEnforcer(t)
	org := uuid.New()
	mustSeed(t, e, org)
	syncer := iam.NewMembershipSyncer(e)

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			u := uuid.New()
			role := orgmodel.MemberRoleAdmin
			for i := 0; i < 25; i++ {
				if err := syncer.SyncMembership(u, org, &role); err != nil {
					t.Errorf("sync: %v", err)
					return
				}
				_ = iam.Enforce(e, u.String(), org.String(), iam.ObjOrg, iam.ActUpdate)
			}
		}(g)
	}
	wg.Wait()
}
