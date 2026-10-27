// Package breaker is a tiny shared-state circuit breaker for upstream providers.
//
// Without it, a provider that is down (or hanging until a timeout) is retried
// first by every single request before failover kicks in, so an outage taxes
// every caller. With it, after a handful of transient failures inside a short
// window the provider is "open": it is moved to the back of the candidate list
// (still tried as a last resort, never removed) until the window expires or a
// request to it succeeds. State lives in Redis so all gateway instances agree.
package breaker

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// Threshold is the number of transient failures within Window that opens the breaker.
	Threshold = 5
	Window    = 30 * time.Second
)

func key(provider string) string { return "breaker:" + provider }

var recordScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
return n
`)

// RecordFailure counts one transient failure for the provider.
func RecordFailure(ctx context.Context, rdb *redis.Client, provider string) error {
	return recordScript.Run(ctx, rdb, []string{key(provider)}, int(Window.Seconds())).Err()
}

// RecordSuccess closes the breaker immediately: the provider just worked.
func RecordSuccess(ctx context.Context, rdb *redis.Client, provider string) error {
	return rdb.Del(ctx, key(provider)).Err()
}

// OpenSet returns which of the providers currently have an open breaker.
func OpenSet(ctx context.Context, rdb *redis.Client, providers []string) (map[string]bool, error) {
	open := make(map[string]bool)
	if len(providers) == 0 {
		return open, nil
	}
	keys := make([]string, len(providers))
	for i, p := range providers {
		keys[i] = key(p)
	}
	vals, err := rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for i, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue
		}
		var n int
		for _, ch := range s {
			if ch < '0' || ch > '9' {
				n = 0
				break
			}
			n = n*10 + int(ch-'0')
		}
		if n >= Threshold {
			open[providers[i]] = true
		}
	}
	return open, nil
}
