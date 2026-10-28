package providers

import (
	"crypto/rand"
	"encoding/hex"
)

type Message struct {
	Role    string
	Content string
	Images  []Image
}

type Image struct {
	URL string
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Request struct {
	Messages []Message
	Model    string
	// Temperature is only forwarded upstream when TemperatureSet is true, so a
	// client that omits it gets the provider's own default instead of a silent 0.
	Temperature       float64
	TemperatureSet    bool
	Provider          string
	MaxTokens         int
	FallbackModels    []string
	ContinueOnFailure bool
}

// Chunk is the unit that flows from provider adapters, through the failover
// engine, to the HTTP layer.
//
// Adapters emit content chunks followed by exactly one Done chunk (carrying
// Usage and FinishReason). The failover engine swallows the adapter's Done and
// emits its own single final Done chunk carrying ModelsUsed and a Summary.
type Chunk struct {
	Content      string
	Done         bool
	Usage        *Usage
	FinishReason string
	Err          *ProviderError
	ModelsUsed   []ModelUsage

	// Provider and Model are set by the failover engine on forwarded content
	// chunks so the HTTP layer can report which upstream actually answered.
	Provider string
	Model    string

	// Summary is only set on the final Done chunk emitted by the engine.
	Summary *Summary
}

// Summary describes how a request was served. It is what powers the usage and
// timing data returned to clients and the metrics shown on the dashboard.
type Summary struct {
	RequestID      string
	Provider       string
	Model          string
	FinishReason   string
	Usage          Usage
	UsageEstimated bool
	TTFTMs         *int
	TotalMs        int
	Attempts       int
	Failovers      int
	CacheHit       bool
	ModelsUsed     []ModelUsage
}

func NewRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}
