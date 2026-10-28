package httpserver

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/Devpaul-01/llm-gateway/internal/gatewaykeys"
	"github.com/Devpaul-01/llm-gateway/internal/insights"
	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
	"github.com/redis/go-redis/v9"
)

// sseUpstream mimics an OpenAI-compatible provider. It counts the calls it
// receives so tests can prove a cached answer never reached it.
func sseUpstream(calls *atomic.Int32, content []string, withUsage bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		for _, c := range content {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", c)
			f.Flush()
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		if withUsage {
			fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"total_tokens\":18}}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		f.Flush()
	}))
}

func statusUpstream(calls *atomic.Int32, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":"nope"}`, status)
	}))
}

type env struct {
	t       *testing.T
	srv     *httptest.Server
	pool    *sql.DB
	rdb     *redis.Client
	project string
	key     string
	keyID   string
}

func newEnv(t *testing.T, providerURLs map[string]string, opts Options) *env {
	t.Helper()
	dbURL, redisURL := os.Getenv("DATABASE_URL"), os.Getenv("REDIS_URL")
	if dbURL == "" || redisURL == "" || os.Getenv("ENCRYPTION_KEY") == "" {
		t.Skip("DATABASE_URL, REDIS_URL and ENCRYPTION_KEY required")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dbURL)
	if err != nil {
		t.Skipf("cannot connect to database: %v", err)
	}
	if err := db.Migrate(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rdb, err := redisclient.Connect(ctx, redisURL)
	if err != nil {
		t.Skipf("cannot connect to redis: %v", err)
	}
	encKey, _ := hex.DecodeString(os.Getenv("ENCRYPTION_KEY"))

	e := &env{t: t, pool: pool, rdb: rdb}
	if err := pool.QueryRowContext(ctx, `INSERT INTO projects (name) VALUES ('e2e') RETURNING id`).Scan(&e.project); err != nil {
		t.Fatal(err)
	}
	for provider, url := range providerURLs {
		t.Setenv("PROVIDER_"+strings.ToUpper(provider)+"_BASE_URL", url)
		if err := credentials.Insert(ctx, pool, e.project, provider, "e2e", "sk-"+provider, encKey); err != nil {
			t.Fatal(err)
		}
	}
	plaintext, keyID, err := gatewaykeys.Insert(ctx, pool, e.project, "e2e")
	if err != nil {
		t.Fatal(err)
	}
	e.key, e.keyID = plaintext, keyID

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	defaults := projectsettings.Defaults{RateLimitPerMin: 1000, MaxConcurrentStreams: 50, TokenBudget: 100000}
	e.srv = httptest.NewServer(New(pool, rdb, encKey, "admin-token", logger, defaults, opts))

	t.Cleanup(func() {
		e.srv.Close()
		bg := context.Background()
		pool.ExecContext(bg, `DELETE FROM request_logs WHERE project_id = $1`, e.project)
		pool.ExecContext(bg, `DELETE FROM projects WHERE id = $1`, e.project)
		keys, _ := rdb.Keys(bg, "*"+e.project+"*").Result()
		if len(keys) > 0 {
			rdb.Del(bg, keys...)
		}
		rdb.Del(bg, "ratelimit:"+e.keyID)
		pool.Close()
		rdb.Close()
	})
	return e
}

func (e *env) post(body string, headers ...string) (*http.Response, []byte) {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.key)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func (e *env) budgetCounter() int64 {
	keys, _ := e.rdb.Keys(context.Background(), "budget:"+e.project+":*").Result()
	if len(keys) == 0 {
		return 0
	}
	n, _ := e.rdb.Get(context.Background(), keys[0]).Int64()
	return n
}

func TestE2E_NonStreamingReturnsUsageAndTiming(t *testing.T) {
	var calls atomic.Int32
	up := sseUpstream(&calls, []string{"Hello", ", world"}, true)
	defer up.Close()
	e := newEnv(t, map[string]string{"groq": up.URL}, Options{})

	resp, body := e.post(`{"model":"llama-3.3-70b-versatile","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Object  string `json:"object"`
		Choices []struct {
			Message      map[string]string `json:"message"`
			FinishReason string            `json:"finish_reason"`
		} `json:"choices"`
		Usage   map[string]int         `json:"usage"`
		Gateway map[string]interface{} `json:"gateway"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Choices[0].Message["content"] != "Hello, world" || out.Choices[0].FinishReason != "stop" {
		t.Errorf("unexpected completion: %s", body)
	}
	if out.Usage["total_tokens"] != 18 || out.Usage["prompt_tokens"] != 11 {
		t.Errorf("usage missing or wrong: %v", out.Usage)
	}
	if out.Gateway["provider"] != "groq" || out.Gateway["usage_estimated"] != false {
		t.Errorf("gateway meta wrong: %v", out.Gateway)
	}
	if _, ok := out.Gateway["total_ms"]; !ok {
		t.Errorf("total_ms missing: %v", out.Gateway)
	}
	if !strings.Contains(resp.Header.Get("Server-Timing"), "total;dur=") {
		t.Errorf("Server-Timing header missing: %q", resp.Header.Get("Server-Timing"))
	}
	if resp.Header.Get("X-Gateway-Provider") != "groq" {
		t.Errorf("X-Gateway-Provider header missing")
	}
}

func TestE2E_UsageIsEstimatedWhenUpstreamReportsNone(t *testing.T) {
	var calls atomic.Int32
	up := sseUpstream(&calls, []string{"some text here"}, false)
	defer up.Close()
	e := newEnv(t, map[string]string{"groq": up.URL}, Options{})

	_, body := e.post(`{"model":"llama-3.3-70b-versatile","messages":[{"role":"user","content":"hi"}]}`)
	var out struct {
		Usage   map[string]int         `json:"usage"`
		Gateway map[string]interface{} `json:"gateway"`
	}
	json.Unmarshal(body, &out)
	if out.Usage["completion_tokens"] == 0 || out.Gateway["usage_estimated"] != true {
		t.Errorf("expected estimated usage flagged as such: %s", body)
	}
}

func TestE2E_FailoverIsInvisibleToClientAndRecorded(t *testing.T) {
	var badCalls, goodCalls atomic.Int32
	bad := statusUpstream(&badCalls, 429)
	good := sseUpstream(&goodCalls, []string{"rescued"}, true)
	defer bad.Close()
	defer good.Close()
	e := newEnv(t, map[string]string{"groq": bad.URL, "mistral": good.URL}, Options{})

	resp, body := e.post(`{"model":"llama-3.3-70b-versatile","fallback_models":["mistral-large-latest"],"messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("expected failover to succeed, got %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Gateway map[string]interface{} `json:"gateway"`
	}
	json.Unmarshal(body, &out)
	if out.Gateway["provider"] != "mistral" || out.Gateway["failovers"].(float64) != 1 || out.Gateway["attempts"].(float64) != 2 {
		t.Errorf("gateway meta should show the rescue: %v", out.Gateway)
	}

	// The request log + counters should now tell the same story to the dashboard.
	var report *insights.Report
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, err := insights.Collect(context.Background(), e.pool, e.rdb, e.project, insights.ParseWindow("1h"))
		if err != nil {
			t.Fatalf("metrics: %v", err)
		}
		if r.Reliability.Requests >= 1 && r.Reliability.UpstreamAttemptsFail >= 1 {
			report = r
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if report == nil {
		t.Fatal("metrics never reflected the request")
	}
	rel := report.Reliability
	if rel.RescuedByFailover != 1 || rel.RescuedRateLimits != 1 || rel.RescueRate == nil || *rel.RescueRate != 1 {
		t.Errorf("rescue not recorded: %+v", rel)
	}
	if rel.SuccessRate == nil || *rel.SuccessRate != 1 {
		t.Errorf("success rate should be 100%%: %+v", rel)
	}
	if len(report.Timeseries) == 0 || len(report.Recent) != 1 {
		t.Errorf("timeseries/recent missing: %d points, %d recent", len(report.Timeseries), len(report.Recent))
	}
	var sawBad bool
	for _, p := range report.Providers {
		if p.Provider == "groq" && p.AttemptsFailed == 1 && p.FailuresBy["key_fault"] == 1 {
			sawBad = true
		}
	}
	if !sawBad {
		t.Errorf("failing provider not visible in scoreboard: %+v", report.Providers)
	}
}

func TestE2E_CacheServesDeterministicRequestsWithoutHittingProvider(t *testing.T) {
	var calls atomic.Int32
	up := sseUpstream(&calls, []string{"cached answer"}, true)
	defer up.Close()
	e := newEnv(t, map[string]string{"groq": up.URL}, Options{CacheEnabled: true, CacheTTL: time.Minute})

	req := `{"model":"llama-3.3-70b-versatile","temperature":0,"messages":[{"role":"user","content":"deterministic"}]}`

	r1, _ := e.post(req)
	if r1.Header.Get("X-Gateway-Cache") != "miss" {
		t.Fatalf("first call should be a miss, got %q", r1.Header.Get("X-Gateway-Cache"))
	}
	// Tap stores after the response has been written; give it a moment.
	time.Sleep(200 * time.Millisecond)

	r2, body := e.post(req)
	if r2.Header.Get("X-Gateway-Cache") != "hit" || calls.Load() != 1 {
		t.Fatalf("second call should be a hit with 1 upstream call, got %q / %d calls", r2.Header.Get("X-Gateway-Cache"), calls.Load())
	}
	var out struct {
		Choices []struct {
			Message map[string]string `json:"message"`
		} `json:"choices"`
		Usage map[string]int `json:"usage"`
	}
	json.Unmarshal(body, &out)
	if out.Choices[0].Message["content"] != "cached answer" || out.Usage["total_tokens"] != 18 {
		t.Errorf("cached replay incomplete: %s", body)
	}

	// A sampled request (no explicit temperature) must never be cached.
	e.post(`{"model":"llama-3.3-70b-versatile","messages":[{"role":"user","content":"sampled"}]}`)
	r3, _ := e.post(`{"model":"llama-3.3-70b-versatile","messages":[{"role":"user","content":"sampled"}]}`)
	if r3.Header.Get("X-Gateway-Cache") != "bypass" {
		t.Errorf("sampled request should bypass the cache, got %q", r3.Header.Get("X-Gateway-Cache"))
	}

	// Per-request opt-out.
	r4, _ := e.post(req, "Cache-Control", "no-cache")
	if r4.Header.Get("X-Gateway-Cache") != "bypass" {
		t.Errorf("no-cache should bypass, got %q", r4.Header.Get("X-Gateway-Cache"))
	}

	// Admin flush invalidates immediately.
	fr, _ := http.NewRequest("DELETE", e.srv.URL+"/admin/projects/"+e.project+"/cache", nil)
	fr.Header.Set("Authorization", "Bearer admin-token")
	if resp, err := http.DefaultClient.Do(fr); err != nil || resp.StatusCode != 204 {
		t.Fatalf("flush failed: %v %v", err, resp)
	}
	r5, _ := e.post(req)
	if r5.Header.Get("X-Gateway-Cache") != "miss" {
		t.Errorf("after flush the entry must be gone, got %q", r5.Header.Get("X-Gateway-Cache"))
	}

	// The hit shows up in the metrics as saved tokens.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := insights.Collect(context.Background(), e.pool, e.rdb, e.project, insights.ParseWindow("1h"))
		if r != nil && r.Cache.Hits >= 1 {
			if r.Cache.TokensSaved != 18 {
				t.Errorf("expected 18 tokens saved, got %d", r.Cache.TokensSaved)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("cache hit never appeared in metrics")
}

func TestE2E_StreamingIsOpenAIShapedAndCarriesUsage(t *testing.T) {
	var calls atomic.Int32
	up := sseUpstream(&calls, []string{"a", "b"}, true)
	defer up.Close()
	e := newEnv(t, map[string]string{"groq": up.URL}, Options{})

	resp, _ := func() (*http.Response, []byte) {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/chat/completions", strings.NewReader(
			`{"model":"llama-3.3-70b-versatile","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer "+e.key)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r, nil
	}()
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}

	var content strings.Builder
	var sawUsage, sawDone, sawRole bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		p := strings.TrimPrefix(line, "data: ")
		if p == "[DONE]" {
			sawDone = true
			break
		}
		var ch struct {
			Object  string `json:"object"`
			Choices []struct {
				Delta map[string]string `json:"delta"`
			} `json:"choices"`
			Usage   map[string]int         `json:"usage"`
			Gateway map[string]interface{} `json:"gateway"`
		}
		if err := json.Unmarshal([]byte(p), &ch); err != nil {
			t.Fatalf("bad chunk %q: %v", p, err)
		}
		if ch.Object != "chat.completion.chunk" {
			t.Errorf("object = %q", ch.Object)
		}
		for _, c := range ch.Choices {
			content.WriteString(c.Delta["content"])
			if c.Delta["role"] == "assistant" {
				sawRole = true
			}
		}
		if ch.Usage != nil && ch.Usage["total_tokens"] == 18 {
			sawUsage = true
			if ch.Gateway["provider"] != "groq" {
				t.Errorf("usage chunk missing gateway meta: %v", ch.Gateway)
			}
		}
	}
	if content.String() != "ab" || !sawUsage || !sawDone || !sawRole {
		t.Errorf("stream incomplete: content=%q usage=%v done=%v role=%v", content.String(), sawUsage, sawDone, sawRole)
	}
}

