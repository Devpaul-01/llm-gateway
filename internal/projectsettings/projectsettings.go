package projectsettings

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
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

type updateProjectSettingsRequest struct {
	RateLimitPerMin      *int `json:"rate_limit_per_min"`
	MaxConcurrentStreams *int `json:"max_concurrent_streams"`
	TokenBudget          *int `json:"token_budget"`
}

func HandleUpdateProjectSettings(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		if projectID == "" {
			http.Error(w, "project id is required", http.StatusBadRequest)
			return
		}

		var body updateProjectSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		_, err := db.ExecContext(r.Context(), `
			UPDATE projects
			SET rate_limit_per_min = $1, max_concurrent_streams = $2, token_budget = $3
			WHERE id = $4
		`, body.RateLimitPerMin, body.MaxConcurrentStreams, body.TokenBudget, projectID)
		if err != nil {
			http.Error(w, "failed to update project settings", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

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
