package providers

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Devpaul-01/llm-gateway/internal/credentials"
)

type modelChoice struct {
	Provider string
	Model    string
}

var defaultPriority = []modelChoice{
	{Provider: "groq", Model: "openai/gpt-oss-120b"},
}

// inferProviderForModel is a temporary, Groq-only mapping. Once more
// providers are added, this needs a real model->provider lookup table
// (or the client should be able to name the provider explicitly).
func inferProviderForModel(model string) string {
	return "groq"
}

// newProviderAdapter constructs the right Provider implementation for a
// given provider name. Only "groq" is implemented so far; this grows
// into a real switch/registry as more adapters are built.
func newProviderAdapter(provider, apiKey string) Provider {
	switch provider {
	case "groq":
		return &GroqProvider{APIKey: apiKey, BaseURL: "https://api.groq.com/openai/v1"}
	default:
		return nil
	}
}
func resolveCandidates(ctx context.Context, db *sql.DB, projectID string, encryptionKey []byte, req Request) ([]Candidate, error) {
	var choices []modelChoice

	if req.Model != "" {
		choices = []modelChoice{{Provider: inferProviderForModel(req.Model), Model: req.Model}}
	} else {
		choices = defaultPriority
	}

	var candidates []Candidate
	for _, choice := range choices {
		creds, err := credentials.GetByProjectAndProvider(ctx, db, projectID, choice.Provider, encryptionKey)
		if err != nil {
			return nil, err
		}

		for _, cred := range creds {
			candidates = append(candidates, Candidate{
				Provider: newProviderAdapter(choice.Provider, cred.APIKey),
				Model:    choice.Model,
				Label:    fmt.Sprintf("%s:%s", choice.Provider, choice.Model),
			})
		}
	}

	if len(candidates) == 0 {
		return nil, &ProviderError{Category: NonRetryable, Cause: fmt.Errorf("no usable candidates for request")}
	}

	return candidates, nil
}
