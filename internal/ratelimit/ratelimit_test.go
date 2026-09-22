package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
	"github.com/joho/godotenv"
)

func TestAllowRequest_BlocksOverLimit(t *testing.T) {
	_ = godotenv.Load("../../.env")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	ctx := context.Background()
	rdb, err := redisclient.Connect(ctx, cfg.RedisURL)
	if err != nil {
		t.Fatalf("connecting to redis: %v", err)
	}
	defer rdb.Close()

	testKey := "test-ratelimit-key-unique-12345"
	defer rdb.Del(ctx, "ratelimit:"+testKey)

	limit := 3
	window := time.Minute

	for i := 0; i < limit; i++ {
		allowed, err := AllowRequest(ctx, rdb, testKey, limit, window)
		if err != nil {
			t.Fatalf("unexpected error on request %d: %v", i, err)
		}
		if !allowed {
			t.Fatalf("request %d should have been allowed, was blocked", i)
		}
	}

	allowed, err := AllowRequest(ctx, rdb, testKey, limit, window)
	if err != nil {
		t.Fatalf("unexpected error on over-limit request: %v", err)
	}
	if allowed {
		t.Error("expected the request over the limit to be blocked, but it was allowed")
	}
}