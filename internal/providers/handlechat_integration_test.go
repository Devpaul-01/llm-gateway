package providers

import (
	"context"
	"encoding/hex"
	"os"
	"testing"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/joho/godotenv"
)

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
		out, err := HandleChat(ctx, pool, projectID, encryptionKey, Request{
		Messages: []Message{{Role: "user", Content: "Say the single word: hello"}},
		MaxTokens: 20,
		Temperature: 0.0,
	})
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