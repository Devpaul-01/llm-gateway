package providers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"

	"github.com/Devpaul-01/llm-gateway/internal/breaker"
	"github.com/Devpaul-01/llm-gateway/internal/cooldown"
	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/stats"
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

	// Convenience shortcuts so common models need no explicit "provider".
	// Any model on any registered provider also works via the request's
	// "provider" field or the "provider:model" form (e.g. "cerebras:llama3.1-8b").
	"gpt-4o":            "openai",
	"gpt-4o-mini":       "openai",
	"gpt-4.1":           "openai",
	"gpt-4.1-mini":      "openai",
	"deepseek-chat":     "deepseek",
	"deepseek-reasoner": "deepseek",
	"llama3.1-8b":       "cerebras",
	"gpt-oss-120b":      "cerebras",
}
var visionCapableModels = map[string]bool{
	"meta-llama/llama-4-scout-17b-16e-instruct": true,
	"gemini-2.5-flash-lite":                     true,
	"mistral-large-latest":                      true,
	"gpt-4o":                                    true,
	"gpt-4o-mini":                               true,
	"gpt-4.1":                                   true,
	"gpt-4.1-mini":                              true,
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

// parseModelRef splits an optional "provider:model" reference. The prefix only
// counts when it is a registered provider, so model IDs that legitimately
// contain a colon (e.g. OpenRouter's "...:free") are left alone.
func parseModelRef(ref string) (provider, model string) {
	if i := strings.Index(ref, ":"); i > 0 && IsKnownProvider(ref[:i]) {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

// resolveChoice turns a user-supplied model reference into a (provider, model) pair.
func resolveChoice(ref, explicitProvider string) (modelChoice, bool) {
	prefix, model := parseModelRef(ref)
	provider := explicitProvider
	if prefix != "" {
		provider = prefix
	}
	if provider == "" {
		inferred, found := inferProviderForModel(model)
		if !found {
			return modelChoice{}, false
		}
		provider = inferred
	}
	return modelChoice{Provider: provider, Model: model}, true
}

func resolveCandidates(ctx context.Context, db *sql.DB, projectID string, encryptionKey []byte, req Request, logger *slog.Logger, rdb *redis.Client) ([]Candidate, error) {
	var choices []modelChoice

	if req.Model != "" {
		primary, found := resolveChoice(req.Model, req.Provider)
		if !found {
			return nil, clientError(fmt.Errorf("unknown model %q: provider could not be determined, specify \"provider\" or use \"provider:model\"", req.Model))
		}
		if !IsKnownProvider(primary.Provider) {
			return nil, clientError(fmt.Errorf("unknown provider %q (supported: %s)", primary.Provider, strings.Join(KnownProviders(), ", ")))
		}
		choices = []modelChoice{primary}

		for _, ref := range req.FallbackModels {
			fb, found := resolveChoice(ref, "")
			if !found || !IsKnownProvider(fb.Provider) {
				logger.Info("skipping unknown fallback model", "model", ref)
				continue
			}
			choices = append(choices, fb)
		}
	} else {
		choices = defaultPriority
	}

	providerNames := make([]string, 0, len(choices))
	seen := map[string]bool{}
	for _, c := range choices {
		if !seen[c.Provider] {
			seen[c.Provider] = true
			providerNames = append(providerNames, c.Provider)
		}
	}

	// One cache/DB round trip for every provider in the chain.
	credsByProvider, err := credentials.GetManyCached(ctx, db, rdb, projectID, providerNames, encryptionKey)
	if err != nil {
		logger.Error("failed to fetch credentials", "error", err)
		return nil, err
	}

	// One Redis round trip to learn which credentials are cooling down.
	var cooling map[string]bool
	if rdb != nil {
		var ids []string
		for _, list := range credsByProvider {
			for _, c := range list {
				ids = append(ids, c.ID)
			}
		}
		cooling, err = cooldown.CoolingSet(ctx, rdb, ids)
		if err != nil {
			logger.Error("failed to check cooldown state", "error", err)
			cooling = nil
		}
	}

	var candidates []Candidate
	for _, choice := range choices {
		creds := credsByProvider[choice.Provider]
		usable := make([]credentials.Credential, 0, len(creds))
		for _, cred := range creds {
			if cooling[cred.ID] {
				logger.Info("skipping cooling credential", "credential", cred.ID, "provider", choice.Provider)
				continue
			}
			usable = append(usable, cred)
		}

		// Rotate the starting credential so several keys on one provider share
		// load (and rate limits) instead of the first key taking all of it.
		if len(usable) > 1 {
			off := rand.Intn(len(usable))
			usable = append(usable[off:], usable[:off]...)
		}

		for _, cred := range usable {
			adapter := newProviderAdapter(choice.Provider, cred.APIKey)
			if adapter == nil {
				continue
			}
			candidates = append(candidates, Candidate{
				Adapter:      adapter,
				ProviderName: choice.Provider,
				Model:        choice.Model,
				Label:        fmt.Sprintf("%s:%s", choice.Provider, choice.Model),
				CredentialID: cred.ID,
			})
		}
	}

	candidates = demoteUnhealthy(ctx, rdb, projectID, candidates, providerNames, logger)

	if len(candidates) == 0 {
		return nil, unavailableError(fmt.Errorf("no usable credentials for this request: add a credential for the requested provider, or wait for cooling credentials to recover"))
	}

	if requestHasImages(req) {
		candidates = filterVisionCapable(candidates, logger)
		if len(candidates) == 0 {
			return nil, clientError(fmt.Errorf("request contains images but no vision-capable model is available for this project"))
		}
	}

	return candidates, nil
}

// ModelCatalog returns the model-name -> provider shortcuts the gateway can
// route without an explicit provider.
func ModelCatalog() map[string]string {
	out := make(map[string]string, len(modelToProvider))
	for k, v := range modelToProvider {
		out[k] = v
	}
	return out
}

// demoteUnhealthy moves candidates of providers whose circuit breaker is open
// behind the healthy ones. Nothing is removed: if every provider is unhealthy
// they are all still tried, in the original order.
func demoteUnhealthy(ctx context.Context, rdb *redis.Client, projectID string, candidates []Candidate, providerNames []string, logger *slog.Logger) []Candidate {
	if rdb == nil || len(candidates) < 2 {
		return candidates
	}
	open, err := breaker.OpenSet(ctx, rdb, providerNames)
	if err != nil {
		logger.Error("failed to read circuit breaker state", "error", err)
		return candidates
	}
	if len(open) == 0 {
		return candidates
	}

	healthy := make([]Candidate, 0, len(candidates))
	var demoted []Candidate
	for _, c := range candidates {
		if open[c.ProviderName] {
			demoted = append(demoted, c)
		} else {
			healthy = append(healthy, c)
		}
	}
	if len(healthy) == 0 || len(demoted) == 0 {
		return candidates
	}
	// Only count a reroute when it actually changed who gets tried first.
	if open[candidates[0].ProviderName] {
		logger.Info("circuit open, routing around unhealthy provider", "provider", candidates[0].ProviderName)
		stats.IncrAsync(rdb, projectID, stats.CircuitReroute)
	}
	return append(healthy, demoted...)
}
