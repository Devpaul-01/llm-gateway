package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func CheckAndReserve(ctx context.Context, rdb *redis.Client, projectID string, estimatedTokens int, dailyLimit int) (bool, error) {
	key := fmt.Sprintf("budget:%s:%s", projectID, time.Now().UTC().Format("2006-01-02"))
	current, err := rdb.IncrBy(ctx, key, int64(estimatedTokens)).Result()
	if err != nil {
		return false, fmt.Errorf("incrementing budget counter: %w", err)
	}

	if current == int64(estimatedTokens) {
		rdb.Expire(ctx, key, 25*time.Hour)
	}

	if current > int64(dailyLimit) {
		rdb.DecrBy(ctx, key, int64(estimatedTokens))
		return false, nil
	}

	return true, nil
}
