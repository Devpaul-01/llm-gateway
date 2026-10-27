package providers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/breaker"
	"github.com/Devpaul-01/llm-gateway/internal/cooldown"
	"github.com/Devpaul-01/llm-gateway/internal/discovery"
	"github.com/Devpaul-01/llm-gateway/internal/requestlog"
	"github.com/Devpaul-01/llm-gateway/internal/stats"
	"github.com/redis/go-redis/v9"
)

type requestLogContext struct {
	DB           *sql.DB
	ProjectID    string
	GatewayKeyID string
	RequestID    string
}

// HandleChat resolves the candidate chain (primary model + fallbacks, across
// every credential the project owns) and streams the first one that works.
func HandleChat(ctx context.Context, db *sql.DB, projectID, gatewayKeyID, requestID string, encryptionKey []byte, req Request, logger *slog.Logger, rdb *redis.Client) (<-chan Chunk, error) {
	candidates, err := resolveCandidates(ctx, db, projectID, encryptionKey, req, logger, rdb)
	if err != nil {
		return nil, err
	}
	return handleChatWithCandidates(ctx, req, candidates, requestLogContext{
		DB:           db,
		ProjectID:    projectID,
		GatewayKeyID: gatewayKeyID,
		RequestID:    requestID,
	}, logger, rdb)
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

// estimateTokens is a rough chars/4 heuristic, used only when an upstream
// reports no usage at all. Results are flagged as estimated everywhere.
func estimateTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}

// EstimatePromptTokens approximates the prompt size of a request.
func EstimatePromptTokens(req Request) int {
	chars := 0
	for _, m := range req.Messages {
		chars += len(m.Content) + 16 // role + framing overhead
		chars += len(m.Images) * 1000
	}
	return estimateTokens(chars)
}

