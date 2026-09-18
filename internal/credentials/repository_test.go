package credentials

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/joho/godotenv"
)

func TestInsertAndGet_RoundTrip(t *testing.T) {
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
	`, "test-project-for-credentials").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	err = Insert(ctx, pool, projectID, "groq", "test-label", "gsk_fake_test_key_12345", encryptionKey)
	if err != nil {
		t.Fatalf("inserting credential: %v", err)
	}

	results, err := GetByProjectAndProvider(ctx, pool, projectID, "groq", encryptionKey)
	if err != nil {
		t.Fatalf("getting credential: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(results))
	}

	if results[0].APIKey != "gsk_fake_test_key_12345" {
		t.Errorf("expected decrypted API key %q, got %q", "gsk_fake_test_key_12345", results[0].APIKey)
	}

	if results[0].Provider != "groq" {
		t.Errorf("expected provider %q, got %q", "groq", results[0].Provider)
	}

}
