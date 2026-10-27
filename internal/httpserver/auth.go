package httpserver

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/gatewaykeys"
	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/Devpaul-01/llm-gateway/internal/ratelimit"
	"github.com/Devpaul-01/llm-gateway/internal/stats"
	"github.com/redis/go-redis/v9"
)

type projectIDContextKey struct{}
type gatewayKeyIDContextKey struct{}
type settingsContextKey struct{}

func ProjectIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(projectIDContextKey{}).(string)
	return id, ok
}

func GatewayKeyIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(gatewayKeyIDContextKey{}).(string)
	return id, ok
}

// SettingsFromContext returns the project settings the auth middleware already
// resolved, so handlers don't query them a second time.
func SettingsFromContext(ctx context.Context) (projectsettings.Settings, bool) {
	s, ok := ctx.Value(settingsContextKey{}).(projectsettings.Settings)
	return s, ok
}

func RequireGatewayKey(db *sql.DB, rdb *redis.Client, logger *slog.Logger, defaults projectsettings.Defaults, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			writeAPIError(w, http.StatusUnauthorized, "missing_api_key", "missing or malformed Authorization header, expected: Bearer <gateway key>")
			return
		}
		plaintext := strings.TrimPrefix(authHeader, prefix)

		key, err := gatewaykeys.LookupCached(r.Context(), db, rdb, plaintext)
		if err != nil {
			logger.Error("gateway key lookup failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		if key == nil {
			writeAPIError(w, http.StatusUnauthorized, "invalid_api_key", "invalid API key")
			return
		}

		settings, err := projectsettings.ResolveCached(r.Context(), db, rdb, key.ProjectID, defaults)
		if err != nil {
			logger.Error("failed to resolve project settings", "project", key.ProjectID, "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}

		allowed, err := ratelimit.AllowRequest(r.Context(), rdb, key.ID, settings.RateLimitPerMin, time.Minute)
		if err != nil {
			logger.Error("rate limit check failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		if !allowed {
			stats.IncrAsync(rdb, key.ProjectID, stats.RateLimited)
			writeAPIError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded for this API key")
			return
		}

		ctx := context.WithValue(r.Context(), projectIDContextKey{}, key.ProjectID)
		ctx = context.WithValue(ctx, gatewayKeyIDContextKey{}, key.ID)
		ctx = context.WithValue(ctx, settingsContextKey{}, settings)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
