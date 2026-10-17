package requestlog_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/Devpaul-01/llm-gateway/internal/requestlog"
	"github.com/joho/godotenv"
)

// uniqueTestKeyHash generates a random key_hash for test fixtures.
// gateway_api_keys.key_hash has a UNIQUE constraint; hardcoded literal
// hashes across different test functions in this file previously
// collided whenever a prior run's cleanup hadn't fully completed (e.g.
// the test process was interrupted before its deferred DELETE ran),
// causing spurious "duplicate key value" failures on the next run that
// had nothing to do with the behavior actually under test.
func uniqueTestKeyHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating random test key hash: %v", err)
	}
	return hex.EncodeToString(b)
}

// pgArrayScanner is a minimal sql.Scanner for reading a Postgres TEXT[]
// column back out as a []string in a test assertion. This intentionally
// mirrors only the simple, no-escaped-comma case produced by
// requestlog.Insert's own encodeTextArray, not a general-purpose Postgres
// array parser -- it exists solely to verify the round trip in this test
// file, not as production code.
type pgArrayScanner struct {
	dest *[]string
}

func (s pgArrayScanner) Scan(src interface{}) error {
	var raw string
	switch v := src.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		*s.dest = nil
		return nil
	}

	raw = strings.TrimPrefix(raw, "{")
	raw = strings.TrimSuffix(raw, "}")
	if raw == "" {
		*s.dest = []string{}
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, len(parts))
	for i, p := range parts {
		p = strings.TrimPrefix(p, `"`)
		p = strings.TrimSuffix(p, `"`)
		p = strings.ReplaceAll(p, `\"`, `"`)
		p = strings.ReplaceAll(p, `\\`, `\`)
		result[i] = p
	}
	*s.dest = result
	return nil
}

// TestInsert_PersistsErrorCategoryCredentialIDAndModelsUsed is the direct
// regression test for Findings B-1, B-2, and B-6: previously, handler.go's
// defer block never set ErrorCategory or CredentialID on the Entry it
// built, and Entry had no ModelsUsed field at all. This confirms the fixed
// Insert path actually round-trips all three through real Postgres.
func TestInsert_PersistsErrorCategoryCredentialIDAndModelsUsed(t *testing.T) {
	_ = godotenv.Load("../../.env")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	var projectID string
	err = pool.QueryRowContext(ctx, `INSERT INTO projects (name) VALUES ($1) RETURNING id`, "test-requestlog-project").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	var gatewayKeyID string
	err = pool.QueryRowContext(ctx, `
		INSERT INTO gateway_api_keys (project_id, key_hash, key_prefix, label)
		VALUES ($1, $2, 'gw_live_test', 'test-key')
		RETURNING id
	`, projectID, uniqueTestKeyHash(t)).Scan(&gatewayKeyID)
	if err != nil {
		t.Fatalf("creating gateway key: %v", err)
	}

	var credentialID string
	err = pool.QueryRowContext(ctx, `
		INSERT INTO provider_credentials (project_id, provider, label, encrypted_key, key_nonce, key_version)
		VALUES ($1, 'groq', 'test-cred', '\x00', '\x00', 1)
		RETURNING id
	`, projectID).Scan(&credentialID)
	if err != nil {
		t.Fatalf("creating credential: %v", err)
	}

	errorCategory := "key_fault"
	tokensIn := 10
	tokensOut := 20

	entry := requestlog.Entry{
		ProjectID:     projectID,
		GatewayKeyID:  gatewayKeyID,
		Provider:      "groq",
		Model:         "openai/gpt-oss-120b",
		CredentialID:  &credentialID,
		Status:        "success",
		ErrorCategory: &errorCategory,
		TokensIn:      &tokensIn,
		TokensOut:     &tokensOut,
		Stream:        true,
		UsedVision:    false,
		ModelsUsed:    []string{"groq:model-a", "mistral:model-b"},
	}

	if err := requestlog.Insert(ctx, pool, entry); err != nil {
		t.Fatalf("inserting request log: %v", err)
	}

	var gotCredentialID, gotErrorCategory string
	var gotModelsUsed []string
	err = pool.QueryRowContext(ctx, `
		SELECT credential_id, error_category, models_used
		FROM request_logs
		WHERE project_id = $1
	`, projectID).Scan(&gotCredentialID, &gotErrorCategory, pgArrayScanner{&gotModelsUsed})
	if err != nil {
		t.Fatalf("reading back request log: %v", err)
	}

	if gotCredentialID != credentialID {
		t.Errorf("expected credential_id %q, got %q", credentialID, gotCredentialID)
	}
	if gotErrorCategory != errorCategory {
		t.Errorf("expected error_category %q, got %q", errorCategory, gotErrorCategory)
	}
	if len(gotModelsUsed) != 2 || gotModelsUsed[0] != "groq:model-a" || gotModelsUsed[1] != "mistral:model-b" {
		t.Errorf("expected models_used [groq:model-a mistral:model-b], got %v", gotModelsUsed)
	}
}

// TestInsert_EmptyModelsUsedForNonContinuationRequest confirms a normal
// (non-continuation) request logs an empty array, not an error, when
// ModelsUsed is nil.
func TestInsert_EmptyModelsUsedForNonContinuationRequest(t *testing.T) {
	_ = godotenv.Load("../../.env")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	var projectID string
	err = pool.QueryRowContext(ctx, `INSERT INTO projects (name) VALUES ($1) RETURNING id`, "test-requestlog-empty-models").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	var gatewayKeyID string
	err = pool.QueryRowContext(ctx, `
		INSERT INTO gateway_api_keys (project_id, key_hash, key_prefix, label)
		VALUES ($1, $2, 'gw_live_test2', 'test-key-2')
		RETURNING id
	`, projectID, uniqueTestKeyHash(t)).Scan(&gatewayKeyID)
	if err != nil {
		t.Fatalf("creating gateway key: %v", err)
	}

	entry := requestlog.Entry{
		ProjectID:    projectID,
		GatewayKeyID: gatewayKeyID,
		Provider:     "groq",
		Model:        "openai/gpt-oss-120b",
		Status:       "success",
		Stream:       true,
	}

	if err := requestlog.Insert(ctx, pool, entry); err != nil {
		t.Fatalf("inserting request log with nil ModelsUsed: %v", err)
	}

	var count int
	err = pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM request_logs
		WHERE project_id = $1 AND models_used = '{}'
	`, projectID).Scan(&count)
	if err != nil {
		t.Fatalf("checking models_used default: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row with empty models_used array, got %d", count)
	}
}

