package projectsettings

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const cacheTTL = 5 * time.Minute

func cacheKey(projectID string) string { return "projcfg:" + projectID }

// Invalidate drops the cached overrides for a project; call after PATCHing it.
func Invalidate(ctx context.Context, rdb *redis.Client, projectID string) {
	if rdb == nil {
		return
	}
	rdb.Del(ctx, cacheKey(projectID))
}

// overrides are the raw per-project values (nil = use the default). They are
// cached un-merged so a change to the env defaults is never masked by a cache.
type overrides struct {
	RateLimitPerMin      *int `json:"r"`
	MaxConcurrentStreams *int `json:"c"`
	TokenBudget          *int `json:"b"`
}

type Settings struct {
	RateLimitPerMin      int
	MaxConcurrentStreams int
	TokenBudget          int
}

type Defaults struct {
	RateLimitPerMin      int
	MaxConcurrentStreams int
	TokenBudget          int
}

func Resolve(ctx context.Context, db *sql.DB, projectID string, defaults Defaults) (Settings, error) {
	return ResolveCached(ctx, db, nil, projectID, defaults)
}

// ResolveCached is Resolve with a Redis read-through cache (pass a nil rdb to bypass).
func ResolveCached(ctx context.Context, db *sql.DB, rdb *redis.Client, projectID string, defaults Defaults) (Settings, error) {
	var o overrides
	hit := false
	if rdb != nil {
		if cached, err := rdb.Get(ctx, cacheKey(projectID)).Result(); err == nil {
			hit = json.Unmarshal([]byte(cached), &o) == nil
		}
	}

	if !hit {
		var rateLimit, concurrency, budget sql.NullInt64
		err := db.QueryRowContext(ctx, `
			SELECT rate_limit_per_min, max_concurrent_streams, token_budget
			FROM projects
			WHERE id = $1
		`, projectID).Scan(&rateLimit, &concurrency, &budget)
		if err != nil {
			return Settings{}, err
		}
		if rateLimit.Valid {
			v := int(rateLimit.Int64)
			o.RateLimitPerMin = &v
		}
		if concurrency.Valid {
			v := int(concurrency.Int64)
			o.MaxConcurrentStreams = &v
		}
		if budget.Valid {
			v := int(budget.Int64)
			o.TokenBudget = &v
		}
		if rdb != nil {
			if data, err := json.Marshal(o); err == nil {
				rdb.Set(ctx, cacheKey(projectID), data, cacheTTL)
			}
		}
	}

	s := Settings{
		RateLimitPerMin:      defaults.RateLimitPerMin,
		MaxConcurrentStreams: defaults.MaxConcurrentStreams,
		TokenBudget:          defaults.TokenBudget,
	}
	if o.RateLimitPerMin != nil {
		s.RateLimitPerMin = *o.RateLimitPerMin
	}
	if o.MaxConcurrentStreams != nil {
		s.MaxConcurrentStreams = *o.MaxConcurrentStreams
	}
	if o.TokenBudget != nil {
		s.TokenBudget = *o.TokenBudget
	}
	return s, nil
}
