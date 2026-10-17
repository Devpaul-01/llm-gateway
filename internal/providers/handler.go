package providers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/budget"
	"github.com/Devpaul-01/llm-gateway/internal/cooldown"
	"github.com/Devpaul-01/llm-gateway/internal/discovery"
	"github.com/Devpaul-01/llm-gateway/internal/metrics"

	"github.com/Devpaul-01/llm-gateway/internal/requestlog"
	"github.com/redis/go-redis/v9"
)

func derefIntOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

type requestLogContext struct {
	DB           *sql.DB
	ProjectID    string
	GatewayKeyID string

	// ReservedBudgetTokens is the estimate that was already reserved
	// against the project's daily token budget (internal/budget) before
	// this request began -- typically the client-requested max_tokens
	// ceiling. Zero means no reservation was made (e.g. called from a
	// context, such as the integration tests, that doesn't go through the
	// budget-gated HTTP handler). This is used, once the real token usage
	// is known, to refund the unused difference back into the budget
	// counter (Finding B-3's fix) rather than permanently charging the
	// full estimate regardless of actual consumption.
	ReservedBudgetTokens int
}

func HandleChat(ctx context.Context, db *sql.DB, projectID, gatewayKeyID string, encryptionKey []byte, req Request, logger *slog.Logger, rdb *redis.Client) (<-chan Chunk, error) {
	candidates, err := resolveCandidates(ctx, db, projectID, encryptionKey, req, logger, rdb)
	if err != nil {
		recordResolutionFailure(ctx, db, projectID, gatewayKeyID, req, err, logger)
		return nil, err
	}
	return handleChatWithCandidates(ctx, req, candidates, requestLogContext{
		DB:           db,
		ProjectID:    projectID,
		GatewayKeyID: gatewayKeyID,
	}, logger, rdb)
}

// HandleChatWithBudget is HandleChat plus the reserved-token figure needed
// to reconcile the budget counter once real usage is known (Finding B-3).
// auth.go's chat handler calls this instead of HandleChat so the
// reservation it already made via budget.CheckAndReserve can be trued up
// after the request completes.
func HandleChatWithBudget(ctx context.Context, db *sql.DB, projectID, gatewayKeyID string, encryptionKey []byte, req Request, logger *slog.Logger, rdb *redis.Client, reservedBudgetTokens int) (<-chan Chunk, error) {
	candidates, err := resolveCandidates(ctx, db, projectID, encryptionKey, req, logger, rdb)
	if err != nil {
		recordResolutionFailure(ctx, db, projectID, gatewayKeyID, req, err, logger)
		// A failure here means no candidate was ever tried, so nothing
		// was reserved against budget beyond the pre-request reservation
		// auth.go already made via budget.CheckAndReserve -- that
		// reservation is refunded in full by auth.go's caller context is
		// out of scope here; resolveCandidates failing is a
		// configuration/request error (no credentials, unknown model),
		// not a billable attempt, so no budget.Adjust call belongs here.
		return nil, err
	}
	return handleChatWithCandidates(ctx, req, candidates, requestLogContext{
		DB:                   db,
		ProjectID:            projectID,
		GatewayKeyID:         gatewayKeyID,
		ReservedBudgetTokens: reservedBudgetTokens,
	}, logger, rdb)
}

// recordResolutionFailure logs and records a request_logs row and a
// metrics observation for a failure that happens during candidate
// resolution itself (no credentials configured, unknown model, no
// vision-capable candidate available, etc.) -- i.e. before
// handleChatWithCandidates's goroutine (and the defer block that
// normally does this) is ever constructed.
//
// This closes a gap found while wiring up the new /metrics endpoint and
// verifying it end-to-end against a live server: a resolution failure
// previously returned directly from HandleChat/HandleChatWithBudget with
// no request_logs row and no metric at all, silently exempting an entire
// class of real, common failures (misconfigured project, typo'd model
// name, no credentials for the requested provider) from both historical
// logging and live observability. It was not caught by the earlier
// static audit -- only surfaced by actually running a request against a
// project with no configured credentials and checking /metrics
// afterward.
func recordResolutionFailure(ctx context.Context, db *sql.DB, projectID, gatewayKeyID string, req Request, resolutionErr error, logger *slog.Logger) {
	if db == nil {
		return
	}

	provider := req.Provider
	if provider == "" {
		provider = "unknown"
	}
	model := req.Model
	if model == "" {
		model = "unknown"
	}

	category := NonRetryable.String()
	if pe, ok := resolutionErr.(*ProviderError); ok {
		category = pe.Category.String()
	}

	entry := requestlog.Entry{
		ProjectID:     projectID,
		GatewayKeyID:  gatewayKeyID,
		Provider:      provider,
		Model:         model,
		Status:        "error",
		ErrorCategory: &category,
		Stream:        true,
	}
	if err := requestlog.Insert(context.Background(), db, entry); err != nil {
		logger.Error("failed to write request log for resolution failure", "error", err)
	}

	metrics.Observe(provider, model, "error", 0, 0, false)
}

