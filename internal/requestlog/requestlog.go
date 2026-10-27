package requestlog

import (
	"context"
	"database/sql"
	"time"
)

type Entry struct {
	RequestID          string
	ProjectID          string
	GatewayKeyID       string
	Provider           string
	Model              string
	CredentialID       *string
	Status             string
	ErrorCategory      *string
	FirstErrorCategory *string
	TokensIn           *int
	TokensOut          *int
	UsageEstimated     bool
	TTFTMs             *int
	TotalDurationMs    *int
	Stream             bool
	UsedVision         bool
	CacheHit           bool
	Attempts           int
	FailoverCount      int
	ModelsUsed         []string
}

func Insert(ctx context.Context, db *sql.DB, e Entry) error {
	attempts := e.Attempts
	if attempts < 1 {
		attempts = 1
	}
	models := e.ModelsUsed
	if models == nil {
		models = []string{}
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO request_logs (
			request_id, project_id, gateway_key_id, provider, model, credential_id,
			status, error_category, first_error_category, tokens_in, tokens_out, usage_estimated,
			ttft_ms, total_duration_ms, stream, used_vision, cache_hit,
			attempts, failover_count, models_used
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`, e.RequestID, e.ProjectID, e.GatewayKeyID, e.Provider, e.Model, e.CredentialID,
		e.Status, e.ErrorCategory, e.FirstErrorCategory, e.TokensIn, e.TokensOut, e.UsageEstimated,
		e.TTFTMs, e.TotalDurationMs, e.Stream, e.UsedVision, e.CacheHit,
		attempts, e.FailoverCount, models)
	return err
}

// InsertWithTimeout writes a log row detached from the request context (the
// client may already be gone) but bounded so a slow database can't pile up
// goroutines forever.
func InsertWithTimeout(db *sql.DB, e Entry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return Insert(ctx, db, e)
}
