// Package stats keeps cheap, hourly-bucketed counters in Redis for events that
// never reach request_logs: requests rejected before they hit a provider
// (rate limit, budget, concurrency) and every individual upstream attempt
// (including the failed ones that failover absorbed).
package stats

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	GlobalScope = "global"
	bucketTTL   = 8 * 24 * time.Hour

	RateLimited        = "rate_limited"
	BudgetBlocked      = "budget_blocked"
	ConcurrencyBlocked = "concurrency_blocked"
	// CircuitReroute counts requests whose first-choice provider was skipped
	// because its circuit breaker was open.
	CircuitReroute = "circuit_reroute"
)

func UpstreamOK(provider string) string { return "up_ok:" + provider }
func UpstreamFail(provider, category string) string {
	return "up_fail:" + provider + ":" + category
}

func key(scope string, t time.Time) string {
	return "gwstats:" + scope + ":" + t.UTC().Format("2006010215")
}

// Incr bumps the given fields in both the global bucket and the project's own
// bucket. projectID may be empty (global only).
func Incr(ctx context.Context, rdb *redis.Client, projectID string, fields ...string) error {
	if rdb == nil || len(fields) == 0 {
		return nil
	}
	now := time.Now()
	scopes := []string{GlobalScope}
	if projectID != "" {
		scopes = append(scopes, projectID)
	}
	pipe := rdb.Pipeline()
	for _, s := range scopes {
		k := key(s, now)
		for _, f := range fields {
			pipe.HIncrBy(ctx, k, f, 1)
		}
		pipe.Expire(ctx, k, bucketTTL)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// IncrAsync is Incr off the request path with its own short deadline.
func IncrAsync(rdb *redis.Client, projectID string, fields ...string) {
	if rdb == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = Incr(ctx, rdb, projectID, fields...)
	}()
}

// Read sums the last `hours` hourly buckets of a scope into one map.
func Read(ctx context.Context, rdb *redis.Client, scope string, hours int) (map[string]int64, error) {
	out := map[string]int64{}
	if rdb == nil {
		return out, nil
	}
	now := time.Now()
	pipe := rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, 0, hours)
	for i := 0; i < hours; i++ {
		cmds = append(cmds, pipe.HGetAll(ctx, key(scope, now.Add(-time.Duration(i)*time.Hour))))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("reading stats: %w", err)
	}
	for _, c := range cmds {
		for f, v := range c.Val() {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				continue
			}
			out[f] += n
		}
	}
	return out, nil
}

// ProviderAttempts splits the flat counter map into per-provider attempt stats.
type ProviderAttempts struct {
	OK       int64            `json:"ok"`
	Failed   int64            `json:"failed"`
	Failures map[string]int64 `json:"failures_by_category"`
}

func SplitProviders(counters map[string]int64) map[string]*ProviderAttempts {
	res := map[string]*ProviderAttempts{}
	get := func(p string) *ProviderAttempts {
		if res[p] == nil {
			res[p] = &ProviderAttempts{Failures: map[string]int64{}}
		}
		return res[p]
	}
	for f, n := range counters {
		switch {
		case strings.HasPrefix(f, "up_ok:"):
			get(strings.TrimPrefix(f, "up_ok:")).OK += n
		case strings.HasPrefix(f, "up_fail:"):
			rest := strings.TrimPrefix(f, "up_fail:")
			i := strings.LastIndex(rest, ":")
			if i < 0 {
				continue
			}
			p := get(rest[:i])
			p.Failed += n
			p.Failures[rest[i+1:]] += n
		}
	}
	return res
}
