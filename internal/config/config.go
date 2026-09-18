package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL   string
	EncryptionKey string
}

func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}
	key := os.Getenv("ENCRYPTION_KEY")
	if key == "" {
		return nil, fmt.Errorf("Encryption key environment variable is required")
	}

	return &Config{
		DatabaseURL:   dbURL,
		EncryptionKey: key,
	}, nil
}
