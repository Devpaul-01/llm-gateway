package projectsettings

import (
	"context"
	"database/sql"
)

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
	var rateLimit, concurrency, budget sql.NullInt64

	err := db.QueryRowContext(ctx, `
		SELECT rate_limit_per_min, max_concurrent_streams, token_budget
		FROM projects
		WHERE id = $1
	`, projectID).Scan(&rateLimit, &concurrency, &budget)
	if err != nil {
		return Settings{}, err
	}

	s := Settings{
		RateLimitPerMin:      defaults.RateLimitPerMin,
		MaxConcurrentStreams: defaults.MaxConcurrentStreams,
		TokenBudget:          defaults.TokenBudget,
	}

	if rateLimit.Valid {
		s.RateLimitPerMin = int(rateLimit.Int64)
	}
	if concurrency.Valid {
		s.MaxConcurrentStreams = int(concurrency.Int64)
	}
	if budget.Valid {
		s.TokenBudget = int(budget.Int64)
	}

	return s, nil
}