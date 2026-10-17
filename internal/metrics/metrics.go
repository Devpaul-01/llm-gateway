// Package metrics provides the Prometheus instrumentation named in
// Architecture Section 14.2: "a request counter labeled by
// provider/model/status, and histograms for time-to-first-token and total
// request duration, supporting real p50/p95/p99 latency reporting." This
// was explicitly sequenced in the architecture doc as coming "after the
// core request path is functional... built against real traffic rather
// than before there is anything meaningful to display" -- added now that
// the core request path, request-log persistence, and HTTP layer are all
// in place and tested.
//
// Deliberately scoped to exactly what Section 14.2 names, not a larger
// observability buildout: no per-request histograms beyond TTFT and total
// duration, no business-logic metrics beyond what the request counter's
// labels already capture. A fuller metrics surface can be added
// incrementally against this same registry once there's real traffic to
// learn from, consistent with the architecture's own reasoning for
// sequencing this after (not before) the system was handling real
// requests.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// RequestsTotal counts every chat completion request's terminal outcome,
// labeled by provider, model, and status (success/error/partial, matching
// the same status values already written to request_logs.status -- see
// internal/requestlog.Entry -- so a metric and a historical log row for
// the same request always agree on what happened).
var RequestsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gateway_requests_total",
		Help: "Total chat completion requests, labeled by provider, model, and final status.",
	},
	[]string{"provider", "model", "status"},
)

// TTFTSeconds is a histogram of time-to-first-token, labeled by provider.
// Bucket boundaries are chosen for typical LLM TTFT ranges (fast local
// models respond in well under a second; slower models or degraded
// upstream conditions can take several seconds).
var TTFTSeconds = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "gateway_ttft_seconds",
		Help:    "Time to first token, in seconds, labeled by provider.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20},
	},
	[]string{"provider"},
)

// RequestDurationSeconds is a histogram of total request duration
// (including any failover/continuation attempts across multiple
// candidates), labeled by provider and status.
var RequestDurationSeconds = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "gateway_request_duration_seconds",
		Help:    "Total request duration, in seconds, labeled by provider and final status.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60},
	},
	[]string{"provider", "status"},
)

// Registry is the Prometheus registry this package's collectors are
// registered against. A dedicated registry (rather than the global
// prometheus.DefaultRegisterer) is used so the /metrics handler's output
// is exactly and only this service's own metrics, with no risk of a
// future dependency that also happens to touch the global default
// registry silently adding unrelated series.
var Registry = prometheus.NewRegistry()

func init() {
	Registry.MustRegister(RequestsTotal, TTFTSeconds, RequestDurationSeconds)
}

// Observe records one completed request's outcome. Called once per
// top-level HandleChat/HandleChatWithBudget invocation, from the same
// defer block in internal/providers/handler.go that already writes the
// corresponding request_logs row -- so the metric and the log entry are
// always derived from the same underlying measurement, not computed
// twice from potentially-diverging logic.
func Observe(provider, model, status string, ttftSeconds, totalDurationSeconds float64, ttftRecorded bool) {
	RequestsTotal.WithLabelValues(provider, model, status).Inc()
	RequestDurationSeconds.WithLabelValues(provider, status).Observe(totalDurationSeconds)
	if ttftRecorded {
		TTFTSeconds.WithLabelValues(provider).Observe(ttftSeconds)
	}
}
