package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL   string
	EncryptionKey string
	RedisURL      string
	AdminToken    string

	DefaultRateLimitPerMin      int
	DefaultMaxConcurrentStreams int
	DefaultTokenBudget          int

	Port        string
	AutoMigrate bool

	CacheEnabled    bool
	CacheTTLSeconds int

	PublicMetrics          bool
	PublicMetricsProjectID string
	CORSAllowedOrigins     []string
}

func boolFromEnv(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false, got %q", name, raw)
	}
	return b, nil
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

	cacheEnabled, err := boolFromEnv("CACHE_ENABLED", true)
	if err != nil {
		return nil, err
	}
	cacheTTL, err := intFromEnv("CACHE_TTL_SECONDS", 3600)
	if err != nil {
		return nil, err
	}
	autoMigrate, err := boolFromEnv("AUTO_MIGRATE", true)
	if err != nil {
		return nil, err
	}
	publicMetrics, err := boolFromEnv("PUBLIC_METRICS_ENABLED", false)
	if err != nil {
		return nil, err
	}
	var origins []string
	for _, o := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return &Config{
		DatabaseURL:                 dbURL,
		EncryptionKey:               key,
		RedisURL:                    redisURL,
		AdminToken:                  adminToken,
		DefaultRateLimitPerMin:      rateLimit,
		DefaultMaxConcurrentStreams: maxConcurrent,
		DefaultTokenBudget:          tokenBudget,
		Port:                        port,
		AutoMigrate:                 autoMigrate,
		CacheEnabled:                cacheEnabled,
		CacheTTLSeconds:             cacheTTL,
		PublicMetrics:               publicMetrics,
		PublicMetricsProjectID:      strings.TrimSpace(os.Getenv("PUBLIC_METRICS_PROJECT_ID")),
		CORSAllowedOrigins:          origins,
	}, nil
}
