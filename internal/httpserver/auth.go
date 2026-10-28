package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/budget"
	"github.com/Devpaul-01/llm-gateway/internal/concurrency"
	"github.com/Devpaul-01/llm-gateway/internal/gatewaykeys"
	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/Devpaul-01/llm-gateway/internal/providers"
	"github.com/Devpaul-01/llm-gateway/internal/ratelimit"
	"github.com/redis/go-redis/v9"
)

type projectIDContextKey struct{}
type gatewayKeyIDContextKey struct{}

func ProjectIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(projectIDContextKey{}).(string)
	return id, ok
}

func GatewayKeyIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(gatewayKeyIDContextKey{}).(string)
	return id, ok
}

func RequireGatewayKey(db *sql.DB, rdb *redis.Client, logger *slog.Logger, defaults projectsettings.Defaults, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			writeJSONError(w, http.StatusUnauthorized, "authentication_error", "missing or malformed Authorization header")
			return
		}
		plaintext := strings.TrimPrefix(authHeader, prefix)

		key, err := gatewaykeys.LookupByPlaintext(r.Context(), db, plaintext)
		if err != nil {
			logger.Error("gateway key lookup failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		if key == nil {
			writeJSONError(w, http.StatusUnauthorized, "authentication_error", "invalid API key")
			return
		}

		settings, err := projectsettings.Resolve(r.Context(), db, key.ProjectID, defaults)
		if err != nil {
			logger.Error("failed to resolve project settings", "project", key.ProjectID, "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}

		allowed, err := ratelimit.AllowRequest(r.Context(), rdb, key.ID, settings.RateLimitPerMin, time.Minute)
		if err != nil {
			logger.Error("rate limit check failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		if !allowed {
			writeJSONError(w, http.StatusTooManyRequests, "rate_limit_error", "rate limit exceeded")
			return
		}

		ctx := context.WithValue(r.Context(), projectIDContextKey{}, key.ProjectID)
		ctx = context.WithValue(ctx, gatewayKeyIDContextKey{}, key.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type chatCompletionRequest struct {
	Model             string            `json:"model"`
	Provider          string            `json:"provider"`
	Messages          []chatMessageJSON `json:"messages"`
	Temperature       float64           `json:"temperature"`
	MaxTokens         int               `json:"max_tokens"`
	Stream            bool              `json:"stream"`
	ContinueOnFailure bool              `json:"continue_on_failure"`
	FallbackModels    []string          `json:"fallback_models"`
}

type chatMessageJSON struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentPartJSON struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
}

func parseMessage(m chatMessageJSON) (providers.Message, error) {
	var text string
	if err := json.Unmarshal(m.Content, &text); err == nil {
		return providers.Message{Role: m.Role, Content: text}, nil
	}

	var parts []contentPartJSON
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return providers.Message{}, errBadContent
	}

	msg := providers.Message{Role: m.Role}
	for _, p := range parts {
		switch p.Type {
		case "text":
			msg.Content += p.Text
		case "image_url":
			msg.Images = append(msg.Images, providers.Image{URL: p.ImageURL.URL})
		}
	}
	return msg, nil
}

var errBadContent = errors.New("content must be a string or an array of parts")

func handleChatCompletions(db *sql.DB, rdb *redis.Client, encryptionKey []byte, logger *slog.Logger, defaults projectsettings.Defaults) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

		projectID, ok := ProjectIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "authentication_error", "unauthorized")
			return
		}
		gatewayKeyID, ok := GatewayKeyIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "authentication_error", "unauthorized")
			return
		}

		settings, err := projectsettings.Resolve(r.Context(), db, projectID, defaults)
		if err != nil {
			logger.Error("failed to resolve project settings", "project", projectID, "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}

		allowedConcurrent, err := concurrency.Acquire(r.Context(), rdb, projectID, settings.MaxConcurrentStreams)
		if err != nil {
			logger.Error("concurrency check failed", "project", projectID, "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		if !allowedConcurrent {
			writeJSONError(w, http.StatusTooManyRequests, "rate_limit_error", "too many concurrent requests for this project")
			return
		}
		defer concurrency.Release(context.Background(), rdb, projectID)

		var reqBody chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
			return
		}

		messages := make([]providers.Message, len(reqBody.Messages))
		for i, m := range reqBody.Messages {
			parsed, err := parseMessage(m)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
				return
			}
			messages[i] = parsed
		}

		maxTokens := reqBody.MaxTokens
		if maxTokens <= 0 {
			maxTokens = 1024
		}

		allowedBudget, err := budget.CheckAndReserve(r.Context(), rdb, projectID, maxTokens, settings.TokenBudget)
		if err != nil {
			logger.Error("budget check failed", "project", projectID, "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		if !allowedBudget {
			writeJSONError(w, http.StatusTooManyRequests, "rate_limit_error", "daily token budget exceeded for this project")
			return
		}

		req := providers.Request{
			Model:             reqBody.Model,
			Provider:          reqBody.Provider,
			Messages:          messages,
			Temperature:       reqBody.Temperature,
			MaxTokens:         maxTokens,
			ContinueOnFailure: reqBody.ContinueOnFailure,
			FallbackModels:    reqBody.FallbackModels,
		}

		// HandleChatWithBudget (rather than HandleChat) so the
		// maxTokens reservation made just above via CheckAndReserve gets
		// reconciled against real usage once the request completes --
		// Finding B-3's fix. Without threading maxTokens through here,
		// handler.go would have no way to know what was reserved and
		// therefore nothing to refund.
		out, err := providers.HandleChat(r.Context(), db, projectID, gatewayKeyID, "", encryptionKey, req, logger, rdb)
		if err != nil {
			logger.Error("chat handling failed", "project", projectID, "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		if reqBody.Stream {
			streamSSE(w, out)
		} else {
			writeNonStreamingResponse(w, out)
		}
	}
}
