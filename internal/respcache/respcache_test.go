package respcache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/providers"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
)

func req(content string) providers.Request {
	return providers.Request{Model: "m", MaxTokens: 10, TemperatureSet: true, Messages: []providers.Message{{Role: "user", Content: content}}}
}

func TestCacheable(t *testing.T) {
	if !Cacheable(req("x")) {
		t.Error("temperature 0 should be cacheable")
	}
	r := req("x")
	r.TemperatureSet = false
	if Cacheable(r) {
		t.Error("an omitted temperature means sampling: must not be cached")
	}
	r = req("x")
	r.Temperature = 0.7
	if Cacheable(r) {
		t.Error("non-zero temperature must not be cached")
	}
	r = req("x")
	r.Messages[0].Images = []providers.Image{{URL: "data:..."}}
	if Cacheable(r) {
		t.Error("image requests must not be cached")
	}
}

func TestKeyDependsOnEverythingThatChangesTheAnswer(t *testing.T) {
	base := Key(req("hello"))
	if base != Key(req("hello")) {
		t.Error("key must be stable")
	}
	variants := map[string]providers.Request{}
	r := req("hello")
	r.Model = "other"
	variants["model"] = r
	r = req("hello")
	r.MaxTokens = 11
	variants["max_tokens"] = r
	r = req("hello!")
	variants["content"] = r
	r = req("hello")
	r.Messages[0].Role = "system"
	variants["role"] = r
	r = req("hello")
	r.FallbackModels = []string{"x"}
	variants["fallbacks"] = r
	for name, v := range variants {
		if Key(v) == base {
			t.Errorf("changing %s must change the key", name)
		}
	}
}

func TestEpochInvalidationAndInFlightProtection(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	ctx := context.Background()
	rdb, err := redisclient.Connect(ctx, url)
	if err != nil {
		t.Skipf("cannot connect to redis: %v", err)
	}
	defer rdb.Close()
	const project = "test-respcache-project"
	defer func() {
		keys, _ := rdb.Keys(ctx, "respcache:*"+project+"*").Result()
		if len(keys) > 0 {
			rdb.Del(ctx, keys...)
		}
	}()

	e := Entry{Content: "answer", FinishReason: "stop", Provider: "p", Model: "m"}
	got, epoch, err := Get(ctx, rdb, project, "k1")
	if err != nil || got != nil {
		t.Fatalf("expected a clean miss, got %v %v", got, err)
	}
	if err := Set(ctx, rdb, project, "k1", epoch, e, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, _, _ = Get(ctx, rdb, project, "k1")
	if got == nil || got.Content != "answer" {
		t.Fatalf("expected a hit, got %+v", got)
	}

	// A response that started before an invalidation must not repopulate the cache.
	_, staleEpoch, _ := Get(ctx, rdb, project, "k2")
	if err := Invalidate(ctx, rdb, project); err != nil {
		t.Fatal(err)
	}
	Set(ctx, rdb, project, "k2", staleEpoch, e, time.Minute)
	if got, _, _ := Get(ctx, rdb, project, "k2"); got != nil {
		t.Error("a write from before the invalidation must be discarded")
	}
	// ...and everything cached before it is gone.
	if got, _, _ := Get(ctx, rdb, project, "k1"); got != nil {
		t.Error("invalidate must make old entries unreachable")
	}
}
