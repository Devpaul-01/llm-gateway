package providers

import (
	"context"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestGroqProvider_LiveChatCompletion(t *testing.T) {
	_ = godotenv.Load("../../.env")

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		t.Skip("GROQ_API_KEY not set, skipping live Groq integration test")
	}

	gp := &GroqProvider{
		APIKey:  apiKey,
		BaseURL: "https://api.groq.com/openai/v1",
	}

	ch, err := gp.Chat(context.Background(), Request{
		Model: "openai/gpt-oss-120b",
		Messages:    []Message{{Role: "user", Content: "Say the single word: hello"}},
		MaxTokens:   20,
		Temperature: 0.0,
	})
	if err != nil {
		t.Fatalf("unexpected error calling Groq: %v", err)
	}

	var fullContent string
	var sawDone bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("received error chunk: %v", chunk.Err)
		}
		fullContent += chunk.Content
		if chunk.Done {
			sawDone = true
		}
	}

	if !sawDone {
		t.Error("expected to see a Done chunk, never received one")
	}
	if fullContent == "" {
		t.Error("expected non-empty content from Groq, got empty string")
	}

	t.Logf("Groq responded: %q", fullContent)
}
