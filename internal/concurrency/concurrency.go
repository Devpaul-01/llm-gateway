package concurrency

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func Acquire(ctx context.Context, rdb *redis.Client, projectID string, maxConcurrent int) (bool, error) {
	key := fmt.Sprintf("concurrent:%s", projectID)

	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("incrementing concurrency counter: %w", err)
	}

	if count > int64(maxConcurrent) {
		rdb.Decr(ctx, key)
		return false, nil
	}

	return true, nil
}

func Release(ctx context.Context, rdb *redis.Client, projectID string) {
	key := fmt.Sprintf("concurrent:%s", projectID)
	rdb.Decr(ctx, key)
}
