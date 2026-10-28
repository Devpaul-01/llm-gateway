package providers

import (
	"os"
	"sort"
	"strings"
)

// providerSpec describes one upstream that speaks the OpenAI chat-completions
// format. Adding a provider that follows the format is one entry in this map.
type providerSpec struct {
	BaseURL string
	// StreamUsage: the provider accepts stream_options.include_usage. Providers
	// that always append usage to the stream (Groq, Mistral, OpenRouter...) are
	// left false so we never send a parameter they might reject.
	StreamUsage bool
	// MaxCompletionTokens: use max_completion_tokens instead of max_tokens.
	MaxCompletionTokens bool
}

var providerRegistry = map[string]providerSpec{
	"groq":       {BaseURL: "https://api.groq.com/openai/v1", StreamUsage: true},
	"mistral":    {BaseURL: "https://api.mistral.ai/v1", StreamUsage: true},
	"openrouter": {BaseURL: "https://openrouter.ai/api/v1", StreamUsage: true},
	"gemini":     {BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", StreamUsage: true},
	"openai":     {BaseURL: "https://api.openai.com/v1", StreamUsage: true, MaxCompletionTokens: true},
	"deepseek":   {BaseURL: "https://api.deepseek.com/v1", StreamUsage: true},
	"together":   {BaseURL: "https://api.together.xyz/v1", StreamUsage: true},
	"fireworks":  {BaseURL: "https://api.fireworks.ai/inference/v1", StreamUsage: true},
	"cerebras":   {BaseURL: "https://api.cerebras.ai/v1"},
	"xai":        {BaseURL: "https://api.x.ai/v1", StreamUsage: true},
	"deepinfra":  {BaseURL: "https://api.deepinfra.com/v1/openai", StreamUsage: true},
	"sambanova":  {BaseURL: "https://api.sambanova.ai/v1"},
}

// IsKnownProvider reports whether name is a provider the gateway can route to.
func IsKnownProvider(name string) bool {
	_, ok := providerRegistry[name]
	return ok
}

// KnownProviders returns the supported provider names, sorted.
func KnownProviders() []string {
	names := make([]string, 0, len(providerRegistry))
	for n := range providerRegistry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func envName(provider, suffix string) string {
	return "PROVIDER_" + strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_" + suffix
}

// newProviderAdapter builds an adapter for a provider. Two env overrides exist
// per provider, mainly for pointing the gateway at a mock/proxy in tests:
// PROVIDER_<NAME>_BASE_URL and PROVIDER_<NAME>_STREAM_USAGE (true/false).
func newProviderAdapter(provider, apiKey string) Provider {
	spec, ok := providerRegistry[provider]
	if !ok {
		return nil
	}
	baseURL := spec.BaseURL
	if v := os.Getenv(envName(provider, "BASE_URL")); v != "" {
		baseURL = v
	}
	streamUsage := spec.StreamUsage
	switch strings.ToLower(os.Getenv(envName(provider, "STREAM_USAGE"))) {
	case "true", "1":
		streamUsage = true
	case "false", "0":
		streamUsage = false
	}

	a := NewOpenAICompat(provider, apiKey, baseURL)
	a.StreamUsage = streamUsage
	a.MaxCompletionTokens = spec.MaxCompletionTokens
	return a
}
