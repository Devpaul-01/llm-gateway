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

func TestRevoke_ExcludesCredentialFromActiveLookup(t *testing.T) {
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
	`, "test-project-for-revoke").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	if err := Insert(ctx, pool, projectID, "groq", "revoke-me", "gsk_fake_revoke_test", encryptionKey); err != nil {
		t.Fatalf("inserting credential: %v", err)
	}

	before, err := GetByProjectAndProvider(ctx, pool, projectID, "groq", encryptionKey)
	if err != nil || len(before) != 1 {
		t.Fatalf("expected 1 active credential before revoke, got %d, err: %v", len(before), err)
	}
	credentialID := before[0].ID

	affected, err := Revoke(ctx, pool, projectID, credentialID)
	if err != nil {
		t.Fatalf("revoking credential: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 row affected by revoke, got %d", affected)
	}

	after, err := GetByProjectAndProvider(ctx, pool, projectID, "groq", encryptionKey)
	if err != nil {
		t.Fatalf("getting credentials after revoke: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected 0 active credentials after revoke, got %d", len(after))
	}

	meta, err := ListByProject(ctx, pool, projectID)
	if err != nil {
		t.Fatalf("listing credentials: %v", err)
	}
	if len(meta) != 1 {
		t.Fatalf("expected 1 credential in metadata listing (revoked ones still listed), got %d", len(meta))
	}
	if meta[0].Status != "revoked" {
		t.Errorf("expected status %q, got %q", "revoked", meta[0].Status)
	}

	// Revoking again should affect 0 rows, not error.
	affectedAgain, err := Revoke(ctx, pool, projectID, credentialID)
	if err != nil {
		t.Fatalf("unexpected error re-revoking: %v", err)
	}
	if affectedAgain != 0 {
		t.Errorf("expected 0 rows affected re-revoking an already-revoked credential, got %d", affectedAgain)
	}
}
