package gatewaykeys

import (
	"context"
	"testing"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/joho/godotenv"
)

func TestInsertAndLookup(t *testing.T) {
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
	err = pool.QueryRowContext(ctx, `
		INSERT INTO projects (name) VALUES ($1) RETURNING id
	`, "test-project-gatewaykeys").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	plaintext, keyID, err := Insert(ctx, pool, projectID, "test-key")
	if err != nil {
		t.Fatalf("inserting key: %v", err)
	}
	if plaintext == "" || keyID == "" {
		t.Fatal("expected non-empty plaintext and keyID")
	}

	found, err := LookupByPlaintext(ctx, pool, plaintext)
	if err != nil {
		t.Fatalf("unexpected lookup error: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find the key, got nil")
	}
	if found.ProjectID != projectID {
		t.Errorf("expected ProjectID %q, got %q", projectID, found.ProjectID)
	}

	notFound, err := LookupByPlaintext(ctx, pool, "gw_live_totally_wrong_key")
	if err != nil {
		t.Fatalf("unexpected error on wrong key lookup: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for a nonexistent key, got a result")
	}
}