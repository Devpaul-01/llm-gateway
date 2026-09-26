package providers

import (
	"context"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestOpenRouterProvider_LiveChatCompletion(t *testing.T) {
	_ = godotenv.Load("../../.env")

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set, skipping live OpenRouter integration test")
	}

	op := &OpenRouterProvider{
		APIKey:  apiKey,
		BaseURL: "https://openrouter.ai/api/v1",
	}

	ch, err := op.Chat(context.Background(), Request{
		Model:       "meta-llama/llama-3.1-8b-instruct",
		Messages:    []Message{{Role: "user", Content: "Say the single word: hello"}},
		MaxTokens:   20,
		Temperature: 0.0,
	})
	if err != nil {
		t.Fatalf("unexpected error calling OpenRouter: %v", err)
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
		t.Error("expected non-empty content from OpenRouter, got empty string")
	}

	t.Logf("OpenRouter responded: %q", fullContent)
}