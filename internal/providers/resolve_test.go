package providers

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/cooldown"
	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
	"github.com/joho/godotenv"
)

func TestFilterVisionCapable_KeepsOnlyVisionModels(t *testing.T) {
	candidates := []Candidate{
		{Model: "openai/gpt-oss-120b", Label: "groq:text-only"},
		{Model: "gemini-2.5-flash-lite", Label: "gemini:vision"},
		{Model: "mistral-large-latest", Label: "mistral:vision"},
	}

	got := filterVisionCapable(candidates, testLogger)

	if len(got) != 2 {
		t.Fatalf("expected 2 vision-capable candidates, got %d", len(got))
	}
	for _, c := range got {
		if c.Label == "groq:text-only" {
			t.Errorf("text-only candidate should have been filtered out")
		}
	}
}

func TestFilterVisionCapable_DoesNotMutateInput(t *testing.T) {
	candidates := []Candidate{
		{Model: "openai/gpt-oss-120b", Label: "a"},
		{Model: "gemini-2.5-flash-lite", Label: "b"},
	}

	_ = filterVisionCapable(candidates, testLogger)

	if candidates[0].Label != "a" || candidates[1].Label != "b" {
		t.Errorf("filter mutated its input slice: %+v", candidates)
	}
}

func TestBuildRequestBody_SendsImagesAsContentParts(t *testing.T) {
	body, err := buildRequestBody(Request{
		Model: "m",
		Messages: []Message{
			{Role: "user", Content: "what is this?", Images: []Image{{URL: "data:image/png;base64,AAA"}}},
		},
	}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(body, `"type":"image_url"`) || !strings.Contains(body, "data:image/png;base64,AAA") {
		t.Errorf("expected image part in request body, got %s", body)
	}
}

func TestBuildRequestBody_PlainTextStaysAString(t *testing.T) {
	body, err := buildRequestBody(Request{
		Model:    "m",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(body, `"content":"hello"`) {
		t.Errorf("expected plain string content for text-only message, got %s", body)
	}
}

func TestResolveCandidates_ExpandsFallbackModels(t *testing.T) {
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

	encryptionKey, err := hex.DecodeString(cfg.EncryptionKey)
	if err != nil {
		t.Fatalf("decoding encryption key: %v", err)
	}

	var projectID string
	err = pool.QueryRowContext(ctx, `INSERT INTO projects (name) VALUES ($1) RETURNING id`, "test-fallback-models").Scan(&projectID)
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	defer pool.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)

	if err := credentials.Insert(ctx, pool, projectID, "groq", "fb-test", "fake-groq-key", encryptionKey); err != nil {
		t.Fatalf("inserting groq credential: %v", err)
	}
	if err := credentials.Insert(ctx, pool, projectID, "mistral", "fb-test", "fake-mistral-key", encryptionKey); err != nil {
		t.Fatalf("inserting mistral credential: %v", err)
	}

	candidates, err := resolveCandidates(ctx, pool, projectID, encryptionKey, Request{
		Model:          "openai/gpt-oss-120b",
		FallbackModels: []string{"mistral-large-latest"},
	}, testLogger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates (primary + fallback), got %d", len(candidates))
	}
	if candidates[0].Model != "openai/gpt-oss-120b" || candidates[0].ProviderName != "groq" {
		t.Errorf("expected first candidate to be groq/openai/gpt-oss-120b, got %s/%s", candidates[0].ProviderName, candidates[0].Model)
	}
	if candidates[1].Model != "mistral-large-latest" || candidates[1].ProviderName != "mistral" {
		t.Errorf("expected second candidate to be mistral/mistral-large-latest, got %s/%s", candidates[1].ProviderName, candidates[1].Model)
	}
}
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
