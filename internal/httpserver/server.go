package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/metrics"
	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

// adminBodyLimit bounds admin request bodies. Finding H-4: admin handlers
// previously had no size limit at all, unlike the chat completions
// handler which already used http.MaxBytesReader. Admin endpoints sit
// behind RequireAdminToken, so the severity is lower than an
// unauthenticated path, but there's no reason to leave it unbounded.
const adminBodyLimit = 1 << 20 // 1MB

func limitBody(limit int64, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next(w, r)
	}
}

// healthResponse backs both /healthz and /readyz.
type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Redis    string `json:"redis"`
}

// healthCheckTimeout bounds how long a health check will wait on each
// dependency ping, so a hung Postgres/Redis connection can't make the
// health endpoint itself hang indefinitely (which would be worse than
// just reporting unhealthy).
const healthCheckTimeout = 2 * time.Second

// handleHealth backs both /healthz and /readyz with a real dependency
// check. Finding H-1: the original /healthz unconditionally returned
// "200 ok" regardless of whether Postgres or Redis were reachable, which
// defeats the entire point of a PaaS health-gated rolling deploy
// (Section 16.4: "a new instance is brought up and health-checked before
// receiving traffic") -- a check that can never fail provides no signal.
//
// /healthz and /readyz currently share this same implementation
// (liveness and readiness collapse to the same real question for this
// service: can it actually serve a request right now), kept as two routes
// rather than one so a PaaS that distinguishes the two concepts (Finding
// H-2) has somewhere correct to point each at, and so the distinction is
// easy to split apart later if liveness and readiness ever need to
// diverge (e.g. a future in-process warm-up state that affects readiness
// but not liveness).
func handleHealth(db *sql.DB, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
		defer cancel()

		resp := healthResponse{Status: "ok", Database: "ok", Redis: "ok"}
		healthy := true

		if err := db.PingContext(ctx); err != nil {
			resp.Database = "unreachable"
			healthy = false
		}

		if err := rdb.Ping(ctx).Err(); err != nil {
			resp.Redis = "unreachable"
			healthy = false
		}

		if !healthy {
			resp.Status = "unhealthy"
		}

		w.Header().Set("Content-Type", "application/json")
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(resp)
	}
}

// Options are the runtime switches that don't belong to any one handler.
type Options struct {
	// CacheEnabled turns on the Redis response cache for temperature-0 requests.
	CacheEnabled bool
	CacheTTL     time.Duration

	// PublicMetrics exposes GET /public/metrics (no auth, read-only) for a
	// portfolio/status page. PublicMetricsProjectID scopes it to one project
	// (empty = aggregate all projects).
	PublicMetrics          bool
	PublicMetricsProjectID string
	// CORSOrigins are the origins allowed to call /public/* from a browser.
	CORSOrigins []string
}

// New builds the HTTP mux. opts is variadic so existing callers (and tests)
// that predate Options keep compiling; with none given, the response cache and
// the public metrics endpoint are off.
func New(db *sql.DB, rdb *redis.Client, encryptionKey []byte, adminToken string, logger *slog.Logger, defaults projectsettings.Defaults, optsList ...Options) *http.ServeMux {
	var opts Options
	if len(optsList) > 0 {
		opts = optsList[0]
	}
	mux := http.NewServeMux()

	health := handleHealth(db, rdb)
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", health)

	// /metrics is intentionally unauthenticated, matching the conventional
	// Prometheus scrape setup (network-level access control, e.g. only
	// the cluster's own Prometheus instance can reach this port/path, is
	// the normal way this endpoint is protected in practice -- adding a
	// bearer-token check here would just complicate scrape config for a
	// surface that exposes aggregate counts, not secrets or any
	// individual project's data).
	mux.Handle("GET /metrics", promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))

	chatHandler := handleChatCompletions(db, rdb, encryptionKey, logger, defaults, opts)
	mux.Handle("POST /v1/chat/completions", RequireGatewayKey(db, rdb, logger, defaults, chatHandler))

	mux.Handle("GET /v1/models", RequireGatewayKey(db, rdb, logger, defaults, handleListModels(db, rdb, logger)))

	mux.Handle("GET /admin/projects", RequireAdminToken(adminToken, handleListProjects(db)))
	mux.Handle("GET /admin/projects/{id}", RequireAdminToken(adminToken, handleGetProject(db)))
	mux.Handle("POST /admin/projects", RequireAdminToken(adminToken, limitBody(adminBodyLimit, handleCreateProject(db))))
	mux.Handle("PATCH /admin/projects/{id}", RequireAdminToken(adminToken, limitBody(adminBodyLimit, handleUpdateProjectSettings(db, rdb))))

	mux.Handle("POST /admin/gateway-keys", RequireAdminToken(adminToken, limitBody(adminBodyLimit, handleCreateGatewayKey(db))))
	mux.Handle("GET /admin/projects/{id}/gateway-keys", RequireAdminToken(adminToken, handleListGatewayKeys(db)))
	mux.Handle("DELETE /admin/projects/{id}/gateway-keys/{keyID}", RequireAdminToken(adminToken, handleRevokeGatewayKey(db, rdb)))

	mux.Handle("POST /admin/credentials", RequireAdminToken(adminToken, limitBody(adminBodyLimit, handleCreateCredential(db, rdb, encryptionKey))))
	mux.Handle("GET /admin/projects/{id}/credentials", RequireAdminToken(adminToken, handleListCredentials(db)))
	mux.Handle("DELETE /admin/projects/{id}/credentials/{credentialID}", RequireAdminToken(adminToken, handleRevokeCredential(db, rdb)))

	mux.Handle("DELETE /admin/projects/{id}/cache", RequireAdminToken(adminToken, handleFlushCache(rdb)))
	mux.Handle("GET /admin/providers", RequireAdminToken(adminToken, handleListProviders()))
	mux.Handle("GET /admin/metrics", RequireAdminToken(adminToken, handleAdminMetrics(db, rdb, logger)))

	if opts.PublicMetrics {
		pub := withCORS(opts.CORSOrigins, handlePublicMetrics(db, rdb, logger, opts.PublicMetricsProjectID))
		mux.Handle("GET /public/metrics", pub)
		mux.Handle("OPTIONS /public/metrics", pub)
	}

	return mux
}

// RunWithGracefulShutdown starts srv and blocks until it exits, either
// because srv.Shutdown returned (triggered by SIGINT/SIGTERM below) or
// because ListenAndServe failed outright. Finding C-3: previously
// main.go called http.ListenAndServe directly with no signal handling at
// all, so a PaaS rolling deploy's SIGTERM (ADR-09: "an old instance is
// removed from the load balancer, sent SIGTERM, and given a grace period
// to finish in-flight requests") killed the process immediately --
// cutting off in-flight streams mid-response and skipping the
// concurrency-counter Release deferred in auth.go's chat handler, since
// an abrupt process kill doesn't unwind Go's defer stack.
//
// shutdownGracePeriod bounds how long Shutdown waits for in-flight
// requests (including long-lived SSE streams) to finish before the
// process exits anyway. This is intentionally generous relative to a
// typical request but still bounded, so a single stuck connection can't
// block a deploy indefinitely.
const shutdownGracePeriod = 25 * time.Second

func RunWithGracefulShutdown(srv *http.Server, logger *slog.Logger) {
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
		return
	case sig := <-sigCh:
		logger.Info("received shutdown signal, draining in-flight requests", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown did not complete cleanly", "error", err)
		os.Exit(1)
	}

	logger.Info("shutdown complete")
}
