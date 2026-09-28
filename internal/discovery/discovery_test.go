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