func handleChatWithCandidates(ctx context.Context, req Request, candidates []Candidate, logCtx requestLogContext, logger *slog.Logger, rdb *redis.Client) (<-chan Chunk, error) {
	if len(candidates) == 0 {
		return nil, &ProviderError{Category: NonRetryable, Cause: errors.New("no candidates available"), ClientStatus: 503}
	}

	out := make(chan Chunk)
	requestID := logCtx.RequestID
	if requestID == "" {
		requestID = NewRequestID()
	}

	go func() {
		startTime := time.Now()
		var ttft *time.Time
		finalStatus := "error"
		var finalProvider, finalModel string
		var finalCredentialID *string
		var usage *Usage
		usageEstimated := false
		attempts, failovers := 0, 0
		var firstErrCategory, lastErrCategory *string
		var accumulated strings.Builder
		var modelsUsed []ModelUsage
		usedVision := requestHasImages(req)

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
			entry := requestlog.Entry{
				RequestID:          requestID,
				ProjectID:          logCtx.ProjectID,
				GatewayKeyID:       logCtx.GatewayKeyID,
				Provider:           finalProvider,
				Model:              finalModel,
				CredentialID:       finalCredentialID,
				Status:             finalStatus,
				FirstErrorCategory: firstErrCategory,
				UsageEstimated:     usageEstimated,
				TTFTMs:             ttftMs,
				TotalDurationMs:    &totalMs,
				Stream:             true,
				UsedVision:         usedVision,
				Attempts:           attempts,
				FailoverCount:      failovers,
			}
			if finalStatus != "success" {
				entry.ErrorCategory = lastErrCategory
			}
			if usage != nil {
				in, outTok := usage.PromptTokens, usage.CompletionTokens
				entry.TokensIn, entry.TokensOut = &in, &outTok
			}
			for _, m := range modelsUsed {
				entry.ModelsUsed = append(entry.ModelsUsed, m.Provider+"/"+m.Model)
			}
			if err := requestlog.InsertWithTimeout(logCtx.DB, entry); err != nil {
				logger.Error("failed to write request log", "error", err)
			}
		}()

		defer close(out)

		activeReq := req
		bg := func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 2*time.Second)
		}

		// recordFailure applies the side effects of a failed attempt: credential
		// cooldown, model eviction, provider health, and the attempt counters.
		recordFailure := func(candidate Candidate, pe *ProviderError) {
			cat := pe.Category.String()
			lastErrCategory = &cat
			if firstErrCategory == nil {
				firstErrCategory = &cat
			}
			finalProvider, finalModel = candidate.ProviderName, candidate.Model
			if rdb == nil {
				return
			}
			stats.IncrAsync(rdb, logCtx.ProjectID, stats.UpstreamFail(candidate.ProviderName, cat))
			c, cancel := bg()
			defer cancel()
			switch pe.Category {
			case BadModel:
				discovery.EvictModel(c, rdb, candidate.ProviderName, candidate.Model)
			case ProviderTransient:
				discovery.RecordProviderTransient(c, rdb, candidate.ProviderName)
				if err := breaker.RecordFailure(c, rdb, candidate.ProviderName); err != nil {
					logger.Error("failed to record provider failure", "error", err)
				}
			case KeyFault:
				if markErr := cooldown.MarkFailed(c, rdb, candidate.CredentialID); markErr != nil {
					logger.Error("failed to mark credential as cooling", "error", markErr)
				}
			}
		}

		toProviderError := func(candidate Candidate, err error) *ProviderError {
			var pe *ProviderError
			if errors.As(err, &pe) {
				return pe
			}
			// A bare error (e.g. connection refused) is treated as transient so
			// failover moves on to the next candidate.
			return &ProviderError{Category: ProviderTransient, Provider: candidate.ProviderName, Model: candidate.Model, Cause: err}
		}

		for _, candidate := range candidates {
			if ctx.Err() != nil {
				finalStatus = "canceled"
				return
			}

			attempts++
			candidateReq := activeReq
			candidateReq.Model = candidate.Model

			providerCh, err := candidate.Adapter.Chat(ctx, candidateReq)
			if err != nil {
				if ctx.Err() != nil {
					finalStatus = "canceled"
					return
				}
				pe := toProviderError(candidate, err)
				logger.Info("candidate failed pre-stream", "candidate", candidate.Label, "error", err)
				recordFailure(candidate, pe)

				if pe.Category == NonRetryable {
					logger.Error("non-retryable failure, aborting fallback chain", "candidate", candidate.Label, "error", err)
					send(ctx, out, Chunk{Err: pe})
					return
				}
				failovers++
				continue
			}

			var streamErr *ProviderError
			completed := false

			for chunk := range providerCh {
				if chunk.Err != nil {
					streamErr = chunk.Err
					break
				}

				if chunk.Done {
					completed = true
					finalStatus = "success"
					finalProvider, finalModel = candidate.ProviderName, candidate.Model
					cid := candidate.CredentialID
					if cid != "" {
						finalCredentialID = &cid
					}
					modelsUsed = append(modelsUsed, ModelUsage{Provider: candidate.ProviderName, Model: candidate.Model})
					if rdb != nil {
						stats.IncrAsync(rdb, logCtx.ProjectID, stats.UpstreamOK(candidate.ProviderName))
						go func(provider string) {
							c, cancel := bg()
							defer cancel()
							_ = breaker.RecordSuccess(c, rdb, provider)
						}(candidate.ProviderName)
					}

					if chunk.Usage != nil && len(modelsUsed) == 1 {
						usage = chunk.Usage
					} else {
						// No usage from the upstream (or the answer was stitched
						// together from several): estimate, and say so.
						u := Usage{
							PromptTokens:     EstimatePromptTokens(req),
							CompletionTokens: estimateTokens(accumulated.Len()),
						}
						u.TotalTokens = u.PromptTokens + u.CompletionTokens
						usage = &u
						usageEstimated = true
					}

					totalMs := int(time.Since(startTime).Milliseconds())
					var ttftMs *int
					if ttft != nil {
						ms := int(ttft.Sub(startTime).Milliseconds())
						ttftMs = &ms
					}
					finish := chunk.FinishReason
					if finish == "" {
						finish = "stop"
					}
					send(ctx, out, Chunk{
						Done:         true,
						Usage:        usage,
						FinishReason: finish,
						ModelsUsed:   modelsUsed,
						Provider:     candidate.ProviderName,
						Model:        candidate.Model,
						Summary: &Summary{
							RequestID:      requestID,
							Provider:       candidate.ProviderName,
							Model:          candidate.Model,
							FinishReason:   finish,
							Usage:          *usage,
							UsageEstimated: usageEstimated,
							TTFTMs:         ttftMs,
							TotalMs:        totalMs,
							Attempts:       attempts,
							Failovers:      failovers,
							ModelsUsed:     modelsUsed,
						},
					})
					return
				}

				if chunk.Content == "" {
					continue
				}
				if ttft == nil {
					now := time.Now()
					ttft = &now
				}
				accumulated.WriteString(chunk.Content)

				if !send(ctx, out, Chunk{Content: chunk.Content, Provider: candidate.ProviderName, Model: candidate.Model}) {
					finalStatus = "canceled"
					finalProvider, finalModel = candidate.ProviderName, candidate.Model
					return
				}
			}

			if completed {
				return
			}

			if ctx.Err() != nil {
				finalStatus = "canceled"
				finalProvider, finalModel = candidate.ProviderName, candidate.Model
				return
			}

			// The stream broke (explicit error, or the channel closed early).
			if streamErr == nil {
				streamErr = &ProviderError{Category: ProviderTransient, Provider: candidate.ProviderName, Model: candidate.Model, Cause: errors.New("stream ended before completion")}
			}
			recordFailure(candidate, streamErr)
			modelsUsed = append(modelsUsed, ModelUsage{Provider: candidate.ProviderName, Model: candidate.Model})

			if streamErr.Category == NonRetryable {
				logger.Error("non-retryable mid-stream failure, aborting", "candidate", candidate.Label, "error", streamErr)
				finalStatus = "partial"
				send(ctx, out, Chunk{Err: streamErr})
				return
			}

			if req.ContinueOnFailure {
				logger.Info("mid-stream failure, attempting continuation", "candidate", candidate.Label)
				activeReq = buildContinuationRequest(activeReq, accumulated.String())
				failovers++
				continue
			}

			finalStatus = "partial"
			send(ctx, out, Chunk{Err: &ProviderError{Category: NonRetryable, Cause: errors.New("stream failed mid-response"), ClientStatus: 502}})
			return
		}

		cause := "all candidates failed"
		if lastErrCategory != nil {
			cause += " (last failure: " + *lastErrCategory + ")"
		}
		send(ctx, out, Chunk{Err: &ProviderError{Category: NonRetryable, Cause: errors.New(cause), ClientStatus: 502}})
	}()

	return out, nil
}
