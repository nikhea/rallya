package config

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" for database/sql
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	// DB is the GORM handle used by domain repositories.
	DB *gorm.DB
	// SQLDB is the shared *sql.DB under GORM. River's database/sql driver
	// runs on this handle so jobs enqueue on the same pool (and can join
	// GORM transactions). See https://riverqueue.com/docs/gorm.
	SQLDB *sql.DB
	// ListenerPool is a dedicated pgx pool for River LISTEN/NOTIFY.
	// database/sql cannot LISTEN, so without this River falls back to
	// polling (still correct, just slower pickup). Nil when unreachable.
	ListenerPool *pgxpool.Pool
)

func ConnectDatabase() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Error("DATABASE_URL is not set")
		os.Exit(1)
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		slog.Error("Database handle failed", "error", err)
		os.Exit(1)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	if err := sqlDB.Ping(); err != nil {
		slog.Error("Database connection failed", "error", err)
		os.Exit(1)
	}

	database, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB}),
		&gorm.Config{},
	)
	if err != nil {
		slog.Error("GORM open failed", "error", err)
		os.Exit(1)
	}

	DB = database
	SQLDB = sqlDB

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if pool, err := pgxpool.New(ctx, dsn); err != nil {
		slog.Warn("River listener pool unavailable, job pickup will poll", "error", err)
	} else {
		ListenerPool = pool
	}

	slog.Info("Database connected")
}

// CloseDatabase releases the listener pool and shared handle.
func CloseDatabase() {
	if ListenerPool != nil {
		ListenerPool.Close()
		ListenerPool = nil
	}
	if SQLDB != nil {
		_ = SQLDB.Close()
		SQLDB = nil
	}
}
