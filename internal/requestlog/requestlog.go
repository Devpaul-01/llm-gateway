package requestlog

import (
	"context"
	"database/sql"
)

type Entry struct {
	ProjectID       string
	GatewayKeyID    string
	Provider        string
	Model           string
	CredentialID    *string
	Status          string
	ErrorCategory   *string
	TokensIn        *int
	TokensOut       *int
	TTFTMs          *int
	TotalDurationMs *int
	Stream          bool
}

func Insert(ctx context.Context, db *sql.DB, e Entry) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO request_logs (
			project_id, gateway_key_id, provider, model, credential_id,
			status, error_category, tokens_in, tokens_out,
			ttft_ms, total_duration_ms, stream
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, e.ProjectID, e.GatewayKeyID, e.Provider, e.Model, e.CredentialID,
		e.Status, e.ErrorCategory, e.TokensIn, e.TokensOut,
		e.TTFTMs, e.TotalDurationMs, e.Stream)
	return err
}