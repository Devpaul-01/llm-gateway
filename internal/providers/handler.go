package providers

import (
	"context"
	"errors"
)

func HandleChat(ctx context.Context, req Request) (<-chan Chunk, error) {
	candidates, err := resolveCandidates(req)
	if err != nil {
		return nil, err
	}
	return handleChatWithCandidates(ctx, req, candidates)

}

func handleChatWithCandidates(ctx context.Context, req Request, candidates []Candidate) (<-chan Chunk, error) {
	if len(candidates) == 0 {
		return nil, &ProviderError{Category: NonRetryable, Cause: errors.New("no candidates available")}
	}

	out := make(chan Chunk)
	go func() {
		defer close(out)
		for _, candidate := range candidates {
			providerCh, err := candidate.Provider.Chat(ctx, req)
			if err != nil {
				continue
			}

			streamFailed := false
			for chunk := range providerCh {
				if chunk.Err != nil {
					streamFailed = true
					break
				}
				if chunk.Done {
					select {
					case <-ctx.Done():
					case out <- chunk:
					}
					return
				}
				select {
				case <-ctx.Done():
					return
				case out <- chunk:
				}
			}

			if streamFailed {
				// mid-stream failure occurred — default behavior (ADR-15): stop, report error
				select {
				case <-ctx.Done():
				case out <- Chunk{Err: &ProviderError{Category: NonRetryable, Cause: errors.New("stream failed mid-response")}}:
				}
				return
			}

			// channel closed cleanly without a Done chunk — try next candidate
		}
	}()

	return out, nil
}


