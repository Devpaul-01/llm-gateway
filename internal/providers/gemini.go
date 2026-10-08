package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// geminiStreamChunk mirrors the OpenAI-compatible SSE chunk shape Gemini's
// compatibility endpoint actually returns. This was previously an alias
// for mistral.go's mistralStreamChunk (same shape, different name) with no
// Usage field read at all.
//
// Research note (requested verification, Audit Finding B-5): Gemini's
// /v1beta/openai compatibility endpoint does honor stream_options:
// {"include_usage": true} and returns a trailing chunk with an empty (or
// absent) choices array and a populated usage object -- same pattern as
// Groq/Mistral/OpenRouter. Without explicitly requesting it, Gemini
// returns no usage at all on the stream, which is what this adapter was
// previously doing (buildRequestBody never set stream_options, and this
// file never read a Usage field even if one had come back). That silently
// starved every Gemini chunk's Usage pointer, which in turn fed directly
// into the token-budget reservation/reconciliation path now relying on
// real tokensOut (see internal/budget's Adjust and handler.go). Chunk
// granularity for Gemini is coarser (per-sentence rather than per-token)
// per provider behavior, but that only affects delivery cadence, not
// parsing correctness -- accumulating deltas as they arrive still works.
type geminiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type GeminiProvider struct {
	APIKey  string
	BaseURL string
}

// buildGeminiRequestBody is buildRequestBody's shape plus
// stream_options.include_usage, which Gemini's compatibility endpoint
// requires in order to return a usage object on the stream at all.
func buildGeminiRequestBody(req Request) (string, error) {
	messages := make([]groqMessage, len(req.Messages))
	for i, m := range req.Messages {
		if len(m.Images) == 0 {
			messages[i] = groqMessage{Role: m.Role, Content: m.Content}
			continue
		}
		parts := []map[string]interface{}{{"type": "text", "text": m.Content}}
		for _, img := range m.Images {
			parts = append(parts, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]string{"url": img.URL},
			})
		}
		messages[i] = groqMessage{Role: m.Role, Content: parts}
	}

	body := struct {
		Model         string        `json:"model"`
		Messages      []groqMessage `json:"messages"`
		Temperature   float64       `json:"temperature"`
		MaxTokens     int           `json:"max_tokens"`
		Stream        bool          `json:"stream"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
	}
	body.StreamOptions.IncludeUsage = true

	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (g *GeminiProvider) Chat(ctx context.Context, req Request) (<-chan Chunk, error) {
	body, err := buildGeminiRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/chat/completions", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building http request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+g.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &ProviderError{
			Category: classifyStatus(resp.StatusCode),
			Provider: "gemini",
			Model:    req.Model,
			Status:   resp.StatusCode,
			Cause:    fmt.Errorf("gemini returned status %d: %s", resp.StatusCode, string(errBody)),
		}
	}

	ch := make(chan Chunk)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		var pendingUsage *Usage

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")

			if payload == "[DONE]" {
				select {
				case <-ctx.Done():
				case ch <- Chunk{Done: true, Usage: pendingUsage}:
				}
				return
			}

			var parsed geminiStreamChunk
			if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
				continue
			}

			if parsed.Usage != nil {
				pendingUsage = &Usage{
					PromptTokens:     parsed.Usage.PromptTokens,
					CompletionTokens: parsed.Usage.CompletionTokens,
					TotalTokens:      parsed.Usage.TotalTokens,
				}
			}

			if len(parsed.Choices) == 0 {
				continue
			}

			content := parsed.Choices[0].Delta.Content
			if content != "" {
				select {
				case <-ctx.Done():
					return
				case ch <- Chunk{Content: content}:
				}
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case <-ctx.Done():
			case ch <- Chunk{Err: &ProviderError{Category: ProviderTransient, Provider: "gemini", Model: req.Model, Cause: err}}:
			}
		}
	}()

	return ch, nil
}
