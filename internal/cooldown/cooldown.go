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

// CoolingSet checks many credentials in a single Redis round trip and returns
// the IDs that are currently cooling down.
func CoolingSet(ctx context.Context, rdb *redis.Client, credentialIDs []string) (map[string]bool, error) {
	cooling := make(map[string]bool)
	if len(credentialIDs) == 0 {
		return cooling, nil
	}
	pipe := rdb.Pipeline()
	cmds := make([]*redis.IntCmd, len(credentialIDs))
	for i, id := range credentialIDs {
		cmds[i] = pipe.Exists(ctx, fmt.Sprintf("cooldown:%s", id))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("checking cooldown state: %w", err)
	}
	for i, id := range credentialIDs {
		if cmds[i].Val() > 0 {
			cooling[id] = true
		}
	}
	return cooling, nil
}
