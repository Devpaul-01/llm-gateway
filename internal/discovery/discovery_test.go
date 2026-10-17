package discovery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
	"github.com/joho/godotenv"
)

func TestEvictModel_RemovesOnlyTheNamedModel(t *testing.T) {
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

	provider := "test-provider-eviction"
	cacheKey := "discovery:" + provider
	defer rdb.Del(ctx, cacheKey)

	seed, _ := json.Marshal([]string{"model-a", "model-b", "model-c"})
	if err := rdb.Set(ctx, cacheKey, seed, cacheTTL).Err(); err != nil {
		t.Fatalf("seeding cache: %v", err)
	}

	EvictModel(ctx, rdb, provider, "model-b")

	raw, err := rdb.Get(ctx, cacheKey).Result()
	if err != nil {
		t.Fatalf("reading cache after eviction: %v", err)
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		t.Fatalf("decoding cache: %v", err)
	}

	if len(models) != 2 || models[0] != "model-a" || models[1] != "model-c" {
		t.Errorf("expected [model-a model-c], got %v", models)
	}
}

// TestGetModels_ReturnsFromCacheOnHit covers a previously-untested path
// (Audit §12): GetModels when the Redis cache already has a value should
// return it directly without performing any discovery HTTP call or lock
// acquisition.
func TestGetModels_ReturnsFromCacheOnHit(t *testing.T) {
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

	provider := "test-provider-cache-hit"
	cacheKey := "discovery:" + provider
	defer rdb.Del(ctx, cacheKey)

	seed, _ := json.Marshal([]string{"cached-model-1", "cached-model-2"})
	if err := rdb.Set(ctx, cacheKey, seed, cacheTTL).Err(); err != nil {
		t.Fatalf("seeding cache: %v", err)
	}

	// baseURL/apiKey are deliberately invalid -- if GetModels tried to
	// actually fetch, it would fail and fall back to `fallback`, which
	// would make this test fail in a way that reveals the cache wasn't
	// actually used.
	fallback := []string{"fallback-model"}
	models := GetModels(ctx, rdb, provider, "http://invalid.invalid", "fake-key", fallback)

	if len(models) != 2 || models[0] != "cached-model-1" || models[1] != "cached-model-2" {
		t.Errorf("expected cached models [cached-model-1 cached-model-2], got %v", models)
	}
}

// TestGetModels_FallsBackWhenLockHeldByAnotherInstance covers the
// single-flight lock-contention path: if another instance already holds
// the discovery lock, this call should not block or retry -- it should
// return the fallback list immediately for this one request cycle.
func TestGetModels_FallsBackWhenLockHeldByAnotherInstance(t *testing.T) {
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

	provider := "test-provider-lock-contention"
	cacheKey := "discovery:" + provider
	lockKey := "discovery:" + provider + ":lock"
	defer rdb.Del(ctx, cacheKey)
	defer rdb.Del(ctx, lockKey)

	// Simulate another instance holding the lock.
	if err := rdb.SetNX(ctx, lockKey, "1", lockTTL).Err(); err != nil {
		t.Fatalf("seeding lock: %v", err)
	}

	fallback := []string{"fallback-model"}
	models := GetModels(ctx, rdb, provider, "http://invalid.invalid", "fake-key", fallback)

	if len(models) != 1 || models[0] != "fallback-model" {
		t.Errorf("expected fallback list when lock is held elsewhere, got %v", models)
	}
}
