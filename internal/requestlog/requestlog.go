package requestlog

import (
	"context"
	"database/sql"
	"strings"
)

// encodeTextArray renders a Go []string as a Postgres TEXT[] array literal
// (e.g. {"groq:model-a","mistral:model-b"}) suitable for passing as a
// query parameter through database/sql's generic driver interface.
//
// This is deliberately not using pgx's pgtype.FlatArray: FlatArray only
// implements pgx's own ArraySetter/ArrayGetter interfaces, which are used
// by pgx's native Conn.Query/Exec path, not by the generic database/sql
// driver.Valuer/Scanner interfaces this codebase relies on via
// "database/sql" + the pgx stdlib driver (see internal/db/postgres.go's
// sql.Open("pgx", ...)). Passing a FlatArray directly as a database/sql
// parameter does not reliably marshal into a real Postgres array.
func encodeTextArray(values []string) string {
	if len(values) == 0 {
		return "{}"
	}
	quoted := make([]string, len(values))
	for i, v := range values {
		escaped := strings.ReplaceAll(v, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		quoted[i] = `"` + escaped + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

// Entry is a single request_logs row. Audit findings B-1, B-2, and B-6
// identified that ErrorCategory, CredentialID, and ModelsUsed were all
// either present on this struct but never set, or (in ModelsUsed's case)
// entirely absent, despite each having a corresponding schema column and a
// specific architectural purpose: ErrorCategory drives the
// error-rate-by-category observability query ADR-19 exists to support;
// CredentialID is the join key ADR-18's last_used_at derivation depends
// on; ModelsUsed is the only persistent record that the opt-in
// continuation feature (Section 10.2) ever ran for a given request.
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
	UsedVision      bool
	ModelsUsed      []string
}

func Insert(ctx context.Context, db *sql.DB, e Entry) error {
	var credentialID sql.NullString
	if e.CredentialID != nil {
		credentialID = sql.NullString{String: *e.CredentialID, Valid: true}
	}

	var errorCategory sql.NullString
	if e.ErrorCategory != nil {
		errorCategory = sql.NullString{String: *e.ErrorCategory, Valid: true}
	}

	// A nil/empty slice encodes as an empty Postgres array, matching the
	// schema's "populated only on continuation" intent (non-continuation
	// requests simply get an empty array rather than NULL, which is fine
	// for an append-only observability log and keeps the column
	// type-consistent for every row).
	modelsUsed := encodeTextArray(e.ModelsUsed)

	_, err := db.ExecContext(ctx, `
		INSERT INTO request_logs (
			project_id, gateway_key_id, provider, model, credential_id,
			status, error_category, tokens_in, tokens_out,
			ttft_ms, total_duration_ms, stream, used_vision, models_used
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::text[])
	`, e.ProjectID, e.GatewayKeyID, e.Provider, e.Model, credentialID,
		e.Status, errorCategory, e.TokensIn, e.TokensOut,
		e.TTFTMs, e.TotalDurationMs, e.Stream, e.UsedVision, modelsUsed)
	return err
}

// LastUsedAt derives the most recent request_logs timestamp for a given
// credential, per ADR-18's deliberate "no last_used_at column, computed on
// demand" design. Returns (zero time, false, nil) if the credential has
// never appeared in request_logs. This was previously undiscoverable via
// any code path because CredentialID was never populated on insert
// (Finding B-6); with that fixed, this query now returns real data.
func LastUsedAt(ctx context.Context, db *sql.DB, credentialID string) (sql.NullTime, error) {
	var lastUsed sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT MAX(created_at) FROM request_logs WHERE credential_id = $1
	`, credentialID).Scan(&lastUsed)
	if err != nil {
		return sql.NullTime{}, err
	}
	return lastUsed, nil
}