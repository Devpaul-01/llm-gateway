package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Devpaul-01/llm-gateway/internal/concurrency"
	"github.com/Devpaul-01/llm-gateway/internal/gatewaykeys"
	"github.com/Devpaul-01/llm-gateway/internal/providers"
	"github.com/redis/go-redis/v9"
)

type chatCompletionRequest struct {
	Model       string            `json:"model"`
	Messages    []chatMessageJSON `json:"messages"`
	Temperature float64           `json:"temperature"`
	MaxTokens   int               `json:"max_tokens"`
	Stream      bool              `json:"stream"`
}

type chatMessageJSON struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func handleChatCompletions(db *sql.DB, rdb *redis.Client, encryptionKey []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := ProjectIDFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		gatewayKeyID, ok := GatewayKeyIDFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		allowed, err := concurrency.Acquire(r.Context(), rdb, projectID, 10)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !allowed {
			http.Error(w, "too many concurrent requests for this project", http.StatusTooManyRequests)
			return
		}
		defer concurrency.Release(context.Background(), rdb, projectID)

		var reqBody chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		messages := make([]providers.Message, len(reqBody.Messages))
		for i, m := range reqBody.Messages {
			messages[i] = providers.Message{Role: m.Role, Content: m.Content}
		}

		maxTokens := reqBody.MaxTokens
		if maxTokens <= 0 {
			maxTokens = 1024
		}

		req := providers.Request{
			Model:       reqBody.Model,
			Messages:    messages,
			Temperature: reqBody.Temperature,
			MaxTokens:   maxTokens,
		}

		out, err := providers.HandleChat(r.Context(), db, projectID, gatewayKeyID, encryptionKey, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		streamSSE(w, out)
	}
}

func RequireGatewayKey(db *sql.DB, rdb *redis.Client, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			http.Error(w, "missing or malformed Authorization header", http.StatusUnauthorized)
			return
		}
		plaintext := strings.TrimPrefix(authHeader, prefix)

		key, err := gatewaykeys.LookupByPlaintext(r.Context(), db, plaintext)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if key == nil {
			http.Error(w, "invalid API key", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), projectIDContextKey{}, key.ProjectID)
		ctx = context.WithValue(ctx, gatewayKeyIDContextKey{}, key.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type projectIDContextKey struct{}

func ProjectIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(projectIDContextKey{}).(string)
	return id, ok
}

type gatewayKeyIDContextKey struct{}

func GatewayKeyIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(gatewayKeyIDContextKey{}).(string)
	return id, ok
}
