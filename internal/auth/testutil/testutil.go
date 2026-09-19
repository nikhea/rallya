// Package testutil spins up an isolated in-memory SQLite DB with the
// Auth schema migrated, for service/handler tests. Not for production use.
package testutil

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/notification/jobs"
)

// Setup returns a migrated DB, repository, and service.
// Sets a test JWT secret (config fail-closes without 32+ bytes).
// No enqueuer is wired: email jobs are skipped.
func Setup(t *testing.T) (*gorm.DB, *repository.AuthRepository, *service.AuthService) {
	t.Helper()
	db, repo, svc, _ := SetupWithEnqueuer(t)
	svc.SetEnqueuer(nil)
	return db, repo, svc
}

// SetupWithEnqueuer extends Setup with a FakeEnqueuer wired into the
// service, so tests can assert on enqueued email jobs.
func SetupWithEnqueuer(t *testing.T) (*gorm.DB, *repository.AuthRepository, *service.AuthService, *jobs.FakeEnqueuer) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-32-bytes-long-abcdefgh")

	db := OpenTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	repo := repository.NewAuthRepository(db)
	svc := service.NewAuthService(repo)
	fake := &jobs.FakeEnqueuer{}
	svc.SetEnqueuer(fake)
	return db, repo, svc, fake
}

// StrPtr builds a *string for profile fields.
func StrPtr(s string) *string { return &s }

// OpenTestDB opens an isolated in-memory SQLite DB for tests.
// The pool is pinned to one connection: :memory: databases are
// per-connection, so an open pool would scatter queries across
// empty databases (writes invisible to later reads).
func OpenTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("test sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return db
}

// FakeUserReader is a map-backed auth.UserReader for downstream domain
// tests (organization, iam). Misses return gorm.ErrRecordNotFound.
type FakeUserReader struct {
	ByID map[uuid.UUID]*model.User
}

// NewFakeUserReader builds an empty reader.
func NewFakeUserReader() *FakeUserReader {
	return &FakeUserReader{ByID: map[uuid.UUID]*model.User{}}
}

// Add registers a user (ID defaulted when nil).
func (f *FakeUserReader) Add(u *model.User) *model.User {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	f.ByID[u.ID] = u
	return u
}

// GetUserByID implements auth.UserReader.
func (f *FakeUserReader) GetUserByID(id uuid.UUID) (*model.User, error) {
	if u, ok := f.ByID[id]; ok {
		return u, nil
	}
	return nil, gorm.ErrRecordNotFound
}

// GetUserByEmail implements auth.UserReader (case-insensitive).
func (f *FakeUserReader) GetUserByEmail(email string) (*model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, u := range f.ByID {
		if strings.ToLower(u.Email) == email {
			return u, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}
