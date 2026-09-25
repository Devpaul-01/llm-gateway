package cooldown

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const cooldownDuration = 60 * time.Second

func MarkFailed(ctx context.Context, rdb *redis.Client, credentialID string) error {
	key := fmt.Sprintf("cooldown:%s", credentialID)
	return rdb.Set(ctx, key, "1", cooldownDuration).Err()
}

func IsCooling(ctx context.Context, rdb *redis.Client, credentialID string) (bool, error) {
	key := fmt.Sprintf("cooldown:%s", credentialID)
	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("checking cooldown state: %w", err)
	}
	return exists > 0, nil
}
