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

// TestAdjust_RefundsUnusedReservation is the regression test for Finding
// B-3: a request reserves against the full requested max_tokens ceiling,
// but actual usage is almost always lower. Without Adjust, that unused
// headroom is never given back and the budget starves far faster than real
// spend justifies. This test reserves a large estimate, then adjusts down
// to a much smaller real usage figure, and confirms the counter reflects
// the smaller number afterward -- i.e. that subsequent requests see the
// refunded headroom.
func TestAdjust_RefundsUnusedReservation(t *testing.T) {
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

	projectID := "test-budget-adjust-project-unique"
	limit := 1000
	reserved := 500
	actualUsed := 50

	testKey := "budget:" + projectID + ":" + time.Now().UTC().Format("2006-01-02")
	defer rdb.Del(ctx, testKey)

	allowed, err := budget.CheckAndReserve(ctx, rdb, projectID, reserved, limit)
	if err != nil {
		t.Fatalf("unexpected error reserving: %v", err)
	}
	if !allowed {
		t.Fatalf("expected initial reservation of %d against limit %d to be allowed", reserved, limit)
	}

	// Refund the unused headroom: actual (50) - reserved (500) = -450.
	if err := budget.Adjust(ctx, rdb, projectID, actualUsed-reserved); err != nil {
		t.Fatalf("unexpected error adjusting: %v", err)
	}

	current, err := rdb.Get(ctx, testKey).Int64()
	if err != nil {
		t.Fatalf("reading budget counter after adjust: %v", err)
	}
	if current != int64(actualUsed) {
		t.Errorf("expected counter to reflect actual usage %d after refund, got %d", actualUsed, current)
	}

	// With the refund applied, there should now be room for many more
	// requests of similar size that would NOT have fit under the original
	// (un-refunded) reservation.
	allowed, err = budget.CheckAndReserve(ctx, rdb, projectID, 400, limit)
	if err != nil {
		t.Fatalf("unexpected error on follow-up reservation: %v", err)
	}
	if !allowed {
		t.Error("expected follow-up reservation to be allowed after refund freed up budget headroom")
	}
}

// TestAdjust_NoOpOnExpiredKey confirms Adjust degrades safely when the
// underlying day's counter no longer exists (e.g. TTL expired, or this is
// called long after the fact) rather than erroring or fabricating a new
// negative counter.
func TestAdjust_NoOpOnExpiredKey(t *testing.T) {
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

	projectID := "test-budget-adjust-noop-project-unique"
	testKey := "budget:" + projectID + ":" + time.Now().UTC().Format("2006-01-02")
	defer rdb.Del(ctx, testKey)

	// No reservation was ever made, so the key doesn't exist.
	if err := budget.Adjust(ctx, rdb, projectID, -100); err != nil {
		t.Fatalf("unexpected error adjusting non-existent key: %v", err)
	}

	exists, err := rdb.Exists(ctx, testKey).Result()
	if err != nil {
		t.Fatalf("checking key existence: %v", err)
	}
	if exists != 0 {
		t.Error("expected Adjust to be a no-op on a non-existent key, but a key was created")
	}
}