// TestLastUsedAt_DerivesFromRequestLogs is the regression test for ADR-18:
// last_used_at should be computable from request_logs once credential_id
// is actually populated (Finding B-6's fix).
func TestLastUsedAt_DerivesFromRequestLogs(t *testing.T) {
	_ = godotenv.Load("../../.env")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	var projectID string
	err = pool.QueryRowContext(ctx, `INSERT INTO projects (name) VALUES ($1) RETURNING id`, "test-lastused-project").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	var gatewayKeyID string
	err = pool.QueryRowContext(ctx, `
		INSERT INTO gateway_api_keys (project_id, key_hash, key_prefix, label)
		VALUES ($1, $2, 'gw_live_test3', 'test-key-3')
		RETURNING id
	`, projectID, uniqueTestKeyHash(t)).Scan(&gatewayKeyID)
	if err != nil {
		t.Fatalf("creating gateway key: %v", err)
	}

	var credentialID string
	err = pool.QueryRowContext(ctx, `
		INSERT INTO provider_credentials (project_id, provider, label, encrypted_key, key_nonce, key_version)
		VALUES ($1, 'groq', 'test-cred-2', '\x00', '\x00', 1)
		RETURNING id
	`, projectID).Scan(&credentialID)
	if err != nil {
		t.Fatalf("creating credential: %v", err)
	}

	noneYet, err := requestlog.LastUsedAt(ctx, pool, credentialID)
	if err != nil {
		t.Fatalf("unexpected error before any usage: %v", err)
	}
	if noneYet.Valid {
		t.Error("expected no last_used_at before any request_logs row exists")
	}

	if err := requestlog.Insert(ctx, pool, requestlog.Entry{
		ProjectID:    projectID,
		GatewayKeyID: gatewayKeyID,
		Provider:     "groq",
		Model:        "openai/gpt-oss-120b",
		CredentialID: &credentialID,
		Status:       "success",
		Stream:       true,
	}); err != nil {
		t.Fatalf("inserting request log: %v", err)
	}

	lastUsed, err := requestlog.LastUsedAt(ctx, pool, credentialID)
	if err != nil {
		t.Fatalf("unexpected error after usage: %v", err)
	}
	if !lastUsed.Valid {
		t.Error("expected a valid last_used_at after a request_logs row exists")
	}
}
