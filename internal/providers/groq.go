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

type groqStreamChunk struct {
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
type groqRequestBody struct {
	Model       string        `json:"model"`
	Messages    []groqMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
}

type GroqProvider struct {
	APIKey  string
	BaseURL string
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func buildRequestBody(req Request, stream bool) (string, error) {
	messages := make([]groqMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = groqMessage{Role: m.Role, Content: m.Content}
	}

	body := groqRequestBody{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      stream,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (g *GroqProvider) Chat(ctx context.Context, req Request) (<-chan Chunk, error) {
	body, err := buildRequestBody(req, true)
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
			Provider: "groq",
			Model:    req.Model,
			Status:   resp.StatusCode,
			Cause:    fmt.Errorf("groq returned status %d: %s", resp.StatusCode, string(errBody)),
		}
	}

	ch := make(chan Chunk)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
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
				case ch <- Chunk{Done: true}:
				}
				return
			}

			var parsed groqStreamChunk
			if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
				continue
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
			case ch <- Chunk{Err: &ProviderError{Category: ProviderTransient, Provider: "groq", Model: req.Model, Cause: err}}:
			}
		}
	}()

	return ch, nil
}

func classifyStatus(status int) ErrorCategory {
	switch {
	case status == 401 || status == 403 || status == 429:
		return KeyFault
	case status == 404:
		return BadModel
	case status >= 500:
		return ProviderTransient
	default:
		return NonRetryable
	}
}
