package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/insights"
	"github.com/redis/go-redis/v9"
)

// handleAdminMetrics: GET /admin/metrics?project_id=<uuid, optional>&window=1h|6h|24h|7d
func handleAdminMetrics(db *sql.DB, rdb *redis.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report, err := insights.Collect(r.Context(), db, rdb, r.URL.Query().Get("project_id"), insights.ParseWindow(r.URL.Query().Get("window")))
		if err != nil {
			logger.Error("metrics collection failed", "error", err)
			http.Error(w, "failed to collect metrics", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(report)
	}
}

const publicMetricsTTL = 10 * time.Second

// handlePublicMetrics serves the same report with no auth, for a portfolio or
// status page. It is read-only, contains no keys, prompts, project ids or
// user data, and is cached for a few seconds in Redis so a popular page cannot
// turn into a load test against the database.
func handlePublicMetrics(db *sql.DB, rdb *redis.Client, logger *slog.Logger, projectID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		win := insights.ParseWindow(r.URL.Query().Get("window"))
		cacheKey := "pubmetrics:" + projectID + ":" + win.Name

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=5")

		if rdb != nil {
			if cached, err := rdb.Get(r.Context(), cacheKey).Result(); err == nil {
				w.Header().Set("X-Metrics-Cache", "hit")
				w.Write([]byte(cached))
				return
			}
		}

		report, err := insights.Collect(r.Context(), db, rdb, projectID, win)
		if err != nil {
			logger.Error("public metrics collection failed", "error", err)
			http.Error(w, "metrics unavailable", http.StatusInternalServerError)
			return
		}
		data, err := json.Marshal(report)
		if err != nil {
			http.Error(w, "metrics unavailable", http.StatusInternalServerError)
			return
		}
		if rdb != nil {
			bg, cancel := context.WithTimeout(context.Background(), time.Second)
			rdb.Set(bg, cacheKey, data, publicMetricsTTL)
			cancel()
		}
		w.Header().Set("X-Metrics-Cache", "miss")
		w.Write(data)
	}
}

// withCORS allows browser pages on the listed origins ("*" for any) to call
// the wrapped handler. Only used for the read-only public endpoints; the admin
// and /v1 routes deliberately get no CORS headers.
func withCORS(origins []string, next http.Handler) http.Handler {
	allowAll := false
	allowed := map[string]bool{}
	for _, o := range origins {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o == "*" {
			allowAll = true
		} else if o != "" {
			allowed[o] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" && allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
