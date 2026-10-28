package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/budget"
	"github.com/Devpaul-01/llm-gateway/internal/concurrency"
	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/Devpaul-01/llm-gateway/internal/providers"
	"github.com/Devpaul-01/llm-gateway/internal/requestlog"
	"github.com/Devpaul-01/llm-gateway/internal/respcache"
	"github.com/Devpaul-01/llm-gateway/internal/stats"
	"github.com/redis/go-redis/v9"
)

type chatCompletionRequest struct {
	Model               string            `json:"model"`
	Provider            string            `json:"provider"`
	Messages            []chatMessageJSON `json:"messages"`
	Temperature         *float64          `json:"temperature"`
	MaxTokens           int               `json:"max_tokens"`
	MaxCompletionTokens int               `json:"max_completion_tokens"`
	Stream              bool              `json:"stream"`
	StreamOptions       *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
	ContinueOnFailure bool     `json:"continue_on_failure"`
	FallbackModels    []string `json:"fallback_models"`
	// Cache lets a caller opt a single request out of the response cache.
	Cache *bool `json:"cache"`
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

func (o Options) cacheTTL() time.Duration {
	if o.CacheTTL <= 0 {
		return time.Hour
	}
	return o.CacheTTL
}

func callerAllowsCache(r *http.Request, bodyFlag *bool) bool {
	if bodyFlag != nil && !*bodyFlag {
		return false
	}
	return !strings.Contains(strings.ToLower(r.Header.Get("Cache-Control")), "no-cache")
}

func handleChatCompletions(db *sql.DB, rdb *redis.Client, encryptionKey []byte, logger *slog.Logger, defaults projectsettings.Defaults, opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

		projectID, ok := ProjectIDFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		gatewayKeyID, ok := GatewayKeyIDFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		settings, ok := SettingsFromContext(r.Context())
		if !ok {
			var err error
			settings, err = projectsettings.ResolveCached(r.Context(), db, rdb, projectID, defaults)
			if err != nil {
				logger.Error("failed to resolve project settings", "project", projectID, "error", err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
				return
			}
		}

		// Parse and validate before touching Redis: malformed requests must be
		// cheap and must never consume a concurrency slot or budget.
		var reqBody chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds 10MB")
				return
			}
			writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
			return
		}
		if len(reqBody.Messages) == 0 {
			writeAPIError(w, http.StatusBadRequest, "missing_messages", "messages must be a non-empty array")
			return
		}
		if reqBody.Temperature != nil && (*reqBody.Temperature < 0 || *reqBody.Temperature > 2) {
			writeAPIError(w, http.StatusBadRequest, "invalid_temperature", "temperature must be between 0 and 2")
			return
		}

		messages := make([]providers.Message, len(reqBody.Messages))
		for i, m := range reqBody.Messages {
			parsed, err := parseMessage(m)
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, "invalid_message", err.Error())
				return
			}
			messages[i] = parsed
		}

		maxTokens := reqBody.MaxTokens
		if maxTokens <= 0 {
			maxTokens = reqBody.MaxCompletionTokens
		}
		if maxTokens <= 0 {
			maxTokens = 1024
		}

		req := providers.Request{
			Model:             reqBody.Model,
			Provider:          reqBody.Provider,
			Messages:          messages,
			MaxTokens:         maxTokens,
			ContinueOnFailure: reqBody.ContinueOnFailure,
			FallbackModels:    reqBody.FallbackModels,
		}
		if reqBody.Temperature != nil {
			req.Temperature = *reqBody.Temperature
			req.TemperatureSet = true
		}
		includeUsage := reqBody.StreamOptions != nil && reqBody.StreamOptions.IncludeUsage

		requestID := providers.NewRequestID()
		w.Header().Set("X-Gateway-Request-Id", requestID)

		// --- response cache -------------------------------------------------
		// Checked before concurrency/budget: a hit costs no provider tokens and
		// holds no provider connection, so it should consume neither.
		cacheMode := "bypass"
		var cacheKey, cacheEpoch string
		if opts.CacheEnabled && rdb != nil && callerAllowsCache(r, reqBody.Cache) && respcacheable(req) {
			cacheMode = "miss"
			cacheKey = respcache.Key(req)
			entry, epoch, err := respcache.Get(r.Context(), rdb, projectID, cacheKey)
			switch {
			case err != nil:
				logger.Error("cache lookup failed", "error", err)
				cacheMode = "bypass"
			case entry != nil:
				cr := newChunkReader(respcache.Replay(entry, requestID, started))
				var res streamResult
				if reqBody.Stream {
					res = streamSSE(w, cr, requestID, "hit", includeUsage, entry.Model)
				} else {
					res = writeNonStreamingResponse(w, cr, requestID, "hit")
				}
				if res.Summary != nil && db != nil {
					logCacheHit(db, logger, projectID, gatewayKeyID, reqBody.Stream, res.Summary, time.Since(started))
				}
				return
			default:
				cacheEpoch = epoch
			}
		}

		// --- admission control ---------------------------------------------
		allowedConcurrent, err := concurrency.Acquire(r.Context(), rdb, projectID, settings.MaxConcurrentStreams)
		if err != nil {
			logger.Error("concurrency check failed", "project", projectID, "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		if !allowedConcurrent {
			stats.IncrAsync(rdb, projectID, stats.ConcurrencyBlocked)
			writeAPIError(w, http.StatusTooManyRequests, "too_many_concurrent_requests", "too many concurrent requests for this project")
			return
		}

		// Reserve prompt + completion tokens up front; settle to the real
		// usage afterwards so unused reservation is refunded.
		reserved := maxTokens + providers.EstimatePromptTokens(req)
		reservedAt := time.Now()
		allowedBudget, err := budget.CheckAndReserve(r.Context(), rdb, projectID, reserved, settings.TokenBudget)
		if err != nil {
			concurrency.Release(context.Background(), rdb, projectID)
			logger.Error("budget check failed", "project", projectID, "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		if !allowedBudget {
			concurrency.Release(context.Background(), rdb, projectID)
			stats.IncrAsync(rdb, projectID, stats.BudgetBlocked)
			w.Header().Set("Retry-After", strconv.Itoa(budget.SecondsUntilReset(time.Now())))
			writeAPIError(w, http.StatusTooManyRequests, "daily_token_budget_exceeded", "daily token budget exceeded for this project")
			return
		}

		var res streamResult
		defer func() {
			bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			concurrency.Release(bg, rdb, projectID)

			actual := 0
			switch {
			case res.Summary != nil:
				actual = res.Summary.Usage.TotalTokens
			case res.StreamedChars > 0:
				// Stream died part-way: bill what was actually delivered.
				actual = providers.EstimatePromptTokens(req) + (res.StreamedChars+3)/4
			}
			if err := budget.Settle(bg, rdb, projectID, reservedAt, reserved, actual); err != nil {
				logger.Error("failed to settle token budget", "project", projectID, "error", err)
			}
		}()

		out, err := providers.HandleChat(r.Context(), db, projectID, gatewayKeyID, requestID, encryptionKey, req, logger, rdb)
		if err != nil {
			logger.Error("chat handling failed", "project", projectID, "error", err)
			status := http.StatusInternalServerError
			var pe *providers.ProviderError
			if errors.As(err, &pe) {
				status = pe.HTTPStatus()
			}
			writeAPIError(w, status, "request_failed", err.Error())
			return
		}

		if cacheMode == "miss" {
			out = respcache.Tap(r.Context(), rdb, logger, projectID, cacheKey, cacheEpoch, opts.cacheTTL(), out)
		}

		cr := newChunkReader(out)
		if reqBody.Stream {
			res = streamSSE(w, cr, requestID, cacheMode, includeUsage, reqBody.Model)
		} else {
			res = writeNonStreamingResponse(w, cr, requestID, cacheMode)
		}
	}
}

func respcacheable(req providers.Request) bool { return respcache.Cacheable(req) }

func logCacheHit(db *sql.DB, logger *slog.Logger, projectID, gatewayKeyID string, stream bool, s *providers.Summary, total time.Duration) {
	totalMs := int(total.Milliseconds())
	in, out := s.Usage.PromptTokens, s.Usage.CompletionTokens
	entry := requestlog.Entry{
		RequestID:       s.RequestID,
		ProjectID:       projectID,
		GatewayKeyID:    gatewayKeyID,
		Provider:        s.Provider,
		Model:           s.Model,
		Status:          "success",
		TokensIn:        &in,
		TokensOut:       &out,
		TTFTMs:          &totalMs,
		TotalDurationMs: &totalMs,
		Stream:          stream,
		CacheHit:        true,
		Attempts:        1,
		ModelsUsed:      []string{s.Provider + ":" + s.Model},
	}
	go func() {
		if err := requestlog.InsertWithTimeout(db, entry); err != nil {
			logger.Error("failed to write request log", "error", err)
		}
	}()
}
