package providers

import (
	"context"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestMistralProvider_LiveChatCompletion(t *testing.T) {
	_ = godotenv.Load("../../.env")

	apiKey := os.Getenv("MISTRAL_API_KEY")
	if apiKey == "" {
		t.Skip("MISTRAL_API_KEY not set, skipping live mistral integration test")
	}

	gp := &MistralProvider{
		APIKey:  apiKey,
		BaseURL: "https://api.mistral.ai/v1",
	}

	ch, err := gp.Chat(context.Background(), Request{
		Model:       "mistral-large-latest",
		Messages:    []Message{{Role: "user", Content: "Say the single word: hello"}},
		MaxTokens:   20,
		Temperature: 0.0,
	})
	if err != nil {
		t.Fatalf("unexpected error calling Mistral: %v", err)
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
		t.Error("expected non-empty content from mistral, got empty string")
	}

	t.Logf("mistral responded: %q", fullContent)
}
