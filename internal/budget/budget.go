package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyTTLSeconds = 25 * 3600

// Reserve-if-it-fits, atomically. The previous INCR-then-DECR approach briefly
// pushed the counter over the limit and could reject concurrent requests that
// would have fit.
var reserveScript = redis.NewScript(`
local cur = tonumber(redis.call('GET', KEYS[1]) or '0')
if cur + tonumber(ARGV[1]) > tonumber(ARGV[2]) then
  return 0
end
redis.call('INCRBY', KEYS[1], ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[3])
return 1
`)

// Adjust the counter by a signed delta, never letting it go below zero.
var adjustScript = redis.NewScript(`
local v = redis.call('INCRBY', KEYS[1], ARGV[1])
if v < 0 then
  redis.call('SET', KEYS[1], 0, 'KEEPTTL')
end
if redis.call('TTL', KEYS[1]) < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
return 1
`)

func dayKey(projectID string, t time.Time) string {
	return fmt.Sprintf("budget:%s:%s", projectID, t.UTC().Format("2006-01-02"))
}

// CheckAndReserve reserves estimatedTokens against the project's daily budget.
func CheckAndReserve(ctx context.Context, rdb *redis.Client, projectID string, estimatedTokens int, dailyLimit int) (bool, error) {
	res, err := reserveScript.Run(ctx, rdb, []string{dayKey(projectID, time.Now())}, estimatedTokens, dailyLimit, keyTTLSeconds).Int()
	if err != nil {
		return false, fmt.Errorf("reserving budget: %w", err)
	}
	return res == 1, nil
}

// Settle replaces a reservation made at reservedAt with the tokens actually
// consumed: it refunds the unused part of an over-estimate (or charges the
// overshoot). Without this, every request is billed for max_tokens no matter
// how short the answer was, and failed requests are billed in full.
func Settle(ctx context.Context, rdb *redis.Client, projectID string, reservedAt time.Time, reserved, actual int) error {
	delta := actual - reserved
	if delta == 0 {
		return nil
	}
	if err := adjustScript.Run(ctx, rdb, []string{dayKey(projectID, reservedAt)}, delta, keyTTLSeconds).Err(); err != nil {
		return fmt.Errorf("settling budget: %w", err)
	}
	return nil
}

// SecondsUntilReset is how long until the daily budget rolls over (UTC midnight).
func SecondsUntilReset(now time.Time) int {
	n := now.UTC()
	next := time.Date(n.Year(), n.Month(), n.Day()+1, 0, 0, 0, 0, time.UTC)
	return int(next.Sub(n).Seconds()) + 1
}
