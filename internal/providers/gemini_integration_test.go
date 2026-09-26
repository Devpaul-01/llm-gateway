package providers

import (
	"context"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestGeminiProvider_LiveChatCompletion(t *testing.T) {
	_ = godotenv.Load("../../.env")

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set, skipping live Gemini integration test")
	}

	gp := &GeminiProvider{
		APIKey:  apiKey,
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
	}

	ch, err := gp.Chat(context.Background(), Request{
		Model:       "gemini-2.5-flash-lite",
		Messages:    []Message{{Role: "user", Content: "Say the single word: hello"}},
		MaxTokens:   20,
		Temperature: 0.0,
	})
	if err != nil {
		t.Fatalf("unexpected error calling Gemini: %v", err)
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
		t.Error("expected non-empty content from Gemini, got empty string")
	}

	t.Logf("Gemini responded: %q", fullContent)
}