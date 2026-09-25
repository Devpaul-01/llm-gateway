package providers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/cooldown"
	"github.com/redis/go-redis/v9"

	"github.com/Devpaul-01/llm-gateway/internal/requestlog"
)

type requestLogContext struct {
	DB           *sql.DB
	ProjectID    string
	GatewayKeyID string
}

func HandleChat(ctx context.Context, db *sql.DB, projectID, gatewayKeyID string, encryptionKey []byte, req Request, logger *slog.Logger, rdb *redis.Client) (<-chan Chunk, error) {
	candidates, err := resolveCandidates(ctx, db, projectID, encryptionKey, req, logger)
	if err != nil {
		return nil, err
	}
	return handleChatWithCandidates(ctx, req, candidates, requestLogContext{
		DB:           db,
		ProjectID:    projectID,
		GatewayKeyID: gatewayKeyID,
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
		var tokensIn, tokensOut *int
		var accumulated strings.Builder

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
				ProjectID:       logCtx.ProjectID,
				GatewayKeyID:    logCtx.GatewayKeyID,
				Provider:        finalProvider,
				Model:           finalModel,
				Status:          finalStatus,
				TokensIn:        tokensIn,
				TokensOut:       tokensOut,
				TTFTMs:          ttftMs,
				TotalDurationMs: &totalMs,
				Stream:          true,
			}
			if err := requestlog.Insert(context.Background(), logCtx.DB, entry); err != nil {
				logger.Error("failed to write request log", "error", err)
			}
		}()

		defer close(out)

		activeReq := req
		modelsUsed := []string{}

		for _, candidate := range candidates {
			candidateReq := activeReq
			candidateReq.Model = candidate.Model

			providerCh, err := candidate.Provider.Chat(ctx, candidateReq)
			if err != nil {
				logger.Info("candidate failed pre-stream", "candidate", candidate.Label, "error", err)
				if pe, ok := err.(*ProviderError); ok && pe.Category == KeyFault && rdb != nil {
					if markErr := cooldown.MarkFailed(context.Background(), rdb, candidate.CredentialID); markErr != nil {
						logger.Error("failed to mark credential as cooling", "error", markErr)
					}
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

				select {
				case <-ctx.Done():
					finalStatus = "error"
					return
				case out <- chunk:
				}

				if chunk.Done {
					finalStatus = "success"
					modelsUsed = append(modelsUsed, candidate.Label)
					finalProvider = candidate.Label
					finalModel = candidate.Model
					if chunk.Usage != nil {
						tokensIn = &chunk.Usage.PromptTokens
						tokensOut = &chunk.Usage.CompletionTokens
					}
					select {
					case <-ctx.Done():
					case out <- Chunk{Done: true, ModelsUsed: modelsUsed}:
					}
					return
				}
			}

			if !streamFailed {
				continue
			}

			modelsUsed = append(modelsUsed, candidate.Label)

			if streamErr != nil && streamErr.Category == KeyFault && rdb != nil {
				if markErr := cooldown.MarkFailed(context.Background(), rdb, candidate.CredentialID); markErr != nil {
					logger.Error("failed to mark credential as cooling", "error", markErr)
				}
			}

			if req.ContinueOnFailure {
				logger.Info("mid-stream failure, attempting continuation", "candidate", candidate.Label)
				activeReq = buildContinuationRequest(activeReq, accumulated.String())
				continue
			}

			finalStatus = "partial"
			finalProvider = candidate.Label
			finalModel = candidate.Model
			select {
			case <-ctx.Done():
			case out <- Chunk{Err: &ProviderError{Category: NonRetryable, Cause: errors.New("stream failed mid-response")}}:
			}
			return
		}

		select {
		case <-ctx.Done():
		case out <- Chunk{Err: &ProviderError{Category: NonRetryable, Cause: errors.New("all candidates failed")}}:
		}
	}()

	return out, nil
}
