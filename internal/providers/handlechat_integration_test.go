package providers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/joho/godotenv"
)

// uniqueTestKeyHash generates a random key_hash for test fixtures.
// gateway_api_keys.key_hash has a UNIQUE constraint; a hardcoded literal
// hash across repeated manual/test runs can collide with leftover data
// from a prior run whose cleanup didn't complete (e.g. a manual smoke
// test interrupted before cleanup ran), causing a spurious "duplicate
// key value" failure unrelated to the behavior actually under test --
// this happened once already in internal/requestlog's test file and was
// fixed the same way there.
func uniqueTestKeyHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating random test key hash: %v", err)
	}
	return hex.EncodeToString(b)
}

var newLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// TestHandleChat_ResolutionFailureStillWritesRequestLog is the regression
// test for a gap found while verifying the new /metrics endpoint against
// a live server: calling HandleChat/HandleChatWithBudget for a project
// with no configured credentials returns an error directly from
// resolveCandidates, before handleChatWithCandidates's goroutine (and its
// defer block, which normally writes the request_logs row and records
// the metric) is ever constructed. Previously this entire class of
// failure -- misconfigured project, typo'd model, no credentials for the
// requested provider -- left no historical record and no metric at all.
// Does not require a live provider API key: the failure under test is
// "no credentials exist for this project", which is reachable with only
// a real Postgres connection.
func TestHandleChat_ResolutionFailureStillWritesRequestLog(t *testing.T) {
	_ = godotenv.Load("../../.env")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	encryptionKey, err := hex.DecodeString(cfg.EncryptionKey)
	if err != nil {
		t.Fatalf("decoding encryption key: %v", err)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	var projectID string
	err = pool.QueryRowContext(ctx, `
		INSERT INTO projects (name) VALUES ($1) RETURNING id
	`, "test-project-resolution-failure").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	var gatewayKeyID string
	err = pool.QueryRowContext(ctx, `
		INSERT INTO gateway_api_keys (project_id, key_hash, key_prefix, label)
		VALUES ($1, $2, 'gw_live_resfail', 'resolution-failure-test')
		RETURNING id
	`, projectID, uniqueTestKeyHash(t)).Scan(&gatewayKeyID)
	if err != nil {
		t.Fatalf("creating gateway key: %v", err)
	}

	// Deliberately no credentials inserted for this project -- resolveCandidates
	// should fail with "no usable candidates for request" before any
	// candidate is ever tried.
	_, err = HandleChat(ctx, pool, projectID, gatewayKeyID, encryptionKey, Request{
		Model:    "openai/gpt-oss-120b",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}, newLogger, nil)
	if err == nil {
		t.Fatal("expected an error since no credentials exist for this project")
	}

	var count int
	var errorCategory string
	err = pool.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MAX(error_category), '')
		FROM request_logs
		WHERE project_id = $1
	`, projectID).Scan(&count, &errorCategory)
	if err != nil {
		t.Fatalf("querying request_logs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 request_logs row for the resolution failure, got %d", count)
	}
	if errorCategory != "non_retryable" {
		t.Errorf("expected error_category %q, got %q", "non_retryable", errorCategory)
	}
}

func TestHandleChat_EndToEnd_RealGroq(t *testing.T) {
	_ = godotenv.Load("../../.env")

	if os.Getenv("GROQ_API_KEY") == "" {
		t.Skip("GROQ_API_KEY not set, skipping full end-to-end test")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	encryptionKey, err := hex.DecodeString(cfg.EncryptionKey)
	if err != nil {
		t.Fatalf("decoding encryption key: %v", err)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	var projectID string
	err = pool.QueryRowContext(ctx, `
		INSERT INTO projects (name) VALUES ($1) RETURNING id
	`, "test-project-e2e").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	realGroqKey := os.Getenv("GROQ_API_KEY")
	err = credentials.Insert(ctx, pool, projectID, "groq", "e2e-test-key", realGroqKey, encryptionKey)
	if err != nil {
		t.Fatalf("inserting test credential: %v", err)
	}
	out, err := HandleChat(ctx, pool, projectID, "", encryptionKey, Request{
		Messages:    []Message{{Role: "user", Content: "Say the single word: hello"}},
		MaxTokens:   500,
		Temperature: 0.0,
	}, newLogger, nil)
	if err != nil {
		t.Fatalf("HandleChat returned an error: %v", err)
	}

	var fullContent string
	var sawDone bool
	for chunk := range out {
		if chunk.Err != nil {
			t.Fatalf("received error chunk: %v", chunk.Err)
		}
		fullContent += chunk.Content
		if chunk.Done {
			sawDone = true
		}
	}

	if !sawDone {
		t.Error("expected a Done chunk, never received one")
	}
	if fullContent == "" {
		t.Error("expected non-empty content, got empty string")
	}

	t.Logf("End-to-end response: %q", fullContent)
}
