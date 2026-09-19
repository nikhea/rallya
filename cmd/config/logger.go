package config

import (
	"log/slog"
	"os"
)

// InitLogger installs a JSON slog handler on stdout at Debug level as the
// process-wide default logger. Call once at startup before anything logs.
func InitLogger() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
}
