package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func dayKey(projectID string) string {
	return fmt.Sprintf("budget:%s:%s", projectID, time.Now().UTC().Format("2006-01-02"))
}

// CheckAndReserve atomically reserves estimatedTokens against a project's
// daily token budget and reports whether the reservation fits inside
// dailyLimit. This is a fast pre-request gate (Architecture Section 12,
// ADR-20) -- it is deliberately approximate under concurrent bursts, not a
// precise accounting mechanism on its own. Callers that know the real token
// usage after the request completes should call Adjust to true up the
// difference between what was reserved here and what was actually used;
// without that follow-up call, every request permanently reserves its full
// estimate even if actual usage was much lower.
func CheckAndReserve(ctx context.Context, rdb *redis.Client, projectID string, estimatedTokens int, dailyLimit int) (bool, error) {
	key := dayKey(projectID)
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

// Adjust corrects a prior reservation to reflect real usage once it's
// known. delta is actualTokens - reservedTokens: a negative delta (actual
// usage came in under the reservation, the common case) refunds the
// difference back into the day's remaining budget; a positive delta (the
// request used more than was reserved -- possible since reservations are
// made against the client-requested max_tokens ceiling, which usually
// exceeds what a response actually consumes, but a provider could in
// principle report more) further debits the counter. A request that failed
// before producing any output should pass delta = -reservedTokens to fully
// refund the reservation.
//
// This is a best-effort correction, consistent with ADR-20's accepted
// trade-off that the Redis counter is a fast approximate gate and
// request_logs remains the authoritative record for real reporting. If the
// key has already expired (crossed a day boundary, or TTL elapsed) this is
// a no-op: there's nothing meaningful left to adjust, and that's fine --
// the authoritative historical record lives in request_logs, not here.
func Adjust(ctx context.Context, rdb *redis.Client, projectID string, delta int) error {
	if delta == 0 {
		return nil
	}
	key := dayKey(projectID)

	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("checking budget key existence: %w", err)
	}
	if exists == 0 {
		return nil
	}

	if _, err := rdb.IncrBy(ctx, key, int64(delta)).Result(); err != nil {
		return fmt.Errorf("adjusting budget counter: %w", err)
	}
	return nil
}