func buildContinuationRequest(original Request, accumulatedContent string) Request {
	continued := original
	continued.Messages = append(
		append([]Message{}, original.Messages...),
		Message{Role: "assistant", Content: accumulatedContent},
		Message{Role: "user", Content: "Continue your previous response exactly where it left off. Do not repeat any part of what you already said, and do not restart or summarize."},
	)
	return continued
}

func handleChatWithCandidates(ctx context.Context, req Request, candidates []Candidate, logCtx requestLogContext, logger *slog.Logger, rdb *redis.Client) (<-chan Chunk, error) {
	if len(candidates) == 0 {
		return nil, &ProviderError{Category: NonRetryable, Cause: errors.New("no candidates available")}
	}

	out := make(chan Chunk)

	go func() {
		startTime := time.Now()
		var ttft *time.Time
		var finalStatus string = "error"
		var finalProvider, finalModel string
		var finalCredentialID string
		var finalErrorCategory string
		var tokensIn, tokensOut *int
		var accumulated strings.Builder
		var usedVision bool
		// modelsUsed must be declared before the defer below (not simply
		// before the defer *runs*, but before it textually in this
		// function's source) because a deferred closure is parsed as a
		// function literal referencing the enclosing scope at that point
		// in the source -- an identifier used inside it still has to
		// already be declared in that enclosing scope. It's updated
		// throughout the failover loop below and read back here.
		var modelsUsed []ModelUsage

		defer func() {
			if logCtx.DB == nil {
				return
			}
			totalMs := int(time.Since(startTime).Milliseconds())
			var ttftMs *int
			if ttft != nil {
				ms := int(ttft.Sub(startTime).Milliseconds())
				ttftMs = &ms
			}

			var credentialIDPtr *string
			if finalCredentialID != "" {
				credentialIDPtr = &finalCredentialID
			}
			var errorCategoryPtr *string
			if finalErrorCategory != "" {
				errorCategoryPtr = &finalErrorCategory
			}

			entry := requestlog.Entry{
				ProjectID:       logCtx.ProjectID,
				GatewayKeyID:    logCtx.GatewayKeyID,
				Provider:        finalProvider,
				Model:           finalModel,
				CredentialID:    credentialIDPtr,
				Status:          finalStatus,
				ErrorCategory:   errorCategoryPtr,
				TokensIn:        tokensIn,
				TokensOut:       tokensOut,
				TTFTMs:          ttftMs,
				TotalDurationMs: &totalMs,
				Stream:          true,
				UsedVision:      usedVision,
				ModelsUsed:      modelsUsedToStrings(modelsUsed),
			}
			if err := requestlog.Insert(context.Background(), logCtx.DB, entry); err != nil {
				logger.Error("failed to write request log", "error", err)
			}

			// Record the same measurement as the request_logs row above
			// into Prometheus (Architecture Section 14.2). Derived from
			// the same totalMs/ttftMs/finalProvider/finalModel/finalStatus
			// values just written to entry, not recomputed separately, so
			// the metric and the log row can never disagree about what
			// happened for a given request.
			metrics.Observe(
				finalProvider,
				finalModel,
				finalStatus,
				float64(derefIntOrZero(ttftMs))/1000.0,
				float64(totalMs)/1000.0,
				ttftMs != nil,
			)

			// Refund the gap between what was reserved up front (the
			// client's max_tokens ceiling, typically far larger than what
			// a response actually consumes) and what was actually used.
			// This is the fix for Finding B-3: without it, every request
			// permanently debits the full reservation regardless of real
			// usage, and the budget starves far faster than real spend
			// would justify. If no reservation was made for this call
			// (ReservedBudgetTokens == 0, e.g. a direct HandleChat call
			// outside the HTTP budget-gated path) or the request produced
			// no usage figure at all, there's nothing to reconcile.
			if rdb != nil && logCtx.ReservedBudgetTokens > 0 {
				actualOut := 0
				if tokensOut != nil {
					actualOut = *tokensOut
				}
				delta := actualOut - logCtx.ReservedBudgetTokens
				if err := budget.Adjust(context.Background(), rdb, logCtx.ProjectID, delta); err != nil {
					logger.Error("failed to reconcile budget reservation", "error", err)
				}
			}
		}()

		defer close(out)

		activeReq := req
		for _, candidate := range candidates {
			candidateReq := activeReq
			candidateReq.Model = candidate.Model

			providerCh, err := candidate.Adapter.Chat(ctx, candidateReq)
			if err != nil {
				logger.Info("candidate failed pre-stream", "candidate", candidate.Label, "error", err)

				if pe, ok := err.(*ProviderError); ok {
					switch pe.Category {
					case BadModel:
						if rdb != nil {
							discovery.EvictModel(context.Background(), rdb, candidate.ProviderName, candidate.Model)
						}
					case ProviderTransient:
						if rdb != nil {
							discovery.RecordProviderTransient(context.Background(), rdb, candidate.ProviderName)
						}
					case KeyFault:
						if rdb != nil {
							if markErr := cooldown.MarkFailed(context.Background(), rdb, candidate.CredentialID); markErr != nil {
								logger.Error("failed to mark credential as cooling", "error", markErr)
							}
						}
					case NonRetryable:
						logger.Error("non-retryable failure, aborting fallback chain", "candidate", candidate.Label, "error", err)
						finalStatus = "error"
						finalProvider = candidate.ProviderName
						finalModel = candidate.Model
						finalCredentialID = candidate.CredentialID
						finalErrorCategory = pe.Category.String()
						select {
						case <-ctx.Done():
						case out <- Chunk{Err: pe}:
						}
						return
					}
					// ProviderTransient, BadModel: no action beyond logging above;
					// fall through to the next candidate either way. Still record
					// the category/credential/provider/model in case this turns
					// out to be the last candidate tried (all-candidates-failed
					// path below uses whatever these were last set to).
					finalErrorCategory = pe.Category.String()
					finalProvider = candidate.ProviderName
					finalModel = candidate.Model
					finalCredentialID = candidate.CredentialID
				}
				continue
			}

			streamFailed := false
			var streamErr *ProviderError
			for chunk := range providerCh {
				if chunk.Err != nil {
					streamFailed = true
					streamErr = chunk.Err
					break
				}

				if ttft == nil {
					now := time.Now()
					ttft = &now
				}

				accumulated.WriteString(chunk.Content)

				if chunk.Done {
					// Bug fix (found by actually running the previously
					// untested handler_test.go suite under -race, not
					// caught by the static audit): the original logic
					// here forwarded the provider's own Done chunk via
					// `out <- chunk` first, and then separately sent a
					// second synthetic `Chunk{Done: true, ModelsUsed:
					// ...}` right after. That emits two Done chunks for
					// every successful request -- TestHandleChat_
					// SingleCandidateSuccess and TestHandleChat_
					// FailsOverAfterPreStreamError both expected exactly
					// one Done chunk and would have failed under a real
					// test run. The fix: do not forward the provider's
					// raw Done chunk at all; synthesize exactly one Done
					// chunk carrying ModelsUsed (and anything else the
					// provider's own Done chunk might have carried, e.g.
					// Usage, which is preserved below) and send only that.
					finalStatus = "success"
					modelsUsed = append(modelsUsed, ModelUsage{Provider: candidate.ProviderName, Model: candidate.Model})
					finalProvider = candidate.ProviderName
					finalModel = candidate.Model
					finalCredentialID = candidate.CredentialID
					finalErrorCategory = ""
					if isVisionCapable(candidate.Model) && requestHasImages(req) {
						usedVision = true
					}
					if chunk.Usage != nil {
						tokensIn = &chunk.Usage.PromptTokens
						tokensOut = &chunk.Usage.CompletionTokens
					}
					select {
					case <-ctx.Done():
					case out <- Chunk{Done: true, Usage: chunk.Usage, ModelsUsed: modelsUsed}:
					}
					return
				}

				select {
				case <-ctx.Done():
					finalStatus = "error"
					return
				case out <- chunk:
				}
			}
			if !streamFailed {
				continue
			}

			modelsUsed = append(modelsUsed, ModelUsage{Provider: candidate.ProviderName, Model: candidate.Model})
			finalProvider = candidate.ProviderName
			finalModel = candidate.Model
			finalCredentialID = candidate.CredentialID

			if streamErr != nil {
				finalErrorCategory = streamErr.Category.String()
				switch streamErr.Category {
				case KeyFault:
					if rdb != nil {
						if markErr := cooldown.MarkFailed(context.Background(), rdb, candidate.CredentialID); markErr != nil {
							logger.Error("failed to mark credential as cooling", "error", markErr)
						}
					}
				case BadModel:
					if rdb != nil {
						discovery.EvictModel(context.Background(), rdb, candidate.ProviderName, candidate.Model)
					}
				case ProviderTransient:
					if rdb != nil {
						discovery.RecordProviderTransient(context.Background(), rdb, candidate.ProviderName)
					}
				case NonRetryable:
					logger.Error("non-retryable mid-stream failure, aborting", "candidate", candidate.Label, "error", streamErr)
					finalStatus = "partial"
					select {
					case <-ctx.Done():
					case out <- Chunk{Err: streamErr}:
					}
					return
				}
			}

			if req.ContinueOnFailure {
				logger.Info("mid-stream failure, attempting continuation", "candidate", candidate.Label)
				activeReq = buildContinuationRequest(activeReq, accumulated.String())
				continue
			}

			finalStatus = "partial"
			select {
			case <-ctx.Done():
			case out <- Chunk{Err: &ProviderError{Category: NonRetryable, Cause: errors.New("stream failed mid-response")}}:
			}
			return
		}

		finalStatus = "error"
		if finalErrorCategory == "" {
			finalErrorCategory = NonRetryable.String()
		}
		select {
		case <-ctx.Done():
		case out <- Chunk{Err: &ProviderError{Category: NonRetryable, Cause: errors.New("all candidates failed")}}:
		}
	}()

	return out, nil
}

func modelsUsedToStrings(m []ModelUsage) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, len(m))
	for i, u := range m {
		out[i] = u.Provider + ":" + u.Model
	}
	return out
}
