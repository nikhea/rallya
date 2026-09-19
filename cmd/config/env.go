package config

import (
	"log/slog"

	"github.com/joho/godotenv"
)

func LoadEnv() {
	err := godotenv.Load()
	if err != nil {
		slog.Info("No .env file found")
	}
}
