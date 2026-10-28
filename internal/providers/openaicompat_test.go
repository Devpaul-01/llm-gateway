package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func collect(t *testing.T, ch <-chan Chunk) (content string, done *Chunk, errChunk *Chunk) {
	t.Helper()
	for c := range ch {
		c := c
		switch {
		case c.Err != nil:
			errChunk = &c
		case c.Done:
			done = &c
		default:
			content += c.Content
		}
	}
	return
}

func serve(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	}))
}

func TestCompat_ParsesStandardUsageChunk(t *testing.T) {
	srv := serve("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":4,\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n")
	defer srv.Close()

	ch, err := NewOpenAICompat("x", "k", srv.URL).Chat(context.Background(), Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	content, done, errChunk := collect(t, ch)
	if errChunk != nil || done == nil {
		t.Fatalf("expected clean completion, err=%v", errChunk)
	}
	if content != "hi" || done.FinishReason != "length" || done.Usage == nil || done.Usage.TotalTokens != 7 {
		t.Errorf("content=%q finish=%q usage=%+v", content, done.FinishReason, done.Usage)
	}
}

func TestCompat_ParsesGroqXGroqUsage(t *testing.T) {
	srv := serve("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"x_groq\":{\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1}}}\n\n" +
		"data: [DONE]\n\n")
	defer srv.Close()

	ch, _ := NewOpenAICompat("groq", "k", srv.URL).Chat(context.Background(), Request{Model: "m"})
	_, done, _ := collect(t, ch)
	if done == nil || done.Usage == nil || done.Usage.TotalTokens != 6 {
		t.Errorf("x_groq usage not parsed (total should be derived): %+v", done)
	}
}

// Regression: a stream that was cut off used to end silently, which made the
// failover engine splice a second model's answer onto the truncated first one.
func TestCompat_TruncatedStreamIsAnErrorNotACompletion(t *testing.T) {
	srv := serve("data: {\"choices\":[{\"delta\":{\"content\":\"half an ans\"}}]}\n\n")
	defer srv.Close()

	ch, _ := NewOpenAICompat("x", "k", srv.URL).Chat(context.Background(), Request{Model: "m"})
	_, done, errChunk := collect(t, ch)
	if done != nil || errChunk == nil || errChunk.Err.Category != ProviderTransient {
		t.Errorf("a stream with no [DONE] and no finish_reason must be a transient error, got done=%v err=%v", done, errChunk)
	}
}

func TestCompat_FinishReasonWithoutDONEStillCompletes(t *testing.T) {
	srv := serve("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
	defer srv.Close()

	ch, _ := NewOpenAICompat("x", "k", srv.URL).Chat(context.Background(), Request{Model: "m"})
	content, done, errChunk := collect(t, ch)
	if errChunk != nil || done == nil || content != "ok" {
		t.Errorf("provider that omits [DONE] but sends finish_reason should complete: %v %v", errChunk, done)
	}
}

func TestCompat_InBandStreamErrorBecomesTransientFailure(t *testing.T) {
	srv := serve("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: {\"error\":{\"message\":\"upstream overloaded\"}}\n\n")
	defer srv.Close()

	ch, _ := NewOpenAICompat("x", "k", srv.URL).Chat(context.Background(), Request{Model: "m"})
	_, _, errChunk := collect(t, ch)
	if errChunk == nil || !strings.Contains(errChunk.Err.Error(), "overloaded") {
		t.Errorf("expected in-band error to surface, got %v", errChunk)
	}
}

func TestCompat_HTTPErrorsAreClassified(t *testing.T) {
	cases := map[int]ErrorCategory{401: KeyFault, 402: KeyFault, 429: KeyFault, 404: BadModel, 408: ProviderTransient, 500: ProviderTransient, 503: ProviderTransient, 400: NonRetryable, 422: NonRetryable}
	for status, want := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", status) }))
		_, err := NewOpenAICompat("x", "k", srv.URL).Chat(context.Background(), Request{Model: "m"})
		srv.Close()
		pe, ok := err.(*ProviderError)
		if !ok || pe.Category != want {
			t.Errorf("status %d: want category %v, got %v", status, want, err)
		}
	}
}

func TestCompat_ConnectionRefusedIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := NewOpenAICompat("x", "k", url).Chat(context.Background(), Request{Model: "m"})
	pe, ok := err.(*ProviderError)
	if !ok || pe.Category != ProviderTransient {
		t.Errorf("a network failure should be transient so failover can proceed, got %v", err)
	}
}

func TestBuildRequestBody_TemperatureOnlySentWhenSet(t *testing.T) {
	unset, _ := buildRequestBody(Request{Model: "m", MaxTokens: 5, Messages: []Message{{Role: "user", Content: "x"}}}, true)
	if strings.Contains(unset, "temperature") {
		t.Errorf("an omitted temperature must not become an explicit 0: %s", unset)
	}
	set, _ := buildRequestBody(Request{Model: "m", Temperature: 0, TemperatureSet: true, Messages: []Message{{Role: "user", Content: "x"}}}, true)
	if !strings.Contains(set, `"temperature":0`) {
		t.Errorf("an explicit temperature 0 must be forwarded: %s", set)
	}
}

func TestBuildRequestBody_ProviderSpecificParameters(t *testing.T) {
	req := Request{Model: "m", MaxTokens: 50, Messages: []Message{{Role: "user", Content: "x"}}}

	openai, _ := buildRequestBodyOpts(req, true, bodyOptions{includeUsage: true, maxCompletionTokens: true})
	if !strings.Contains(openai, `"max_completion_tokens":50`) || strings.Contains(openai, `"max_tokens"`) || !strings.Contains(openai, `"include_usage":true`) {
		t.Errorf("openai-style body wrong: %s", openai)
	}
	plain, _ := buildRequestBodyOpts(req, true, bodyOptions{})
	if !strings.Contains(plain, `"max_tokens":50`) || strings.Contains(plain, "stream_options") {
		t.Errorf("plain body wrong (must not send stream_options to providers that may reject it): %s", plain)
	}
}

func TestRegistry_AllProvidersBuildAdaptersAndEnvOverrideWorks(t *testing.T) {
	for _, name := range KnownProviders() {
		if newProviderAdapter(name, "k") == nil {
			t.Errorf("no adapter for %s", name)
		}
	}
	if len(KnownProviders()) < 12 {
		t.Errorf("expected at least 12 providers, got %d", len(KnownProviders()))
	}
	t.Setenv("PROVIDER_OPENAI_BASE_URL", "http://localhost:9999/v1")
	a := newProviderAdapter("openai", "k").(*OpenAICompatProvider)
	if a.BaseURL != "http://localhost:9999/v1" || !a.MaxCompletionTokens {
		t.Errorf("override/spec not applied: %+v", a)
	}
	if newProviderAdapter("nope", "k") != nil {
		t.Error("unknown provider must yield no adapter")
	}
}

func TestResolveChoice(t *testing.T) {
	cases := []struct {
		ref, explicit, wantProvider, wantModel string
		ok                                     bool
	}{
		{"llama-3.3-70b-versatile", "", "groq", "llama-3.3-70b-versatile", true},
		{"cerebras:llama3.1-8b", "", "cerebras", "llama3.1-8b", true},
		{"some-new-model", "together", "together", "some-new-model", true},
		{"meta-llama/llama-3.3-70b-instruct:free", "openrouter", "openrouter", "meta-llama/llama-3.3-70b-instruct:free", true},
		{"mystery-model", "", "", "", false},
	}
	for _, c := range cases {
		got, ok := resolveChoice(c.ref, c.explicit)
		if ok != c.ok || got.Provider != c.wantProvider || got.Model != c.wantModel {
			t.Errorf("resolveChoice(%q,%q) = %+v,%v", c.ref, c.explicit, got, ok)
		}
	}
}

func TestProviderError_HTTPStatus(t *testing.T) {
	cases := []struct {
		e    *ProviderError
		want int
	}{
		{&ProviderError{Category: NonRetryable, Status: 400}, 400},
		{&ProviderError{Category: NonRetryable, Status: 422}, 400},
		{&ProviderError{Category: KeyFault, Status: 429}, 503},
		{&ProviderError{Category: ProviderTransient, Status: 500}, 502},
		{&ProviderError{Category: NonRetryable}, 502},
		{clientError(fmt.Errorf("x")), 400},
		{unavailableError(fmt.Errorf("x")), 503},
	}
	for i, c := range cases {
		if got := c.e.HTTPStatus(); got != c.want {
			t.Errorf("case %d: got %d want %d", i, got, c.want)
		}
	}
}
