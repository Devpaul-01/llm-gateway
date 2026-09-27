package providers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestHandleChat_AbortsImmediatelyOnNonRetryable(t *testing.T) {
	fp1 := &FakeProvider{
		Err: &ProviderError{Category: NonRetryable, Cause: errors.New("malformed request")},
	}
	fp2 := &FakeProvider{
		Chunks: []Chunk{
			{Content: "should never be reached"},
			{Done: true},
		},
	}

	out, err := handleChatWithCandidates(context.Background(), Request{}, []Candidate{
		{Adapter: fp1, ProviderName: "groq", Model: "model-a", Label: "candidate-1"},
		{Adapter: fp2, ProviderName: "mistral", Model: "model-b", Label: "candidate-2"},
	}, requestLogContext{}, testLogger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var received []Chunk
	for chunk := range out {
		if chunk.Content == "should never be reached" {
			t.Fatalf("fp2 should never have been tried after a NonRetryable failure on fp1")
		}
		received = append(received, chunk)
	}

	if len(received) != 1 {
		t.Fatalf("expected exactly 1 chunk (the error), got %d", len(received))
	}
	if received[0].Err == nil {
		t.Fatalf("expected the received chunk to carry an error")
	}
	if received[0].Err.Category != NonRetryable {
		t.Errorf("expected error category NonRetryable, got %v", received[0].Err.Category)
	}
}

func TestHandleChat_SingleCandidateSuccess(t *testing.T) {
	fp := &FakeProvider{
		Chunks: []Chunk{
			{Content: "hi"},
			{Done: true},
		},
	}

	out, err := handleChatWithCandidates(context.Background(), Request{}, []Candidate{
		{Adapter: fp, ProviderName: "test", Model: "test-model", Label: "test"},
	}, requestLogContext{}, testLogger, nil)
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
		{Adapter: fp1, ProviderName: "candidate-1", Model: "model-a", Label: "candidate-1"},
		{Adapter: fp2, ProviderName: "candidate-2", Model: "model-b", Label: "candidate-2"},
	}, requestLogContext{}, testLogger, nil)
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
		{Adapter: fp, ProviderName: "candidate-1", Model: "model-a", Label: "candidate-1"},
		{Adapter: fp2, ProviderName: "candidate-2", Model: "model-a", Label: "candidate-2"},
	}, requestLogContext{}, testLogger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var received []Chunk
	for chunk := range out {
		if chunk.Content == "from fp2" {
			t.Fatalf("fp2 content should not be received")
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

func TestHandleChat_ContinuesOnFailureWhenOptedIn(t *testing.T) {
	fp1 := &FakeProvider{
		Chunks: []Chunk{
			{Content: "Hello, "},
			{Err: &ProviderError{Category: ProviderTransient, Cause: errors.New("connection dropped")}},
		},
	}
	fp2 := &FakeProvider{
		Chunks: []Chunk{
			{Content: "world!"},
			{Done: true},
		},
	}

	out, err := handleChatWithCandidates(context.Background(), Request{ContinueOnFailure: true}, []Candidate{
		{Adapter: fp1, ProviderName: "groq", Model: "model-a", Label: "candidate-1"},
		{Adapter: fp2, ProviderName: "mistral", Model: "model-b", Label: "candidate-2"},
	}, requestLogContext{}, testLogger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var received []Chunk
	for chunk := range out {
		received = append(received, chunk)
	}

	var fullContent string
	var lastChunk Chunk
	for _, c := range received {
		fullContent += c.Content
		lastChunk = c
	}

	if fullContent != "Hello, world!" {
		t.Errorf("expected combined content %q, got %q", "Hello, world!", fullContent)
	}

	if !lastChunk.Done {
		t.Errorf("expected final chunk to be Done")
	}

	if len(lastChunk.ModelsUsed) != 2 {
		t.Fatalf("expected ModelsUsed to have 2 entries, got %d: %v", len(lastChunk.ModelsUsed), lastChunk.ModelsUsed)
	}
	if lastChunk.ModelsUsed[0].Provider != "groq" || lastChunk.ModelsUsed[1].Provider != "mistral" {
		t.Errorf("expected ModelsUsed providers [groq mistral], got %v", lastChunk.ModelsUsed)
	}
}

func TestHandleChat_ContinuationThenPreStreamFailureFallsThrough(t *testing.T) {
	fp1 := &FakeProvider{
		Chunks: []Chunk{
			{Content: "Partial. "},
			{Err: &ProviderError{Category: ProviderTransient, Cause: errors.New("dropped")}},
		},
	}
	fp2 := &FakeProvider{
		Err: errors.New("fp2 connection refused"),
	}
	fp3 := &FakeProvider{
		Chunks: []Chunk{
			{Content: "Recovered."},
			{Done: true},
		},
	}

	out, err := handleChatWithCandidates(context.Background(), Request{ContinueOnFailure: true}, []Candidate{
		{Adapter: fp1, ProviderName: "groq", Model: "model-a", Label: "candidate-1"},
		{Adapter: fp2, ProviderName: "mistral", Model: "model-b", Label: "candidate-2"},
		{Adapter: fp3, ProviderName: "openrouter", Model: "model-c", Label: "candidate-3"},
	}, requestLogContext{}, testLogger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fullContent string
	var lastChunk Chunk
	for chunk := range out {
		fullContent += chunk.Content
		lastChunk = chunk
	}

	if fullContent != "Partial. Recovered." {
		t.Errorf("expected combined content %q, got %q", "Partial. Recovered.", fullContent)
	}
	if !lastChunk.Done {
		t.Errorf("expected final chunk to be Done")
	}
}
