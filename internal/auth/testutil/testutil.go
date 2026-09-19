// Package testutil spins up an isolated in-memory SQLite DB with the
// Auth schema migrated, for service/handler tests. Not for production use.
package testutil

import (
	"testing"

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

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
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
