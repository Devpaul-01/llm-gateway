package concurrency

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// slotTTLSeconds bounds how long a leaked slot (process crash between Acquire
// and Release) can survive: the key expires on its own, so a crashed instance
// can't permanently lock a project out.
const slotTTLSeconds = 15 * 60

var acquireScript = redis.NewScript(`
local c = redis.call('INCR', KEYS[1])
redis.call('EXPIRE', KEYS[1], ARGV[2])
if c > tonumber(ARGV[1]) then
  redis.call('DECR', KEYS[1])
  return 0
end
return 1
`)

var releaseScript = redis.NewScript(`
local c = redis.call('DECR', KEYS[1])
if c < 0 then
  redis.call('SET', KEYS[1], 0, 'KEEPTTL')
end
return 1
`)

func Acquire(ctx context.Context, rdb *redis.Client, projectID string, maxConcurrent int) (bool, error) {
	key := fmt.Sprintf("concurrent:%s", projectID)
	res, err := acquireScript.Run(ctx, rdb, []string{key}, maxConcurrent, slotTTLSeconds).Int()
	if err != nil {
		return false, fmt.Errorf("acquiring concurrency slot: %w", err)
	}
	return res == 1, nil
}

func Release(ctx context.Context, rdb *redis.Client, projectID string) {
	key := fmt.Sprintf("concurrent:%s", projectID)
	releaseScript.Run(ctx, rdb, []string{key})
}
