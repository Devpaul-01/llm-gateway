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

const (
	defaultRateLimitPerMin      = 60
	defaultMaxConcurrentStreams = 10
	defaultTokenBudget          = 100000
)

func Resolve(ctx context.Context, db *sql.DB, projectID string) (Settings, error) {
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
		RateLimitPerMin:      defaultRateLimitPerMin,
		MaxConcurrentStreams: defaultMaxConcurrentStreams,
		TokenBudget:          defaultTokenBudget,
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
