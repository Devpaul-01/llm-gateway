package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL string
}

func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}

	return &Config{
		DatabaseURL: dbURL,
	}, nil
}