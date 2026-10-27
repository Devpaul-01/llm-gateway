package ratelimit

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Sliding-window log, executed atomically in one round trip. Rejected requests
// are NOT recorded, so a client hammering a closed door doesn't keep extending
// its own lockout.
var allowScript = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], 0, ARGV[2])
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[3]) then
  return 0
end
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[5])
redis.call('PEXPIRE', KEYS[1], ARGV[4])
return 1
`)

var seq atomic.Uint64

func AllowRequest(ctx context.Context, rdb *redis.Client, gatewayKeyID string, limit int, window time.Duration) (bool, error) {
	key := fmt.Sprintf("ratelimit:%s", gatewayKeyID)
	now := time.Now()
	nowMs := now.UnixMilli()
	windowStartMs := now.Add(-window).UnixMilli()
	member := fmt.Sprintf("%d:%d", now.UnixNano(), seq.Add(1))

	res, err := allowScript.Run(ctx, rdb, []string{key}, nowMs, windowStartMs, limit, window.Milliseconds(), member).Int()
	if err != nil {
		return false, fmt.Errorf("rate limit check failed: %w", err)
	}
	return res == 1, nil
}
