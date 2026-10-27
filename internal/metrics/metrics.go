// Package metrics turns request_logs plus the Redis counters into the numbers a
// dashboard needs. Every figure here is chosen to answer one question: "what
// problem does putting this gateway in front of LLM providers solve?"
//
//	reliability  - requests that would have failed, but failover served anyway
//	latency      - p50/p95/p99 and time-to-first-token, with and without cache
//	cache        - hit rate, tokens not re-bought, how much faster hits are
//	protection   - abusive/runaway traffic stopped before it cost anything
//	providers    - a live scoreboard of upstream reliability and speed
package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/stats"
	"github.com/redis/go-redis/v9"
)

type Window struct {
	Name     string
	Interval string // Postgres interval literal
	Hours    int
	BucketS  int // timeseries bucket size in seconds
	Bucket   string
}

var windows = map[string]Window{
	"1h":  {Name: "1h", Interval: "1 hour", Hours: 1, BucketS: 60, Bucket: "1 minute"},
	"6h":  {Name: "6h", Interval: "6 hours", Hours: 6, BucketS: 300, Bucket: "5 minutes"},
	"24h": {Name: "24h", Interval: "24 hours", Hours: 24, BucketS: 900, Bucket: "15 minutes"},
	"7d":  {Name: "7d", Interval: "7 days", Hours: 168, BucketS: 7200, Bucket: "2 hours"},
}

// ParseWindow maps a user-supplied window name to a vetted Window. Anything
// unknown falls back to 24h, so user input never reaches SQL.
func ParseWindow(name string) Window {
	if w, ok := windows[name]; ok {
		return w
	}
	return windows["24h"]
}

type Reliability struct {
	Requests              int64    `json:"requests"`
	Succeeded             int64    `json:"succeeded"`
	Failed                int64    `json:"failed"`
	SuccessRate           *float64 `json:"success_rate"`
	HitUpstreamFailure    int64    `json:"hit_upstream_failure"`
	RescuedByFailover     int64    `json:"rescued_by_failover"`
	RescueRate            *float64 `json:"rescue_rate"`
	RescuedRateLimits     int64    `json:"rescued_rate_limits"`
	RescuedOutages        int64    `json:"rescued_outages"`
	RescuedBadModel       int64    `json:"rescued_bad_model"`
	UpstreamAttemptsTotal int64    `json:"upstream_attempts_total"`
	UpstreamAttemptsFail  int64    `json:"upstream_attempts_failed"`
	CircuitReroutes       int64    `json:"circuit_reroutes"`
}

type Latency struct {
	P50Ms     *int `json:"p50_ms"`
	P95Ms     *int `json:"p95_ms"`
	P99Ms     *int `json:"p99_ms"`
	TTFTP50Ms *int `json:"ttft_p50_ms"`
	TTFTP95Ms *int `json:"ttft_p95_ms"`
}

type Cache struct {
	Hits        int64    `json:"hits"`
	HitRate     *float64 `json:"hit_rate"`
	TokensSaved int64    `json:"tokens_saved"`
	HitP50Ms    *int     `json:"hit_p50_ms"`
	MissP50Ms   *int     `json:"miss_p50_ms"`
	Speedup     *float64 `json:"speedup"`
}

type Usage struct {
	TokensIn       int64 `json:"tokens_in"`
	TokensOut      int64 `json:"tokens_out"`
	TotalTokens    int64 `json:"total_tokens"`
	StreamRequests int64 `json:"stream_requests"`
}

type Protection struct {
	RateLimited        int64 `json:"rate_limited"`
	BudgetBlocked      int64 `json:"budget_blocked"`
	ConcurrencyBlocked int64 `json:"concurrency_blocked"`
	TotalBlocked       int64 `json:"total_blocked"`
}

type ProviderRow struct {
	Provider       string           `json:"provider"`
	Requests       int64            `json:"requests"`
	Share          float64          `json:"share"`
	SuccessRate    *float64         `json:"success_rate"`
	P50Ms          *int             `json:"p50_ms"`
	P95Ms          *int             `json:"p95_ms"`
	TTFTP50Ms      *int             `json:"ttft_p50_ms"`
	Tokens         int64            `json:"tokens"`
	AttemptsOK     int64            `json:"attempts_ok"`
	AttemptsFailed int64            `json:"attempts_failed"`
	FailuresBy     map[string]int64 `json:"failures_by_category"`
}

type ModelRow struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Requests int64  `json:"requests"`
}

