package concurrency

import (
	"context"
	"sync"
	"testing"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
	"github.com/joho/godotenv"
)

func TestAcquire_EnforcesLimitUnderConcurrency(t *testing.T) {
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

	testProject := "test-concurrency-project-unique-67890"
	defer rdb.Del(ctx, "concurrent:"+testProject)

	maxConcurrent := 5
	totalAttempts := 20

	var wg sync.WaitGroup
	var mu sync.Mutex
	allowedCount := 0

	for i := 0; i < totalAttempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, err := Acquire(ctx, rdb, testProject, maxConcurrent)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if allowed {
				mu.Lock()
				allowedCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if allowedCount != maxConcurrent {
		t.Errorf("expected exactly %d allowed, got %d", maxConcurrent, allowedCount)
	}
}