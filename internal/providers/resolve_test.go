package providers

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/cooldown"
	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
	"github.com/joho/godotenv"
)

func TestResolveCandidates_SkipsCoolingCredential(t *testing.T) {
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

	rdb, err := redisclient.Connect(ctx, cfg.RedisURL)
	if err != nil {
		t.Fatalf("connecting to redis: %v", err)
	}
	defer rdb.Close()

	encryptionKey, err := hex.DecodeString(cfg.EncryptionKey)
	if err != nil {
		t.Fatalf("decoding encryption key: %v", err)
	}

	var projectID string
	err = pool.QueryRowContext(ctx, `INSERT INTO projects (name) VALUES ($1) RETURNING id`, "test-cooldown-project").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	if err := credentials.Insert(ctx, pool, projectID, "groq", "cooldown-test", "fake-key-not-real", encryptionKey); err != nil {
		t.Fatalf("inserting credential: %v", err)
	}

	creds, err := credentials.GetByProjectAndProvider(ctx, pool, projectID, "groq", encryptionKey)
	if err != nil || len(creds) != 1 {
		t.Fatalf("expected exactly 1 credential, got %d, err: %v", len(creds), err)
	}
	credentialID := creds[0].ID
	defer rdb.Del(ctx, "cooldown:"+credentialID)

	if err := cooldown.MarkFailed(ctx, rdb, credentialID); err != nil {
		t.Fatalf("marking credential as failed: %v", err)
	}

	candidates, err := resolveCandidates(ctx, pool, projectID, encryptionKey, Request{Model: "openai/gpt-oss-120b"}, testLogger, rdb)
	if err != nil {
		if len(candidates) != 0 {
			t.Errorf("expected no candidates due to cooldown, got %d", len(candidates))
		}
	} else if len(candidates) != 0 {
		t.Errorf("expected the cooling credential to be skipped, but got %d candidates", len(candidates))
	}
}
