package ratelimit
import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func AllowRequest(ctx context.Context, rdb *redis.Client, gatewayKeyID string, limit int, window time.Duration) (bool, error) {
	key := fmt.Sprintf("ratelimit:%s", gatewayKeyID)
	now := time.Now()
	windowStart := now.Add(-window)

	pipe := rdb.TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart.UnixNano()))
	countCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixNano()), Member: now.UnixNano()})
	pipe.Expire(ctx, key, window)

	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("rate limit check failed: %w", err)
	}

	count := countCmd.Val()
	return count < int64(limit), nil
}