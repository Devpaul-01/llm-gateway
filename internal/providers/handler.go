package providers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/requestlog"
)

func HandleChat(ctx context.Context, db *sql.DB, projectID, gatewayKeyID string, encryptionKey []byte, req Request) (<-chan Chunk, error) {
	candidates, err := resolveCandidates(ctx, db, projectID, encryptionKey, req)
	if err != nil {
		return nil, err
	}
	return handleChatWithCandidates(ctx, req, candidates, requestLogContext{
		DB:           db,
		ProjectID:    projectID,
		GatewayKeyID: gatewayKeyID,
	})
}

type requestLogContext struct {
	DB           *sql.DB
	ProjectID    string
	GatewayKeyID string
}

func handleChatWithCandidates(ctx context.Context, req Request, candidates []Candidate, logCtx requestLogContext) (<-chan Chunk, error) {
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

		defer func() {
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
				log.Printf("[requestlog] failed to write log entry: %v", err)
			}
		}()

		defer close(out)
		for _, candidate := range candidates {
			candidateReq := req
			candidateReq.Model = candidate.Model

			providerCh, err := candidate.Provider.Chat(ctx, candidateReq)
			if err != nil {
				continue
			}

			streamFailed := false
			for chunk := range providerCh {
				if chunk.Err != nil {
					streamFailed = true
					break
				}

				if ttft == nil {
					now := time.Now()
					ttft = &now
				}

				select {
				case <-ctx.Done():
					finalStatus = "error"
					return
				case out <- chunk:
				}

				if chunk.Done {
					finalStatus = "success"
					finalProvider = candidate.Label
					finalModel = candidate.Model
					if chunk.Usage != nil {
						tokensIn = &chunk.Usage.PromptTokens
						tokensOut = &chunk.Usage.CompletionTokens
					}
					select {
					case <-ctx.Done():
					case out <- Chunk{Done: true}:
					}
					return
				}
			}

			if !streamFailed {
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