type Point struct {
	Time     time.Time `json:"t"`
	Requests int64     `json:"requests"`
	Success  int64     `json:"success"`
	CacheHit int64     `json:"cache_hits"`
	Rescued  int64     `json:"rescued"`
	Tokens   int64     `json:"tokens"`
	P50Ms    *int      `json:"p50_ms"`
	P95Ms    *int      `json:"p95_ms"`
}

type RecentRequest struct {
	Time     time.Time `json:"t"`
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Status   string    `json:"status"`
	TotalMs  *int      `json:"total_ms"`
	TTFTMs   *int      `json:"ttft_ms"`
	Tokens   int64     `json:"tokens"`
	CacheHit bool      `json:"cache_hit"`
	Attempts int       `json:"attempts"`
	Stream   bool      `json:"stream"`
}

type Report struct {
	Window      string          `json:"window"`
	GeneratedAt time.Time       `json:"generated_at"`
	Reliability Reliability     `json:"reliability"`
	Latency     Latency         `json:"latency"`
	Cache       Cache           `json:"cache"`
	Usage       Usage           `json:"usage"`
	Protection  Protection      `json:"protection"`
	Providers   []ProviderRow   `json:"providers"`
	Models      []ModelRow      `json:"top_models"`
	Timeseries  []Point         `json:"timeseries"`
	Recent      []RecentRequest `json:"recent"`
}

func ms(f sql.NullFloat64) *int {
	if !f.Valid {
		return nil
	}
	v := int(math.Round(f.Float64))
	return &v
}

func ratio(num, den int64) *float64 {
	if den <= 0 {
		return nil
	}
	v := math.Round(float64(num)/float64(den)*10000) / 10000
	return &v
}

// scope builds the shared WHERE clause. projectID == "" means all projects.
func scope(projectID string, w Window) (string, []interface{}) {
	if projectID == "" {
		return "created_at >= now() - $1::interval", []interface{}{w.Interval}
	}
	return "created_at >= now() - $1::interval AND project_id = $2::uuid", []interface{}{w.Interval, projectID}
}

