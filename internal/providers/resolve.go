package providers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/Devpaul-01/llm-gateway/internal/cooldown"
	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/redis/go-redis/v9"
)

type modelChoice struct {
	Provider string
	Model    string
}

var defaultPriority = []modelChoice{
	{Provider: "groq", Model: "openai/gpt-oss-120b"},
	{Provider: "mistral", Model: "mistral-large-latest"},
	{Provider: "openrouter", Model: "meta-llama/llama-3.1-8b-instruct"},
}

var modelToProvider = map[string]string{
	"openai/gpt-oss-120b":              "groq",
	"openai/gpt-oss-20b":               "groq",
	"llama-3.3-70b-versatile":          "groq",
	"mistral-large-latest":             "mistral",
	"mistral-medium-latest":            "mistral",
	"ministral-3b-latest":              "mistral",
	"meta-llama/llama-3.1-8b-instruct": "openrouter",
	"gemini-2.5-flash-lite":            "gemini",
}

func inferProviderForModel(model string) (string, bool) {
	provider, found := modelToProvider[model]
	return provider, found
}

func newProviderAdapter(provider, apiKey string) Provider {
	switch provider {
	case "groq":
		return &GroqProvider{APIKey: apiKey, BaseURL: "https://api.groq.com/openai/v1"}
	case "mistral":
		return &MistralProvider{APIKey: apiKey, BaseURL: "https://api.mistral.ai/v1"}
	case "openrouter":
		return &OpenRouterProvider{APIKey: apiKey, BaseURL: "https://openrouter.ai/api/v1"}
	case "gemini":
		return &GeminiProvider{APIKey: apiKey, BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai"}
	default:
		return nil
	}
}

func resolveCandidates(ctx context.Context, db *sql.DB, projectID string, encryptionKey []byte, req Request, logger *slog.Logger, rdb *redis.Client) ([]Candidate, error) {
	var choices []modelChoice

	if req.Model != "" {
		provider := req.Provider
		if provider == "" {
			inferred, found := inferProviderForModel(req.Model)
			if !found {
				return nil, &ProviderError{Category: NonRetryable, Cause: fmt.Errorf("unknown model %q: provider could not be determined, specify it explicitly", req.Model)}
			}
			provider = inferred
		}
		choices = []modelChoice{{Provider: provider, Model: req.Model}}

		for _, fallbackModel := range req.FallbackModels {
			fbProvider, found := inferProviderForModel(fallbackModel)
			if !found {
				logger.Info("skipping unknown fallback model", "model", fallbackModel)
				continue
			}
			choices = append(choices, modelChoice{Provider: fbProvider, Model: fallbackModel})
		}
	} else {
		choices = defaultPriority
	}
	var candidates []Candidate
	for _, choice := range choices {
		creds, err := credentials.GetByProjectAndProvider(ctx, db, projectID, choice.Provider, encryptionKey)
		if err != nil {
			logger.Error("failed to fetch credentials", "provider", choice.Provider, "error", err)
			return nil, err
		}
		logger.Info("resolved credentials", "provider", choice.Provider, "count", len(creds))

		for _, cred := range creds {
			if rdb != nil {
				cooling, err := cooldown.IsCooling(ctx, rdb, cred.ID)
				if err != nil {
					logger.Error("failed to check cooldown state", "credential", cred.ID, "error", err)
				} else if cooling {
					logger.Info("skipping cooling credential", "credential", cred.ID, "provider", choice.Provider)
					continue
				}
			}

			candidates = append(candidates, Candidate{
				Adapter:      newProviderAdapter(choice.Provider, cred.APIKey),
				ProviderName: choice.Provider,
				Model:        choice.Model,
				Label:        fmt.Sprintf("%s:%s", choice.Provider, choice.Model),
				CredentialID: cred.ID,
			})
		}
	}

	if len(candidates) == 0 {
		return nil, &ProviderError{Category: NonRetryable, Cause: fmt.Errorf("no usable candidates for request")}
	}

	return candidates, nil
}
