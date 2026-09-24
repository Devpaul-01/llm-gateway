package budget_test

import (
	"context"
	"testing"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/budget"
	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
	"github.com/joho/godotenv"
)

func TestTokenBudget_End_To_End(t *testing.T) {
	_ = godotenv.Load("../../.env")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	ctx := context.Background()
	rdb, err := redisclient.Connect(ctx, cfg.RedisURL)
	if err != nil {
		t.Fatalf("error connecting to redis: %v", err)
	}
	defer rdb.Close()

	estimatedTokens := 30
	limit := 100
	projectID := "test-budget-project-unique-54321"

	testKey := "budget:" + projectID + ":" + time.Now().UTC().Format("2006-01-02")
	defer rdb.Del(ctx, testKey)

	for i := 0; i < 4; i++ {
		allowed, err := budget.CheckAndReserve(ctx, rdb, projectID, estimatedTokens, limit)
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}

		if i < 3 && !allowed {
			t.Fatalf("iteration %d: expected reservation to be allowed", i)
		}
		if i >= 3 && allowed {
			t.Fatalf("iteration %d: expected reservation to be denied", i)
		}
	}
}