package httpserver

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/redis/go-redis/v9"
)

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

func New(db *sql.DB, rdb *redis.Client, encryptionKey []byte, adminToken string, logger *slog.Logger, defaults projectsettings.Defaults, opts Options) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	chatHandler := handleChatCompletions(db, rdb, encryptionKey, logger, defaults, opts)
	mux.Handle("POST /v1/chat/completions", RequireGatewayKey(db, rdb, logger, defaults, chatHandler))
	mux.Handle("GET /v1/models", RequireGatewayKey(db, rdb, logger, defaults, handleListModels(db, rdb, logger)))

	admin := func(h http.Handler) http.Handler { return RequireAdminToken(adminToken, h) }
	mux.Handle("GET /admin/projects", admin(handleListProjects(db)))
	mux.Handle("GET /admin/projects/{id}", admin(handleGetProject(db)))
	mux.Handle("POST /admin/projects", admin(handleCreateProject(db)))
	mux.Handle("PATCH /admin/projects/{id}", admin(handleUpdateProjectSettings(db, rdb)))
	mux.Handle("DELETE /admin/projects/{id}/cache", admin(handleFlushCache(rdb)))
	mux.Handle("POST /admin/gateway-keys", admin(handleCreateGatewayKey(db)))
	mux.Handle("DELETE /admin/gateway-keys/{id}", admin(handleRevokeGatewayKey(db, rdb)))
	mux.Handle("POST /admin/credentials", admin(handleCreateCredential(db, rdb, encryptionKey)))
	mux.Handle("GET /admin/providers", admin(handleListProviders()))
	mux.Handle("GET /admin/metrics", admin(handleAdminMetrics(db, rdb, logger)))

	if opts.PublicMetrics {
		pub := withCORS(opts.CORSOrigins, handlePublicMetrics(db, rdb, logger, opts.PublicMetricsProjectID))
		mux.Handle("GET /public/metrics", pub)
		mux.Handle("OPTIONS /public/metrics", pub)
	}

	return mux
}