func TestE2E_BudgetIsSettledToActualUsage(t *testing.T) {
	var calls atomic.Int32
	up := sseUpstream(&calls, []string{"short"}, true)
	defer up.Close()
	e := newEnv(t, map[string]string{"groq": up.URL}, Options{})

	// max_tokens 4000 reserves ~4000, but the real usage is 18 tokens.
	resp, body := e.post(`{"model":"llama-3.3-70b-versatile","max_tokens":4000,"messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && e.budgetCounter() != 18 {
		time.Sleep(50 * time.Millisecond)
	}
	if got := e.budgetCounter(); got != 18 {
		t.Errorf("budget should settle to the 18 tokens actually used, counter = %d", got)
	}
}

func TestE2E_ErrorsAreProperHTTPStatusesInOpenAIShape(t *testing.T) {
	var calls atomic.Int32
	bad := statusUpstream(&calls, 500)
	defer bad.Close()
	e := newEnv(t, map[string]string{"groq": bad.URL}, Options{})

	resp, body := e.post(`{"model":"llama-3.3-70b-versatile","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("all-upstreams-failed should be 502, got %d", resp.StatusCode)
	}
	var out struct {
		Error struct{ Message, Type string } `json:"error"`
	}
	if json.Unmarshal(body, &out) != nil || out.Error.Message == "" || out.Error.Type != "upstream_error" {
		t.Errorf("error body not OpenAI-shaped: %s", body)
	}

	// Same for a streaming request: a failure before the first token is a real status, not 200.
	resp, _ = e.post(`{"model":"llama-3.3-70b-versatile","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("streaming pre-token failure should be 502, got %d", resp.StatusCode)
	}

	resp, _ = e.post(`{"model":"no-such-model","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown model should be 400, got %d", resp.StatusCode)
	}
	resp, _ = e.post(`{"model":"llama-3.3-70b-versatile","messages":[]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty messages should be 400, got %d", resp.StatusCode)
	}
}
