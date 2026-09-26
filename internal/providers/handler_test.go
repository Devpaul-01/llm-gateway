package providers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

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
