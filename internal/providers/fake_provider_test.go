package providers

import (
	"context"
	"testing"
)

func TestFakeProvider_DeliversChunks(t *testing.T) {
	fp := &FakeProvider{
		Chunks: []Chunk{
			{Content: "hello"},
			{Content: " world"},
			{Done: true},
		},
	}

	ch, err := fp.Chat(context.Background(), Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var received []Chunk
	for chunk := range ch {
		received = append(received, chunk)
	}

	if len(received) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(received))
	}

	if received[0].Content != "hello" {
		t.Errorf("expected first chunk content %q, got %q", "hello", received[0].Content)
	}

	if received[1].Content != " world" {
		t.Errorf("expected second chunk content %q, got %q", " world", received[1].Content)
	}

	if !received[2].Done {
		t.Errorf("expected third chunk to be Done")
	}
}
