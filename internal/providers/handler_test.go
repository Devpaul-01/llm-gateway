package providers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestHandleChat_StopsOnMidStreamFailure(t *testing.T) {
	fp := &FakeProvider{
		Chunks: []Chunk{
			{Content: "oluwa"},
			{Content: "oluwa"},
			{Err: &ProviderError{Category: ProviderTransient, Cause: errors.New("something failed")}},
		},
	}
	fp2 := &FakeProvider{
		Chunks: []Chunk{
			{Content: "from fp2"},
			{Done: true},
		},
	}
	out, err := handleChatWithCandidates(context.Background(), Request{}, []Candidate{
		{Provider: fp, Model: "model-a", Label: "candidate-1"},
		{Provider: fp2, Model: "model-a", Label: "candidate-2"},
	}, requestLogContext{}, testLogger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var received []Chunk
	for chunk := range out {
		if chunk.Content == "from fp2" {
			t.Fatalf("fp2 content shoule not be received")
		}
		received = append(received, chunk)
	}

	if received[len(received)-1].Err == nil {
		t.Errorf("expected final chunk to carry an error")
	}

	if len(received) != 3 {
		t.Fatalf("total chunks received should be 3")
	}
}
func TestHandleChat_FailsOverAfterPreStreamError(t *testing.T) {
	fp1 := &FakeProvider{
		Err: errors.New("connection refused"),
	}
	fp2 := &FakeProvider{
		Chunks: []Chunk{
			{Content: "from fp2"},
			{Done: true},
		},
	}

	out, err := handleChatWithCandidates(context.Background(), Request{}, []Candidate{
		{Provider: fp1, Model: "model-a", Label: "candidate-1"},
		{Provider: fp2, Model: "model-b", Label: "candidate-2"},
	}, requestLogContext{}, testLogger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var received []Chunk
	for chunk := range out {
		received = append(received, chunk)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(received))
	}
	if received[0].Content != "from fp2" {
		t.Errorf("expected content from fp2, got %q", received[0].Content)
	}
	if !received[1].Done {
		t.Errorf("expected final chunk to be Done")
	}
}

func TestHandleChat_SingleCandidateSuccess(t *testing.T) {
	// Directly construct a Candidate rather than going through
	// resolveCandidates, so this test doesn't depend on the temporary
	// stub's contents.
	fp := &FakeProvider{
		Chunks: []Chunk{
			{Content: "hi"},
			{Done: true},
		},
	}

	out, err := handleChatWithCandidates(context.Background(), Request{}, []Candidate{
		{Provider: fp, Model: "test-model", Label: "test"},
	}, requestLogContext{}, testLogger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var received []Chunk
	for chunk := range out {
		received = append(received, chunk)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(received))
	}
	if received[0].Content != "hi" {
		t.Errorf("expected content %q, got %q", "hi", received[0].Content)
	}
	if !received[1].Done {
		t.Errorf("expected final chunk to be Done")
	}
}
