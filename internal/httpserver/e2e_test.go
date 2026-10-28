package httpserver

import (
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
