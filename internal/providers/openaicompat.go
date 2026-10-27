package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// sharedHTTPClient is reused by every adapter so upstream connections are
// pooled and kept alive instead of paying a TLS handshake on each request.
// There is deliberately no overall Timeout: completions stream for a long
// time. Dial/TLS/header timeouts bound the parts that should be fast.
var sharedHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 45 * time.Second,
		ExpectContinueTimeout: time.Second,
	},
}

type usageJSON struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (u *usageJSON) toUsage() *Usage {
	if u == nil {
		return nil
	}
	total := u.TotalTokens
	if total == 0 {
		total = u.PromptTokens + u.CompletionTokens
	}
	return &Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: total}
}

type compatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usageJSON `json:"usage"`
	// Groq reports streaming usage under x_groq.
	XGroq *struct {
		Usage *usageJSON `json:"usage"`
	} `json:"x_groq"`
	// Some providers (OpenRouter, others) report mid-stream failures in-band.
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type compatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type compatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type compatRequestBody struct {
	Model               string               `json:"model"`
	Messages            []compatMessage      `json:"messages"`
	Temperature         *float64             `json:"temperature,omitempty"`
	MaxTokens           int                  `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                  `json:"max_completion_tokens,omitempty"`
	Stream              bool                 `json:"stream"`
	StreamOptions       *compatStreamOptions `json:"stream_options,omitempty"`
}

type bodyOptions struct {
	includeUsage        bool
	maxCompletionTokens bool
}

func buildRequestBody(req Request, stream bool) (string, error) {
	return buildRequestBodyOpts(req, stream, bodyOptions{})
}

func buildRequestBodyOpts(req Request, stream bool, opts bodyOptions) (string, error) {
	messages := make([]compatMessage, len(req.Messages))
	for i, m := range req.Messages {
		if len(m.Images) == 0 {
			messages[i] = compatMessage{Role: m.Role, Content: m.Content}
			continue
		}
		parts := []map[string]interface{}{{"type": "text", "text": m.Content}}
		for _, img := range m.Images {
			parts = append(parts, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]string{"url": img.URL},
			})
		}
		messages[i] = compatMessage{Role: m.Role, Content: parts}
	}

	body := compatRequestBody{
		Model:    req.Model,
		Messages: messages,
		Stream:   stream,
	}
	if req.TemperatureSet {
		t := req.Temperature
		body.Temperature = &t
	}
	if opts.maxCompletionTokens {
		body.MaxCompletionTokens = req.MaxTokens
	} else {
		body.MaxTokens = req.MaxTokens
	}
	if stream && opts.includeUsage {
		body.StreamOptions = &compatStreamOptions{IncludeUsage: true}
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// OpenAICompatProvider talks to any provider that implements the OpenAI
// chat-completions wire format (Groq, Mistral, OpenRouter, Gemini's OpenAI
// endpoint, OpenAI, DeepSeek, Together, Fireworks, Cerebras, xAI, ...).
type OpenAICompatProvider struct {
	Name    string
	APIKey  string
	BaseURL string

	// StreamUsage asks the upstream to append a usage chunk to the stream
	// (stream_options.include_usage). Only enabled for providers known to accept it.
	StreamUsage bool
	// MaxCompletionTokens sends max_completion_tokens instead of the deprecated max_tokens.
	MaxCompletionTokens bool

	Client *http.Client
}

func NewOpenAICompat(name, apiKey, baseURL string) *OpenAICompatProvider {
	return &OpenAICompatProvider{Name: name, APIKey: apiKey, BaseURL: baseURL}
}

func send(ctx context.Context, ch chan<- Chunk, c Chunk) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- c:
		return true
	}
}

func (p *OpenAICompatProvider) Chat(ctx context.Context, req Request) (<-chan Chunk, error) {
	body, err := buildRequestBodyOpts(req, true, bodyOptions{
		includeUsage:        p.StreamUsage,
		maxCompletionTokens: p.MaxCompletionTokens,
	})
	if err != nil {
		return nil, &ProviderError{Category: NonRetryable, Provider: p.Name, Model: req.Model, Cause: fmt.Errorf("building request body: %w", err)}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/chat/completions", strings.NewReader(body))
	if err != nil {
		return nil, &ProviderError{Category: NonRetryable, Provider: p.Name, Model: req.Model, Cause: fmt.Errorf("building http request: %w", err)}
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	client := p.Client
	if client == nil {
		client = sharedHTTPClient
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			// The caller went away; this is not the provider's fault.
			return nil, &ProviderError{Category: NonRetryable, Provider: p.Name, Model: req.Model, Cause: ctx.Err()}
		}
		return nil, &ProviderError{Category: ProviderTransient, Provider: p.Name, Model: req.Model, Cause: fmt.Errorf("sending request: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, &ProviderError{
			Category: classifyStatus(resp.StatusCode),
			Provider: p.Name,
			Model:    req.Model,
			Status:   resp.StatusCode,
			Cause:    fmt.Errorf("%s returned status %d: %s", p.Name, resp.StatusCode, strings.TrimSpace(string(errBody))),
		}
	}

	ch := make(chan Chunk)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		fail := func(cat ErrorCategory, cause error) {
			send(ctx, ch, Chunk{Err: &ProviderError{Category: cat, Provider: p.Name, Model: req.Model, Cause: cause}})
		}

		var usage *Usage
		finishReason := ""
		sawDone := false

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" {
				continue
			}

			if payload == "[DONE]" {
				sawDone = true
				break
			}

			var parsed compatStreamChunk
			if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
				continue
			}

			if parsed.Error != nil {
				fail(ProviderTransient, fmt.Errorf("upstream stream error: %s", parsed.Error.Message))
				return
			}

			if u := parsed.Usage.toUsage(); u != nil {
				usage = u
			} else if parsed.XGroq != nil {
				if u := parsed.XGroq.Usage.toUsage(); u != nil {
					usage = u
				}
			}

			if len(parsed.Choices) == 0 {
				continue
			}
			if fr := parsed.Choices[0].FinishReason; fr != nil && *fr != "" {
				finishReason = *fr
			}
			if content := parsed.Choices[0].Delta.Content; content != "" {
				if !send(ctx, ch, Chunk{Content: content}) {
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			if ctx.Err() == nil {
				fail(ProviderTransient, err)
			}
			return
		}

		// A stream that ends without [DONE] and without a finish_reason was cut
		// off. Reporting it as a failure (instead of silently ending) is what
		// stops the failover engine from splicing a second model's answer onto
		// a truncated first one.
		if !sawDone && finishReason == "" {
			if ctx.Err() == nil {
				fail(ProviderTransient, errors.New("stream ended before completion"))
			}
			return
		}

		send(ctx, ch, Chunk{Done: true, Usage: usage, FinishReason: finishReason})
	}()

	return ch, nil
}
