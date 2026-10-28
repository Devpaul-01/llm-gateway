package providers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/Devpaul-01/llm-gateway/internal/cooldown"
	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/discovery"
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

// modelToProvider maps every model the gateway can route to by name back
// to the provider that serves it. Finding B-4: this previously omitted
// "meta-llama/llama-4-scout-17b-16e-instruct" entirely, which meant the
// one model Architecture Section 8.3 specifically calls out as Groq's
// vision-capable option could never resolve via inferProviderForModel --
// a client requesting it by name with no explicit provider field always
// hit the "unknown model" NonRetryable error before any candidate was
// built. Added below.
var modelToProvider = map[string]string{
	"openai/gpt-oss-120b":                       "groq",
	"openai/gpt-oss-20b":                        "groq",
	"llama-3.3-70b-versatile":                   "groq",
	"meta-llama/llama-4-scout-17b-16e-instruct": "groq",
	"mistral-large-latest":                      "mistral",
	"mistral-medium-latest":                     "mistral",
	"ministral-3b-latest":                       "mistral",
	"meta-llama/llama-3.1-8b-instruct":          "openrouter",
	"gemini-2.5-flash-lite":                     "gemini",
}

var visionCapableModels = map[string]bool{
	"meta-llama/llama-4-scout-17b-16e-instruct": true,
	"gemini-2.5-flash-lite":                     true,
	"mistral-large-latest":                      true,
}

func isVisionCapable(model string) bool {
	return visionCapableModels[model]
}
func requestHasImages(req Request) bool {
	for _, m := range req.Messages {
		if len(m.Images) > 0 {
			return true
		}
	}
	return false
}
func filterVisionCapable(candidates []Candidate, logger *slog.Logger) []Candidate {
	filtered := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if isVisionCapable(c.Model) {
			filtered = append(filtered, c)
		} else {
			logger.Info("skipping non-vision candidate for request with images", "candidate", c.Label)
		}
	}
	return filtered
}

func inferProviderForModel(model string) (string, bool) {
	provider, found := modelToProvider[model]
	return provider, found
}

// refreshDiscoveryForTierMode calls discovery.GetModels for a provider
// when operating in tier/auto mode, so the Redis-backed model-discovery
// cache (internal/discovery) actually gets populated and refreshed via
// live traffic instead of sitting permanently empty.
//
// This was previously fully implemented (internal/discovery.GetModels)
// but never called from anywhere in the routing path -- EvictModel and
// RecordProviderTransient were wired into the failover loop in handler.go,
// but nothing ever primed or refreshed the cache those two functions
// operate on, so eviction/health-recording had nothing real to act
// against (Audit §7.1). This is scoped to tier/auto mode only: explicit-
// model requests already name an exact model and provider via
// inferProviderForModel/modelToProvider, so discovery has nothing to add
// there -- it only matters for the open-ended "give me whatever this
// provider currently supports" case tier mode represents.
//
// A discovery failure or empty result is not an error for resolution
// purposes: GetModels already falls back to the static priority list
// internally (lock contention, fetch failure, or a genuine empty
// response), so this function only matters for its side effect of
// keeping the cache warm; the candidate list itself still comes from the
// existing static defaultPriority chain either way, to avoid making
// candidate resolution's behavior depend on a live third-party HTTP call
// succeeding.
func refreshDiscoveryForTierMode(ctx context.Context, rdb *redis.Client, provider, apiKey string, fallback []string) {
	if rdb == nil || apiKey == "" {
		return
	}
	spec, ok := providerRegistry[provider]
	if !ok {
		return
	}
	discovery.GetModels(ctx, rdb, provider, spec.BaseURL, apiKey, fallback)
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

		// Keep the discovery cache warm for tier/auto-mode requests, using
		// the first healthy credential found for this provider. This is a
		// best-effort cache refresh, not a correctness dependency: if it's
		// skipped or fails, routing still proceeds on the static
		// defaultPriority list exactly as before. Explicit-model requests
		// (req.Model != "") don't need this, since the client has already
		// named the model directly.
		if req.Model == "" && rdb != nil && len(creds) > 0 {
			refreshDiscoveryForTierMode(ctx, rdb, choice.Provider, creds[0].APIKey, []string{choice.Model})
		}

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

	if requestHasImages(req) {
		candidates = filterVisionCapable(candidates, logger)
		if len(candidates) == 0 {
			return nil, &ProviderError{Category: NonRetryable, Cause: fmt.Errorf("request contains images but no vision-capable model is available for this project")}
		}
	}

	return candidates, nil
}