// Collect gathers the full report. projectID == "" aggregates every project.
func Collect(ctx context.Context, db *sql.DB, rdb *redis.Client, projectID string, w Window) (*Report, error) {
	where, args := scope(projectID, w)
	r := &Report{Window: w.Name, GeneratedAt: time.Now().UTC(), Providers: []ProviderRow{}, Models: []ModelRow{}, Timeseries: []Point{}, Recent: []RecentRequest{}}

	var canceled int64
	var p50, p95, p99, t50, t95, hit50 sql.NullFloat64
	var tokIn, tokOut, tokSaved int64
	var rescuedKey, rescuedTransient, rescuedBad int64
	err := db.QueryRowContext(ctx, `
		SELECT
		  count(*),
		  count(*) FILTER (WHERE status = 'success'),
		  count(*) FILTER (WHERE status = 'canceled'),
		  count(*) FILTER (WHERE first_error_category IS NOT NULL),
		  count(*) FILTER (WHERE first_error_category IS NOT NULL AND status = 'success'),
		  count(*) FILTER (WHERE first_error_category = 'key_fault' AND status = 'success'),
		  count(*) FILTER (WHERE first_error_category = 'transient' AND status = 'success'),
		  count(*) FILTER (WHERE first_error_category = 'bad_model' AND status = 'success'),
		  count(*) FILTER (WHERE cache_hit),
		  count(*) FILTER (WHERE stream),
		  COALESCE(sum(tokens_in) FILTER (WHERE NOT cache_hit), 0),
		  COALESCE(sum(tokens_out) FILTER (WHERE NOT cache_hit), 0),
		  COALESCE(sum(COALESCE(tokens_in,0) + COALESCE(tokens_out,0)) FILTER (WHERE cache_hit), 0),
		  percentile_cont(0.5)  WITHIN GROUP (ORDER BY total_duration_ms) FILTER (WHERE status = 'success' AND NOT cache_hit),
		  percentile_cont(0.95) WITHIN GROUP (ORDER BY total_duration_ms) FILTER (WHERE status = 'success' AND NOT cache_hit),
		  percentile_cont(0.99) WITHIN GROUP (ORDER BY total_duration_ms) FILTER (WHERE status = 'success' AND NOT cache_hit),
		  percentile_cont(0.5)  WITHIN GROUP (ORDER BY ttft_ms) FILTER (WHERE status = 'success' AND NOT cache_hit AND ttft_ms IS NOT NULL),
		  percentile_cont(0.95) WITHIN GROUP (ORDER BY ttft_ms) FILTER (WHERE status = 'success' AND NOT cache_hit AND ttft_ms IS NOT NULL),
		  percentile_cont(0.5)  WITHIN GROUP (ORDER BY total_duration_ms) FILTER (WHERE cache_hit)
		FROM request_logs WHERE `+where, args...).Scan(
		&r.Reliability.Requests, &r.Reliability.Succeeded, &canceled,
		&r.Reliability.HitUpstreamFailure, &r.Reliability.RescuedByFailover,
		&rescuedKey, &rescuedTransient, &rescuedBad,
		&r.Cache.Hits, &r.Usage.StreamRequests,
		&tokIn, &tokOut, &tokSaved,
		&p50, &p95, &p99, &t50, &t95, &hit50,
	)
	if err != nil {
		return nil, fmt.Errorf("overview query: %w", err)
	}

	rel := &r.Reliability
	rel.Failed = rel.Requests - rel.Succeeded - canceled
	rel.SuccessRate = ratio(rel.Succeeded, rel.Requests-canceled)
	rel.RescueRate = ratio(rel.RescuedByFailover, rel.HitUpstreamFailure)
	rel.RescuedRateLimits, rel.RescuedOutages, rel.RescuedBadModel = rescuedKey, rescuedTransient, rescuedBad

	r.Latency = Latency{P50Ms: ms(p50), P95Ms: ms(p95), P99Ms: ms(p99), TTFTP50Ms: ms(t50), TTFTP95Ms: ms(t95)}

	r.Usage.TokensIn, r.Usage.TokensOut = tokIn, tokOut
	r.Usage.TotalTokens = tokIn + tokOut

	r.Cache.TokensSaved = tokSaved
	r.Cache.HitRate = ratio(r.Cache.Hits, rel.Requests)
	r.Cache.HitP50Ms, r.Cache.MissP50Ms = ms(hit50), ms(p50)
	if hit50.Valid && p50.Valid {
		// Hits are often sub-millisecond and stored as whole ms, so floor the
		// divisor at 1ms: the speed-up is then a conservative lower bound.
		sp := math.Round(p50.Float64/math.Max(hit50.Float64, 1)*10) / 10
		r.Cache.Speedup = &sp
	}

	// Per-provider breakdown (cache hits excluded: they never reached a provider).
	prows, err := db.QueryContext(ctx, `
		SELECT provider, count(*),
		  count(*) FILTER (WHERE status = 'success'),
		  count(*) FILTER (WHERE status = 'canceled'),
		  percentile_cont(0.5)  WITHIN GROUP (ORDER BY total_duration_ms) FILTER (WHERE status = 'success'),
		  percentile_cont(0.95) WITHIN GROUP (ORDER BY total_duration_ms) FILTER (WHERE status = 'success'),
		  percentile_cont(0.5)  WITHIN GROUP (ORDER BY ttft_ms) FILTER (WHERE status = 'success' AND ttft_ms IS NOT NULL),
		  COALESCE(sum(COALESCE(tokens_in,0) + COALESCE(tokens_out,0)), 0)
		FROM request_logs
		WHERE `+where+` AND provider <> '' AND NOT cache_hit
		GROUP BY provider ORDER BY 2 DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("provider query: %w", err)
	}
	var providerTotal int64
	for prows.Next() {
		var row ProviderRow
		var ok, cancel int64
		var a, b, c sql.NullFloat64
		if err := prows.Scan(&row.Provider, &row.Requests, &ok, &cancel, &a, &b, &c, &row.Tokens); err != nil {
			prows.Close()
			return nil, err
		}
		row.SuccessRate = ratio(ok, row.Requests-cancel)
		row.P50Ms, row.P95Ms, row.TTFTP50Ms = ms(a), ms(b), ms(c)
		row.FailuresBy = map[string]int64{}
		providerTotal += row.Requests
		r.Providers = append(r.Providers, row)
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return nil, err
	}

	// Redis counters: rejected-before-provider events and per-attempt outcomes.
	scopeName := stats.GlobalScope
	if projectID != "" {
		scopeName = projectID
	}
	counters, err := stats.Read(ctx, rdb, scopeName, w.Hours)
	if err != nil {
		counters = map[string]int64{}
	}
	r.Protection = Protection{
		RateLimited:        counters[stats.RateLimited],
		BudgetBlocked:      counters[stats.BudgetBlocked],
		ConcurrencyBlocked: counters[stats.ConcurrencyBlocked],
	}
	r.Protection.TotalBlocked = r.Protection.RateLimited + r.Protection.BudgetBlocked + r.Protection.ConcurrencyBlocked

	rel.CircuitReroutes = counters[stats.CircuitReroute]
	attempts := stats.SplitProviders(counters)
	seen := map[string]bool{}
	for i := range r.Providers {
		p := &r.Providers[i]
		seen[p.Provider] = true
		if providerTotal > 0 {
			p.Share = math.Round(float64(p.Requests)/float64(providerTotal)*10000) / 10000
		}
		if a := attempts[p.Provider]; a != nil {
			p.AttemptsOK, p.AttemptsFailed, p.FailuresBy = a.OK, a.Failed, a.Failures
		}
	}
	// Providers that only ever failed never appear in request_logs as the final
	// provider; surface them so an outage is visible rather than invisible.
	for name, a := range attempts {
		if !seen[name] {
			r.Providers = append(r.Providers, ProviderRow{Provider: name, AttemptsOK: a.OK, AttemptsFailed: a.Failed, FailuresBy: a.Failures})
		}
		rel.UpstreamAttemptsTotal += a.OK + a.Failed
		rel.UpstreamAttemptsFail += a.Failed
	}

	mrows, err := db.QueryContext(ctx, `
		SELECT provider, model, count(*) FROM request_logs
		WHERE `+where+` AND provider <> '' AND status = 'success'
		GROUP BY provider, model ORDER BY 3 DESC LIMIT 8`, args...)
	if err != nil {
		return nil, fmt.Errorf("model query: %w", err)
	}
	for mrows.Next() {
		var m ModelRow
		if err := mrows.Scan(&m.Provider, &m.Model, &m.Requests); err != nil {
			mrows.Close()
			return nil, err
		}
		r.Models = append(r.Models, m)
	}
	mrows.Close()

	if r.Timeseries, err = timeseries(ctx, db, where, args, w); err != nil {
		return nil, err
	}

	rrows, err := db.QueryContext(ctx, `
		SELECT created_at, provider, model, status, total_duration_ms, ttft_ms,
		  COALESCE(tokens_in,0) + COALESCE(tokens_out,0), cache_hit, attempts, stream
		FROM request_logs WHERE `+where+`
		ORDER BY created_at DESC LIMIT 15`, args...)
	if err != nil {
		return nil, fmt.Errorf("recent query: %w", err)
	}
	for rrows.Next() {
		var q RecentRequest
		if err := rrows.Scan(&q.Time, &q.Provider, &q.Model, &q.Status, &q.TotalMs, &q.TTFTMs, &q.Tokens, &q.CacheHit, &q.Attempts, &q.Stream); err != nil {
			rrows.Close()
			return nil, err
		}
		r.Recent = append(r.Recent, q)
	}
	rrows.Close()

	return r, nil
}

func timeseries(ctx context.Context, db *sql.DB, where string, args []interface{}, w Window) ([]Point, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT date_bin('%s'::interval, created_at, TIMESTAMPTZ '2000-01-01') AS ts,
		  count(*),
		  count(*) FILTER (WHERE status = 'success'),
		  count(*) FILTER (WHERE cache_hit),
		  count(*) FILTER (WHERE first_error_category IS NOT NULL AND status = 'success'),
		  COALESCE(sum(COALESCE(tokens_in,0) + COALESCE(tokens_out,0)), 0),
		  percentile_cont(0.5)  WITHIN GROUP (ORDER BY total_duration_ms) FILTER (WHERE status = 'success' AND NOT cache_hit),
		  percentile_cont(0.95) WITHIN GROUP (ORDER BY total_duration_ms) FILTER (WHERE status = 'success' AND NOT cache_hit)
		FROM request_logs WHERE %s
		GROUP BY ts ORDER BY ts`, w.Bucket, where), args...)
	if err != nil {
		return nil, fmt.Errorf("timeseries query: %w", err)
	}
	defer rows.Close()

	got := map[int64]Point{}
	for rows.Next() {
		var p Point
		var a, b sql.NullFloat64
		if err := rows.Scan(&p.Time, &p.Requests, &p.Success, &p.CacheHit, &p.Rescued, &p.Tokens, &a, &b); err != nil {
			return nil, err
		}
		p.P50Ms, p.P95Ms = ms(a), ms(b)
		got[p.Time.Unix()] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fill empty buckets with zeros so charts have a continuous x-axis.
	step := int64(w.BucketS)
	end := time.Now().Unix() / step * step
	start := end - int64(w.Hours)*3600/step*step
	points := make([]Point, 0, (end-start)/step+1)
	for t := start; t <= end; t += step {
		if p, ok := got[t]; ok {
			points = append(points, p)
		} else {
			points = append(points, Point{Time: time.Unix(t, 0).UTC()})
		}
	}
	return points, nil
}
