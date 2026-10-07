package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL   string
	EncryptionKey string
	RedisURL      string
	AdminToken    string

	DefaultRateLimitPerMin      int
	DefaultMaxConcurrentStreams int
	DefaultTokenBudget          int
}

func intFromEnv(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, raw)
	}
	return n, nil
}

func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}
	key := os.Getenv("ENCRYPTION_KEY")
	if key == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY environment variable is required")
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL environment variable is required")
	}
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		return nil, fmt.Errorf("ADMIN_TOKEN environment variable is required")
	}

	rateLimit, err := intFromEnv("DEFAULT_RATE_LIMIT_PER_MIN", 60)
	if err != nil {
		return nil, err
	}
	maxConcurrent, err := intFromEnv("DEFAULT_MAX_CONCURRENT_STREAMS", 10)
	if err != nil {
		return nil, err
	}
	tokenBudget, err := intFromEnv("DEFAULT_TOKEN_BUDGET", 100000)
	if err != nil {
		return nil, err
	}

	return &Config{
		DatabaseURL:                 dbURL,
		EncryptionKey:               key,
		RedisURL:                    redisURL,
		AdminToken:                  adminToken,
		DefaultRateLimitPerMin:      rateLimit,
		DefaultMaxConcurrentStreams: maxConcurrent,
		DefaultTokenBudget:          tokenBudget,
	}, nil
}