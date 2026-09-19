// Command migrate applies versioned schema as a release step,
// before the API boots. It runs two engines in order:
//
//  1. golang-migrate for app tables (migrations/*.sql, schema_migrations)
//  2. rivermigrate for River queue tables (river_* via river_migration)
//
// Usage:
//
//	go run ./cmd/migrate up            # migrate to latest
//	go run ./cmd/migrate down [n]      # roll back n (default 1, app only)
//	go run ./cmd/migrate version       # print current versions
//	go run ./cmd/migrate force <v>     # clear dirty state after manual fix
//
// It reads DATABASE_URL (.env supported). Run with an owner-level user;
// the API itself runs with a restricted user and never migrates.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/nikhea/rallya/cmd/config"
	"github.com/nikhea/rallya/migrations"
)

func main() {
	if len(os.Args) < 2 {
		usage(2)
	}
	config.LoadEnv()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is not set")
		os.Exit(1)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	defer db.Close()

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration source:", err)
		os.Exit(1)
	}
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration driver:", err)
		os.Exit(1)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate instance:", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fatal("up", err, m)
		}
		v, _, _ := m.Version()
		fmt.Println("app migrated to version", v)
		migrateRiver(db)
	case "down":
		n := 1
		if len(os.Args) > 2 {
			n, err = strconv.Atoi(os.Args[2])
			if err != nil || n < 1 {
				fmt.Fprintln(os.Stderr, "down needs a positive integer, got", os.Args[2:])
				os.Exit(2)
			}
		}
		if err := m.Steps(-n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fatal("down", err, m)
		}
		fmt.Println("rolled back", n, "app step(s) (river schema untouched; remove manually if needed)")
	case "version":
		v, dirty, err := m.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			fmt.Println("no app migrations applied")
		} else if err != nil {
			fmt.Fprintln(os.Stderr, "version:", err)
			os.Exit(1)
		} else {
			fmt.Printf("app version %d dirty=%v\n", v, dirty)
		}
		printRiverVersions(db)
	case "force":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: migrate force <version>")
			os.Exit(2)
		}
		v, err := strconv.Atoi(os.Args[2])
		if err != nil || v < 0 {
			fmt.Fprintln(os.Stderr, "force needs a version integer, got", os.Args[2])
			os.Exit(2)
		}
		if err := m.Force(v); err != nil {
			fmt.Fprintln(os.Stderr, "force:", err)
			os.Exit(1)
		}
		fmt.Println("forced version to", v, "(fix the DB manually first)")
	default:
		usage(2)
	}
}

func fatal(op string, err error, m *migrate.Migrate) {
	var dirty migrate.ErrDirty
	fmt.Fprintln(os.Stderr, op+":", err)
	if errors.As(err, &dirty) {
		fmt.Fprintln(os.Stderr, "database is dirty at version", dirty.Version,
			"— fix manually, then: go run ./cmd/migrate force <version>")
	}
	if _, serr := m.Close(); serr != nil {
		fmt.Fprintln(os.Stderr, "close:", serr)
	}
	os.Exit(1)
}

// riverMigrator builds a rivermigrate Migrator over the shared handle.
func riverMigrator(db *sql.DB) (*rivermigrate.Migrator[*sql.Tx], error) {
	return rivermigrate.New(riverdatabasesql.New(db), nil)
}

// migrateRiver applies all pending River queue migrations.
func migrateRiver(db *sql.DB) {
	migrator, err := riverMigrator(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, "river migrator:", err)
		os.Exit(1)
	}
	res, err := migrator.Migrate(context.Background(), rivermigrate.DirectionUp, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "river up:", err)
		os.Exit(1)
	}
	for _, v := range res.Versions {
		fmt.Println("river migrated version", v.Version, v.Name)
	}
	if len(res.Versions) == 0 {
		fmt.Println("river schema up to date")
	}
}

// printRiverVersions lists applied River migration versions.
func printRiverVersions(db *sql.DB) {
	migrator, err := riverMigrator(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, "river migrator:", err)
		os.Exit(1)
	}
	versions, err := migrator.ExistingVersions(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "river versions:", err)
		os.Exit(1)
	}
	if len(versions) == 0 {
		fmt.Println("no river migrations applied")
		return
	}
	for _, v := range versions {
		fmt.Println("river version", v.Version, v.Name)
	}
}

func usage(code int) {
	fmt.Fprintln(os.Stderr, "usage: migrate <up|down [n]|version|force <v>>")
	os.Exit(code)
}
